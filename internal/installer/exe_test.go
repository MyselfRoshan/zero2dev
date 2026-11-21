package installer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/catalog"
	"zero2dev/internal/model"
)

// A fake rustup-init: it records how it was run and makes the files a real run would.
type fakeRun struct {
	mu                sync.Mutex
	program, dir      string
	args, env         []string
	calls             int
	fn                func(ctx context.Context, dir string, onLine func(string)) error
	existedWhenCalled bool
}

func (f *fakeRun) run(ctx context.Context, program string, args, env []string, dir string, onLine func(string)) error {
	f.mu.Lock()
	f.calls++
	f.program, f.args, f.env, f.dir = program, args, env, dir
	_, serr := os.Stat(program)
	f.existedWhenCalled = serr == nil
	f.mu.Unlock()
	return f.fn(ctx, dir, onLine)
}

func makeCargo(dir string) error {
	p := filepath.Join(dir, "cargo", "bin", "cargo.exe")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte("cargo"), 0o755)
}

func rustEnv(t *testing.T, fake *fakeRun, body []byte, checksum model.Checksum) (*env, model.CatalogEntry) {
	t.Helper()
	e := newEnv(t, map[string][]byte{"/rustup/rustup-init.exe": body, "/rustup/rustup-init.exe.sha256": []byte(sum(body) + "\n")})
	entry := model.CatalogEntry{ID: "rust-1.100.0-windows-amd64", Kind: "rust", Version: "1.100.0", Name: "Rust 1.100.0 (stable, GNU)",
		URL: e.srv.URL + "/rustup/rustup-init.exe", Checksum: checksum, BinaryRelPath: "cargo/bin/cargo.exe",
		OS: runtime.GOOS, Arch: runtime.GOARCH}
	if checksum.Type == "shasums" {
		entry.Checksum.URL = e.srv.URL + "/rustup/rustup-init.exe.sha256"
	}
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{entry}, nil }
	e.in.Run = fake.run
	return e, entry
}

func TestInstallRustRunsRustupInit(t *testing.T) {
	body := []byte("MZ rustup-init")
	fake := &fakeRun{fn: func(_ context.Context, dir string, onLine func(string)) error {
		onLine("info: syncing channel updates for 'stable-x86_64-pc-windows-gnu'")
		onLine("info: downloading component 'cargo'")
		return makeCargo(dir)
	}}
	// The checksum is the bare hash of rust-lang's .sha256 file ("shasums" type).
	e, entry := rustEnv(t, fake, body, model.Checksum{Type: "shasums"})
	j, err := e.in.Start(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j.ID); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	final := filepath.Join(e.in.Bin, entry.ID)
	if fake.calls != 1 || fake.dir != final || fake.program != filepath.Join(final, "rustup-init.exe") || !fake.existedWhenCalled {
		t.Fatalf("run: calls=%d dir=%s program=%s existed=%v", fake.calls, fake.dir, fake.program, fake.existedWhenCalled)
	}
	if strings.Join(fake.args, " ") != strings.Join(rustupArgs, " ") ||
		!strings.Contains(strings.Join(fake.args, " "), "--default-host x86_64-pc-windows-gnu") ||
		!strings.Contains(strings.Join(fake.args, " "), "--no-modify-path") || fake.args[len(fake.args)-1] != "-y" {
		t.Fatalf("args %v", fake.args)
	}
	// The final folder is where it runs: rustup records absolute paths.
	wantEnv := map[string]bool{"RUSTUP_HOME=" + filepath.Join(final, "rustup"): true, "CARGO_HOME=" + filepath.Join(final, "cargo"): true}
	for _, kv := range fake.env {
		delete(wantEnv, kv)
	}
	if len(wantEnv) != 0 {
		t.Fatalf("env %v misses %v", fake.env, wantEnv)
	}
	var rt model.Runtime
	for _, r := range e.st.Snapshot().Runtimes {
		if r.Kind == "rust" {
			rt = r
		}
	}
	if rt.Binary != filepath.Join(final, "cargo", "bin", "cargo.exe") || rt.Version != "1.100.0" || rt.Source != "installed" {
		t.Fatalf("runtime %+v", rt)
	}
	if _, err := os.Stat(filepath.Join(final, "rustup-init.exe")); !os.IsNotExist(err) {
		t.Fatal("the downloaded installer is removed after a successful run")
	}
	if c, r, ok := model.RustHomes(rt.Binary); !ok || c != filepath.Join(final, "cargo") || r != filepath.Join(final, "rustup") {
		t.Fatalf("RustHomes %q %q %v", c, r, ok)
	}
}

