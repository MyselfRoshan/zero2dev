package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/events"
	"zero2dev/internal/jobs"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

type env struct {
	t    *testing.T
	m    *Manager
	sup  *supervisor.Supervisor
	st   *store.Store
	dir  string // the project folder of svc
	svc  model.Service
	mu   sync.Mutex
	runs []Cmd
}

// newEnv makes a Manager on temp folders with a fake Runner: nothing is started for real.
func newEnv(t *testing.T, kind string) *env {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	hub := events.New()
	sup := supervisor.New(home, st, hub)
	// closes the log files, which Windows would keep locked in the temp folder
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sup.StopAll(ctx)
		hub.Close()
	})
	dir := filepath.Join(home, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// a program found on the PATH the service gets
	tools := filepath.Join(home, "tools")
	os.MkdirAll(tools, 0o755)
	for _, n := range []string{"faketool", "faketool.exe", "npm", "npm.cmd", "pnpm", "pnpm.cmd", "go", "go.exe", "php", "php.exe"} {
		os.WriteFile(filepath.Join(tools, n), []byte("x"), 0o755)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	svc := model.Service{ID: "svc_t1", Name: "Shop", Kind: kind, Cwd: dir, Port: 3001, Settings: map[string]string{}}
	if err := st.Update(func(d *store.Data) error { d.Services = append(d.Services, svc); return nil }); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, sup: sup, st: st, dir: dir, svc: svc}
	// The pre-steps of a service (the virtual environment, Corepack for pnpm/Yarn) are not run for real.
	sup.PreRun = func(context.Context, string, []string, string, []string, io.Writer) error { return nil }
	e.m = New(home, st, sup, jobs.New(hub))
	e.m.Run = func(ctx context.Context, c Cmd) error {
		e.mu.Lock()
		e.runs = append(e.runs, c)
		e.mu.Unlock()
		fmt.Fprint(c.Out, "line one\r\nline two\nlast without break")
		return nil
	}
	return e
}