func TestInstallRustFailureRemovesFolderAndKeepsLastLines(t *testing.T) {
	fake := &fakeRun{fn: func(_ context.Context, dir string, onLine func(string)) error {
		_ = makeCargo(dir) // a half-done install
		for _, l := range []string{"l1", "l2", "l3", "l4", "l5", "l6", "info: downloading component 'rustc'", "error: could not download component"} {
			onLine(l)
		}
		return errors.New("exit status 1")
	}}
	e, entry := rustEnv(t, fake, []byte("MZ"), model.Checksum{Type: "sha256", Value: sum([]byte("MZ"))})
	j, _ := e.in.Start(entry.ID)
	done := e.wait(t, j.ID)
	if done.State != "failed" || !strings.Contains(done.Error, "exit status 1") || !strings.Contains(done.Error, "could not download component") ||
		strings.Contains(done.Error, "l1 ") || strings.Contains(done.Error, "| l2") {
		t.Fatalf("%+v", done)
	}
	if _, err := os.Stat(filepath.Join(e.in.Bin, entry.ID)); !os.IsNotExist(err) {
		t.Fatal("a failure removes the folder")
	}
	for _, r := range e.st.Snapshot().Runtimes {
		if r.Kind == "rust" {
			t.Fatal("nothing is registered")
		}
	}
}

func TestInstallRustNeedsTheBinary(t *testing.T) {
	fake := &fakeRun{fn: func(context.Context, string, func(string)) error { return nil }} // "succeeds" but makes nothing
	e, entry := rustEnv(t, fake, []byte("MZ"), model.Checksum{Type: "sha256", Value: sum([]byte("MZ"))})
	j, _ := e.in.Start(entry.ID)
	done := e.wait(t, j.ID)
	if done.State != "failed" || !strings.Contains(done.Error, "cargo.exe was not made") {
		t.Fatalf("%+v", done)
	}
	if _, err := os.Stat(filepath.Join(e.in.Bin, entry.ID)); !os.IsNotExist(err) {
		t.Fatal("a failure removes the folder")
	}
}

func TestInstallRustChecksumMismatchRunsNothing(t *testing.T) {
	fake := &fakeRun{fn: func(_ context.Context, dir string, _ func(string)) error { return makeCargo(dir) }}
	e, entry := rustEnv(t, fake, []byte("MZ"), model.Checksum{Type: "sha256", Value: strings.Repeat("a", 64)})
	j, _ := e.in.Start(entry.ID)
	if done := e.wait(t, j.ID); done.State != "failed" || !strings.Contains(done.Error, "checksum mismatch") {
		t.Fatalf("%+v", done)
	}
	if fake.calls != 0 {
		t.Fatal("an unverified program is never run")
	}
}

func TestInstallRustTimeoutEndsTheRun(t *testing.T) {
	old := exeKinds["rust"]
	t.Cleanup(func() { exeKinds["rust"] = old })
	r := old
	r.Timeout = 50 * time.Millisecond
	exeKinds["rust"] = r
	fake := &fakeRun{fn: func(ctx context.Context, _ string, onLine func(string)) error {
		onLine("info: downloading component 'rustc'")
		<-ctx.Done() // a real run has its process tree killed here
		return ctx.Err()
	}}
	e, entry := rustEnv(t, fake, []byte("MZ"), model.Checksum{Type: "sha256", Value: sum([]byte("MZ"))})
	j, _ := e.in.Start(entry.ID)
	done := e.wait(t, j.ID)
	if done.State != "failed" || !strings.Contains(done.Error, "did not finish") || !strings.Contains(done.Error, "downloading component") {
		t.Fatalf("%+v", done)
	}
	if _, err := os.Stat(filepath.Join(e.in.Bin, entry.ID)); !os.IsNotExist(err) {
		t.Fatal("a timeout removes the folder")
	}
}