func (e *env) touch(rel, content string) {
	e.t.Helper()
	p := filepath.Join(e.dir, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) wait(id string) model.Job {
	e.t.Helper()
	for range 500 {
		if j, ok := e.m.Jobs.Get(id); ok && j.State != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatal("job did not end")
	return model.Job{}
}

func (e *env) lastRun() Cmd {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.runs) == 0 {
		e.t.Fatal("nothing was run")
	}
	return e.runs[len(e.runs)-1]
}

func status(err error) int {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

func TestListPresetsByFile(t *testing.T) {
	e := newEnv(t, "node")
	l, err := e.m.List(e.svc.ID)
	if err != nil || len(l.Presets) != 0 || l.Scripts == nil || len(l.Scripts) != 0 || l.Running != "" {
		t.Fatalf("empty folder: %+v %v", l, err)
	}
	e.touch("package.json", `{"name":"shop","scripts":{"start":"node a.js","build":"vite build","dev":"vite"}}`)
	l, _ = e.m.List(e.svc.ID)
	var presetIDs []string
	for _, p := range l.Presets {
		presetIDs = append(presetIDs, p.ID)
	}
	if !slices.Equal(presetIDs, []string{"npm-install", "npm-build", "npm-test"}) {
		t.Fatalf("presets %v", presetIDs)
	}
	if len(l.Scripts) != 3 || l.Scripts[0] != (Script{"build", "vite build"}) || l.Scripts[2].Name != "start" {
		t.Fatalf("scripts must be sorted by name: %+v", l.Scripts)
	}
	e.touch("package.json", `{ broken`)
	if l, _ = e.m.List(e.svc.ID); len(l.Scripts) != 0 || len(l.Presets) != 3 {
		t.Fatalf("a damaged package.json has no scripts: %+v", l)
	}
	if _, err := e.m.List("svc_nope"); status(err) != 404 {
		t.Fatalf("unknown service: %v", err)
	}

	// a folder `when` and the php kind
	p := newEnv(t, "php")
	p.touch("composer.json", "{}")
	l, _ = p.m.List(p.svc.ID)
	if len(l.Presets) != 3 || l.Presets[0].ID != "composer-install" || len(l.Scripts) != 0 {
		t.Fatalf("php without artisan: %+v", l.Presets)
	}
	p.touch("artisan", "")
	if l, _ = p.m.List(p.svc.ID); len(l.Presets) != 5 {
		t.Fatalf("php with artisan: %+v", l.Presets)
	}
}

func TestStartPresetExpandsAndLogs(t *testing.T) {
	e := newEnv(t, "node")
	e.touch("package.json", "{}")
	j, err := e.m.Start(e.svc.ID, Request{Preset: "npm-build"})
	if err != nil || j.Type != model.JobTask || j.ServiceID != e.svc.ID || j.Step != "Build" || j.State != "running" {
		t.Fatalf("start %+v %v", j, err)
	}
	if l, _ := e.m.List(e.svc.ID); l.Running != j.ID && e.wait(j.ID).State == "running" {
		t.Fatalf("running %q want %q", l.Running, j.ID)
	}
	if done := e.wait(j.ID); done.State != "done" {
		t.Fatalf("job %+v", done)
	}
	c := e.lastRun()
	if filepath.Base(c.Bin) != "npm" && filepath.Base(c.Bin) != "npm.cmd" {
		t.Fatalf("the missing packageManager setting is npm: %s", c.Bin)
	}
	if !slices.Equal(c.Args, []string{"run", "build"}) || c.Dir != e.dir || c.Timeout != 1200*time.Second || !strings.Contains(strings.Join(c.Env, "\n"), "PORT=3001") {
		t.Fatalf("cmd %v %v %s %v", c.Bin, c.Args, c.Dir, c.Timeout)
	}
	var lines []string
	for _, l := range e.sup.Logs(e.svc.ID, 50) {
		if l.Stream == "task" {
			lines = append(lines, l.Text)
		}
	}
	if !slices.Equal(lines, []string{"$ npm run build", "line one", "line two", "last without break"}) {
		t.Fatalf("log %q", lines)
	}

	// pnpm from the settings; the preset's own timeout
	e.st.Update(func(d *store.Data) error {
		d.Services[0].Settings = map[string]string{"packageManager": "pnpm"}
		return nil
	})
	j, err = e.m.Start(e.svc.ID, Request{Preset: "npm-install"})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(j.ID)
	if c := e.lastRun(); !strings.HasPrefix(filepath.Base(c.Bin), "pnpm") || c.Timeout != 1200*time.Second {
		t.Fatalf("pnpm %v %v", c.Bin, c.Timeout)
	}
}

func TestStartProgram(t *testing.T) {
	e := newEnv(t, "node")
	j, err := e.m.Start(e.svc.ID, Request{Program: "faketool", Args: []string{"a b", "--x=$HOME & rm"}})
	if err != nil || j.Step != "faketool a b --x=$HOME & rm" {
		t.Fatalf("%+v %v", j, err)
	}
	e.wait(j.ID)
	if c := e.lastRun(); !slices.Equal(c.Args, []string{"a b", "--x=$HOME & rm"}) || !strings.HasPrefix(filepath.Base(c.Bin), "faketool") {
		t.Fatalf("arguments must go as they are: %v %q", c.Bin, c.Args)
	}
	// a program that is not on the PATH fails the job, not the request
	j, err = e.m.Start(e.svc.ID, Request{Program: "no-such-tool-xyz"})
	if err != nil {
		t.Fatal(err)
	}
	if f := e.wait(j.ID); f.State != "failed" || !strings.Contains(f.Error, "was not found") {
		t.Fatalf("%+v", f)
	}
}

func TestStartRejects(t *testing.T) {
	e := newEnv(t, "node")
	cases := []struct {
		name string
		id   string
		req  Request
		want int
	}{
		{"unknown service", "svc_nope", Request{Program: "x"}, 404},
		{"unknown preset", e.svc.ID, Request{Preset: "nope"}, 404},
		{"neither", e.svc.ID, Request{}, 400},
		{"both", e.svc.ID, Request{Preset: "npm-install", Program: "npm"}, 400},
		{"preset with args", e.svc.ID, Request{Preset: "npm-install", Args: []string{"x"}}, 400},
		{"slash", e.svc.ID, Request{Program: "bin/npm"}, 400},
		{"backslash", e.svc.ID, Request{Program: `C:\x\npm`}, 400},
		{"nul", e.svc.ID, Request{Program: "np\x00m"}, 400},
		{"newline", e.svc.ID, Request{Program: "npm\nx"}, 400},
		{"arg newline", e.svc.ID, Request{Program: "faketool", Args: []string{"a\nb"}}, 400},
		{"arg nul", e.svc.ID, Request{Program: "faketool", Args: []string{"a\x00"}}, 400},
		{"when missing", e.svc.ID, Request{Preset: "npm-install"}, 409},
	}
	for _, c := range cases {
		if _, err := e.m.Start(c.id, c.req); status(err) != c.want {
			t.Errorf("%s: %v (want %d)", c.name, err, c.want)
		}
	}
	// the folder is gone
	os.RemoveAll(e.dir)
	if _, err := e.m.Start(e.svc.ID, Request{Program: "faketool"}); status(err) != 409 {
		t.Errorf("no folder: %v", err)
	}
}

func TestStartSecondTaskAndReserved(t *testing.T) {
	e := newEnv(t, "node")
	release := make(chan struct{})
	e.m.Run = func(ctx context.Context, c Cmd) error { <-release; return nil }
	j, err := e.m.Start(e.svc.ID, Request{Program: "faketool"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.m.Start(e.svc.ID, Request{Program: "faketool"})
	var ae *apperr.Error
	if status(err) != 409 || !errors.As(err, &ae) || ae.Details["jobId"] != j.ID {
		t.Fatalf("second task: %v %+v", err, ae)
	}
	if l, _ := e.m.List(e.svc.ID); l.Running != j.ID {
		t.Fatalf("running %q", l.Running)
	}
	close(release)
	e.wait(j.ID)
	if l, _ := e.m.List(e.svc.ID); l.Running != "" {
		t.Fatalf("after the end: %q", l.Running)
	}
	// reserved by another operation
	rel, err := e.sup.Reserve([]string{e.svc.ID}, "taking a snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Start(e.svc.ID, Request{Program: "faketool"}); status(err) != 409 {
		t.Fatalf("reserved: %v", err)
	}
	rel()
	e.m.Run = func(ctx context.Context, c Cmd) error { return nil }
	j, err = e.m.Start(e.svc.ID, Request{Program: "faketool"})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(j.ID)
}

func TestJobFailures(t *testing.T) {
	e := newEnv(t, "node")
	e.m.Run = func(ctx context.Context, c Cmd) error {
		for i := 1; i <= 8; i++ {
			fmt.Fprintf(c.Out, "out %d\n", i)
		}
		return &ExitError{Code: 3}
	}
	j, _ := e.m.Start(e.svc.ID, Request{Program: "faketool"})
	f := e.wait(j.ID)
	if f.State != "failed" || f.Error != "faketool exited with code 3: out 4; out 5; out 6; out 7; out 8" {
		t.Fatalf("exit: %+v", f)
	}

	e.m.Run = func(ctx context.Context, c Cmd) error { return errors.New("cannot start") }
	j, _ = e.m.Start(e.svc.ID, Request{Program: "faketool"})
	if f = e.wait(j.ID); f.Error != "faketool: cannot start" {
		t.Fatalf("start error: %+v", f)
	}

	// the timeout comes from the preset, ctx ends, the job says so
	e.touch("package.json", "{}")
	var got time.Duration
	e.m.Run = func(ctx context.Context, c Cmd) error {
		got = c.Timeout
		dl, _ := ctx.Deadline()
		if time.Until(dl) > c.Timeout {
			t.Errorf("ctx deadline %v beyond %v", time.Until(dl), c.Timeout)
		}
		return nil
	}
	j, _ = e.m.Start(e.svc.ID, Request{Preset: "npm-test"})
	e.wait(j.ID)
	if got != DefaultTimeout {
		t.Fatalf("timeout %v", got)
	}
}

func TestExecuteTimeout(t *testing.T) {
	e := newEnv(t, "node")
	e.m.Run = func(ctx context.Context, c Cmd) error { <-ctx.Done(); return ctx.Err() }
	err := e.m.execute(e.svc, "faketool", nil, "", nil, 50*time.Millisecond)
	if err == nil || err.Error() != "faketool timed out after 50ms" {
		t.Fatalf("%v", err)
	}
}

func TestComposer(t *testing.T) {
	e := newEnv(t, "php")
	e.touch("composer.json", "{}")
	if _, err := e.m.Start(e.svc.ID, Request{Preset: "composer-install"}); status(err) != 409 || !strings.Contains(err.Error(), "Runtimes") {
		t.Fatalf("no Composer: %v", err)
	}
	e.m.Composer = func(model.Service) (string, []string, error) { return "", nil, errors.New("none") }
	if _, err := e.m.Start(e.svc.ID, Request{Program: "composer", Args: []string{"-V"}}); status(err) != 409 {
		t.Fatalf("failing Composer: %v", err)
	}
	php := filepath.Join(e.dir, "php.exe")
	e.m.Composer = func(model.Service) (string, []string, error) { return php, []string{"composer.phar"}, nil }
	j, err := e.m.Start(e.svc.ID, Request{Preset: "composer-dump-autoload"})
	if err != nil {
		t.Fatal(err)
	}
	e.wait(j.ID)
	if c := e.lastRun(); c.Bin != php || !slices.Equal(c.Args, []string{"composer.phar", "dump-autoload"}) {
		t.Fatalf("composer runs as php: %v %q", c.Bin, c.Args)
	}
}

func TestExpand(t *testing.T) {
	svc := model.Service{Kind: "node", Port: 8080, Settings: map[string]string{"packageManager": "yarn", "script": "a.js"}}
	for in, want := range map[string]string{
		"{s.packageManager}": "yarn", "--port={port}": "--port=8080", "{s.nothing}x": "x", "{s.script} {port}": "a.js 8080", "plain": "plain",
	} {
		if got := expand(in, svc); got != want {
			t.Errorf("%q: %q want %q", in, got, want)
		}
	}
	if got := expand("{s.packageManager}", model.Service{Kind: "node"}); got != "npm" {
		t.Errorf("default %q", got)
	}
	if got := expand("{port}", model.Service{Kind: "go"}); got != "" {
		t.Errorf("no port %q", got)
	}
}

// DefaultRunner is checked with the test binary itself: a helper process, never a real tool.
func TestDefaultRunner(t *testing.T) {
	if os.Getenv("TASKS_HELPER") != "" {
		fmt.Println("to stdout")
		fmt.Fprintln(os.Stderr, "to stderr")
		if os.Getenv("TASKS_HELPER") == "fail" {
			os.Exit(7)
		}
		if os.Getenv("TASKS_HELPER") == "sleep" {
			time.Sleep(time.Minute)
		}
		os.Exit(0)
	}
	self, _ := os.Executable()
	run := func(mode string, ctx context.Context) (string, error) {
		var sb strings.Builder
		var mu sync.Mutex
		out := &lineWriter{emit: func(s string) { mu.Lock(); sb.WriteString(s + "|"); mu.Unlock() }, limit: 3}
		err := DefaultRunner(ctx, Cmd{Bin: self, Args: []string{"-test.run=^TestDefaultRunner$"}, Dir: t.TempDir(), Env: append(os.Environ(), "TASKS_HELPER="+mode), Out: out})
		out.flush()
		mu.Lock()
		defer mu.Unlock()
		return sb.String(), err
	}
	out, err := run("ok", context.Background())
	if err != nil || !strings.Contains(out, "to stdout|") || !strings.Contains(out, "to stderr|") {
		t.Fatalf("ok: %q %v", out, err)
	}
	var ee *ExitError
	if _, err = run("fail", context.Background()); !errors.As(err, &ee) || ee.Code != 7 {
		t.Fatalf("fail: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err = run("sleep", ctx); err == nil || time.Since(start) > 20*time.Second {
		t.Fatalf("sleep must be ended by the context: %v after %v", err, time.Since(start))
	}
}

// Every shipped preset is usable: a unique id, a label, a plain program name, arguments without
// control characters, and for each service kind its tasks are the ones the profile names.
func TestShippedPresets(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range kinds.Profiles() {
		for _, task := range p.Tasks {
			if task.ID == "" || seen[task.ID] || task.Label == "" || task.Help == "" {
				t.Errorf("%s: bad or duplicate task %+v", p.Kind, task)
			}
			seen[task.ID] = true
			svc := model.Service{Kind: p.Kind, Port: 1234}
			if err := checkProgram(expand(task.Program, svc)); err != nil {
				t.Errorf("%s: %v", task.ID, err)
			}
			args := make([]string, len(task.Args))
			for i, a := range task.Args {
				args[i] = expand(a, svc)
			}
			if err := checkArgs(args); err != nil || task.When == "" {
				t.Errorf("%s: %v (when %q)", task.ID, err, task.When)
			}
		}
	}
	for _, id := range []string{"npm-install", "npm-build", "npm-test", "composer-install", "composer-update", "composer-dump-autoload",
		"artisan-migrate", "artisan-cache-clear", "go-mod-download", "go-mod-tidy", "go-test", "go-build"} {
		if !seen[id] {
			t.Errorf("missing preset %s", id)
		}
	}
}

// The preset "composer-install" runs the service's PHP with composer.phar: the manager is
// connected to Supervisor.ComposerCommand (internal/supervisor/composer.go).
func TestComposerPresetRunsPhpWithThePhar(t *testing.T) {
	e := newEnv(t, "php")
	e.touch("composer.json", "{}")
	if _, err := e.m.Start(e.svc.ID, Request{Preset: "composer-install"}); err == nil || !strings.Contains(err.Error(), "Composer is not installed") {
		t.Fatalf("without Composer: %v", err)
	}
	dir := t.TempDir()
	php := filepath.Join(dir, "php", "php.exe")
	bat := filepath.Join(dir, "composer", "composer.bat")
	for _, f := range []string{php, bat, filepath.Join(dir, "composer", "composer.phar")} {
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, []byte("x"), 0o755)
	}
	e.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes,
			model.Runtime{ID: "rt_php", Kind: "php", Version: "8.4", Binary: php, Source: "installed", Default: true},
			model.Runtime{ID: "rt_comp", Kind: "composer", Version: "2.11", Binary: bat, Source: "installed", Default: true})
		return nil
	})
	j, err := e.m.Start(e.svc.ID, Request{Preset: "composer-install"})
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(j.ID); done.State != "done" {
		t.Fatalf("job %+v", done)
	}
	c := e.lastRun()
	if c.Bin != php || len(c.Args) < 2 || c.Args[0] != filepath.Join(dir, "composer", "composer.phar") || c.Args[1] != "install" {
		t.Fatalf("cmd %v %v", c.Bin, c.Args)
	}
}