func TestInstallRustProgressStaysInTheBand(t *testing.T) {
	fake := &fakeRun{fn: func(_ context.Context, dir string, onLine func(string)) error {
		for i := 0; i < 40; i++ {
			onLine("info: installing component 'rust-std'")
		}
		return makeCargo(dir)
	}}
	e, entry := rustEnv(t, fake, []byte("MZ"), model.Checksum{Type: "sha256", Value: sum([]byte("MZ"))})
	em := &recEm{}
	e.in.em = em
	j, _ := e.in.Start(entry.ID)
	if done := e.wait(t, j.ID); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	seen := false
	for _, p := range em.progress {
		if p.step == "Installing: installing component 'rust-std'" {
			seen = true
			if p.percent < exeBandFrom || p.percent > exeBandTo {
				t.Fatalf("percent %d outside the band", p.percent)
			}
		}
	}
	if !seen {
		t.Fatalf("no step from the output: %+v", em.progress)
	}
}

type progressEv struct {
	percent int
	step    string
}
type recEm struct {
	mu       sync.Mutex
	progress []progressEv
}

func (r *recEm) Publish(typ string, data any) {
	if m, ok := data.(map[string]any); ok && typ == "job.progress" {
		r.mu.Lock()
		r.progress = append(r.progress, progressEv{m["percent"].(int), m["step"].(string)})
		r.mu.Unlock()
	}
}

func TestExeKindWithoutDescriptionIsRefused(t *testing.T) {
	fake := &fakeRun{fn: func(context.Context, string, func(string)) error { return nil }}
	e, entry := rustEnv(t, fake, []byte("MZ"), model.Checksum{Type: "sha256", Value: sum([]byte("MZ"))})
	entry.Kind = "go"
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{entry}, nil }
	j, _ := e.in.Start(entry.ID)
	if done := e.wait(t, j.ID); done.State != "failed" || !strings.Contains(done.Error, "not an archive") || fake.calls != 0 {
		t.Fatalf("%+v", done)
	}
}

func TestCatalogAcceptsExe(t *testing.T) {
	raw := `[{"id":"rust-1.100.0-windows-amd64","kind":"rust","version":"1.100.0","name":"Rust","url":"https://static.rust-lang.org/rustup/dist/x86_64-pc-windows-gnu/rustup-init.exe","checksum":{"type":"shasums","url":"https://static.rust-lang.org/rustup/dist/x86_64-pc-windows-gnu/rustup-init.exe.sha256"},"binaryRelPath":"cargo/bin/cargo.exe","os":"windows","arch":"amd64"}]`
	es, err := catalog.Parse([]byte(raw))
	if err != nil || len(es) != 1 || es[0].FileName != "rustup-init.exe" {
		t.Fatalf("%+v %v", es, err)
	}
}

func TestExeStepAndSplit(t *testing.T) {
	if got := exeStep("info: downloading component 'cargo'"); got != "Installing: downloading component 'cargo'" {
		t.Fatal(got)
	}
	if got := exeStep("  "); got != "Installing" {
		t.Fatal(got)
	}
	adv, tok, _ := splitLines([]byte("a\rb\n"), false)
	if adv != 2 || string(tok) != "a" {
		t.Fatalf("%d %q", adv, tok)
	}
}

// The real runner: lines are split on \n and \r, the environment is added, and the end of the
// context ends the program. The program is this test binary, started as a helper.
func TestRunExeRealProcess(t *testing.T) {
	if os.Getenv("Z2D_EXE_HELPER") != "" {
		switch os.Getenv("Z2D_EXE_HELPER") {
		case "lines":
			os.Stdout.WriteString("one\rtwo\nthree " + os.Getenv("RUSTUP_HOME") + "\n")
			os.Stderr.WriteString("four\n")
		case "hang":
			time.Sleep(time.Minute)
		}
		os.Exit(0)
	}
	if testing.Short() || runtime.GOOS == "plan9" {
		t.Skip()
	}
	self, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	var mu sync.Mutex
	var got []string
	err = runExe(context.Background(), self, []string{"-test.run=TestRunExeRealProcess"}, []string{"Z2D_EXE_HELPER=lines", "RUSTUP_HOME=/x"}, t.TempDir(), func(l string) {
		mu.Lock()
		got = append(got, l)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "|")
	for _, w := range []string{"one", "two", "three /x", "four"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("lines %q miss %q", joined, w)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = runExe(ctx, self, []string{"-test.run=TestRunExeRealProcess"}, []string{"Z2D_EXE_HELPER=hang"}, t.TempDir(), func(string) {})
	var ee *exec.ExitError
	if err == nil || (!errors.As(err, &ee) && ctx.Err() == nil) || time.Since(start) > 20*time.Second {
		t.Fatalf("a hanging program must be ended by the context: %v after %v", err, time.Since(start))
	}
}

// Vite+: the ZIP holds only the launcher; after it is unpacked the launcher runs once with VP_HOME
// below the data folder (postKinds) and must have made <VP_HOME>\bin\vp.exe.
func TestInstallViteplusRunsTheLauncherWithVPHome(t *testing.T) {
	z := makeZip(t, map[string]string{"vp.exe": "LAUNCHER", "vp-shim.exe": "SHIM"})
	fake := &fakeRun{fn: func(_ context.Context, _ string, onLine func(string)) error { return nil }}
	e := newEnv(t, map[string][]byte{"/vp/vp.zip": z})
	e.in.Run = fake.run
	vpHome := filepath.Join(e.home, "vite-plus")
	fake.fn = func(_ context.Context, _ string, onLine func(string)) error {
		onLine("info: installing vite-plus@1.7.0...")
		onLine("info: Preparing Node.js and pnpm...")
		p := filepath.Join(vpHome, "bin", "vp.exe")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte("shim"), 0o755)
	}
	job := installViteplus(t, e, z)
	if job.State != "done" || job.RuntimeID == "" {
		t.Fatalf("job %+v", job)
	}
	final := filepath.Join(e.in.Bin, "viteplus-1.7.0-windows-amd64")
	if fake.calls != 1 || fake.program != filepath.Join(final, "vp.exe") || strings.Join(fake.args, " ") != "--version" {
		t.Fatalf("run: calls=%d program=%s args=%v", fake.calls, fake.program, fake.args)
	}
	found := false
	for _, kv := range fake.env {
		if kv == "VP_HOME="+vpHome {
			found = true
		}
	}
	if !found {
		t.Fatalf("VP_HOME=%s missing from %v", vpHome, fake.env)
	}
	if rs := e.rts.List(); len(rs) != 1 || rs[0].Kind != "viteplus" || rs[0].Binary != filepath.Join(final, "vp.exe") || rs[0].Version != "1.7.0" {
		t.Fatalf("runtime %+v", rs)
	}
}

func TestInstallViteplusFailsWhenTheLauncherMakesNothing(t *testing.T) {
	z := makeZip(t, map[string]string{"vp.exe": "LAUNCHER"})
	fake := &fakeRun{fn: func(context.Context, string, func(string)) error { return nil }}
	e := newEnv(t, map[string][]byte{"/vp/vp.zip": z})
	e.in.Run = fake.run
	job := installViteplus(t, e, z)
	if job.State != "failed" || !strings.Contains(job.Error, "was not made") {
		t.Fatalf("job %+v", job)
	}
	if _, err := os.Stat(filepath.Join(e.in.Bin, "viteplus-1.7.0-windows-amd64")); !os.IsNotExist(err) {
		t.Fatal("a failed install removes its folder")
	}
	if len(e.rts.List()) != 0 {
		t.Fatal("nothing is registered")
	}
}

// installViteplus installs the Vite+ entry (the shared test helper forces the kind "node").
func installViteplus(t *testing.T, e *env, z []byte) model.Job {
	t.Helper()
	entry := model.CatalogEntry{ID: "viteplus-1.7.0-windows-amd64", Kind: "viteplus", Version: "1.7.0", Name: "Vite+ 1.7.0",
		URL: e.srv.URL + "/vp/vp.zip", BinaryRelPath: "vp.exe", Checksum: model.Checksum{Type: "sha256", Value: sum(z)},
		OS: runtime.GOOS, Arch: runtime.GOARCH}
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{entry}, nil }
	j, err := e.in.Start(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	return e.wait(t, j.ID)
}
