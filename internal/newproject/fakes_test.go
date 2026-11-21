package newproject

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/frameworks"
	"zero2dev/internal/installer"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/tasks"
)

// ---- fakes

type fakeInst struct {
	t       *testing.T
	st      *store.Store
	bin     string
	catalog []model.CatalogEntry
	fail    map[string]string // catalog id -> job error
	mu      sync.Mutex
	started []string
	jobs    map[string]model.Job
}

func (f *fakeInst) Start(id string, _ ...installer.Options) (model.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var e *model.CatalogEntry
	for i := range f.catalog {
		if f.catalog[i].ID == id {
			e = &f.catalog[i]
		}
	}
	if e == nil {
		return model.Job{}, apperr.NotFound("catalog entry %q not found", id)
	}
	f.started = append(f.started, id)
	jb := model.Job{ID: "job_" + id, Type: "install", State: "done", Percent: 100}
	if msg := f.fail[id]; msg != "" {
		jb.State, jb.Error = "failed", msg
	} else {
		dir := filepath.Join(f.bin, id)
		os.MkdirAll(dir, 0o755)
		binary := filepath.Join(dir, "tool.exe")
		os.WriteFile(binary, []byte("x"), 0o755)
		if e.Kind == "composer" {
			binary = filepath.Join(dir, "composer.bat")
			os.WriteFile(binary, []byte("x"), 0o755)
			os.WriteFile(filepath.Join(dir, "composer.phar"), []byte("x"), 0o644)
		}
		err := f.st.Update(func(d *store.Data) error {
			has := false
			for _, r := range d.Runtimes {
				has = has || r.Kind == e.Kind
			}
			d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_" + id, Kind: e.Kind, Version: e.Version, Name: e.Name, Binary: binary,
				Source: "installed", CatalogID: id, Default: !has})
			return nil
		})
		if err != nil {
			f.t.Fatal(err)
		}
		jb.RuntimeID = "rt_" + id
	}
	f.jobs[jb.ID] = jb
	running := jb
	running.State, running.Percent = "running", 10
	return running, nil
}

func (f *fakeInst) Get(id string) (model.Job, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	return j, ok
}

type fakeDB struct {
	mu                  sync.Mutex
	calls               []string
	createDBErr, userEr error
}

func (f *fakeDB) rec(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakeDB) CreateDatabase(_ context.Context, svc, name string) error {
	f.rec("createdb " + svc + " " + name)
	return f.createDBErr
}
func (f *fakeDB) CreateUser(_ context.Context, svc, user, pw, db string) error {
	f.rec("createuser " + svc + " " + user + " " + db + " pw=" + pw)
	return f.userEr
}
func (f *fakeDB) DropDatabases(_ context.Context, svc string, dbs []string) error {
	f.rec("dropdb " + svc + " " + strings.Join(dbs, ","))
	return nil
}
func (f *fakeDB) DropUser(_ context.Context, svc, user string) error {
	f.rec("dropuser " + svc + " " + user)
	return nil
}

type fakeSup struct {
	mu       sync.Mutex
	binDir   string
	running  map[string]bool
	startErr error
	removed  []string
	prepared []model.Service
	logs     map[string][]string
}

func (f *fakeSup) Environ(svc model.Service) ([]string, error) {
	return []string{"PATH=" + f.binDir, "Z2D_KIND=" + svc.Kind}, nil
}
func (f *fakeSup) PrepareService(svc model.Service) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepared = append(f.prepared, svc)
	return nil
}
func (f *fakeSup) ComposerCommand(svc model.Service) (string, []string, error) {
	return "php-bin", []string{"composer.phar"}, nil
}
func (f *fakeSup) StartWithDeps(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.running[id] = true
	return nil
}
func (f *fakeSup) State(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running[id] {
		return model.StateRunning
	}
	return model.StateStopped
}
func (f *fakeSup) Remove(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	return nil
}
func (f *fakeSup) Log(id, stream, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs[id] = append(f.logs[id], stream+": "+text)
}

type fakeSites struct{ ensured, regenerated int }

func (f *fakeSites) EnsureServers() (bool, error) { f.ensured++; return false, nil }
func (f *fakeSites) Regenerate() error            { f.regenerated++; return nil }

type fakeRun struct {
	mu    sync.Mutex
	cmds  []tasks.Cmd
	hook  func(c tasks.Cmd) error
	lines map[string]string // program base name -> output
}

func (f *fakeRun) run(_ context.Context, c tasks.Cmd) error {
	f.mu.Lock()
	f.cmds = append(f.cmds, c)
	hook := f.hook
	f.mu.Unlock()
	if out := f.lines[strings.TrimSuffix(filepath.Base(c.Bin), ".exe")]; out != "" && c.Out != nil {
		fmt.Fprintln(c.Out, out)
	}
	if hook != nil {
		return hook(c)
	}
	return nil
}

// ---- the test environment

type tenv struct {
	t        *testing.T
	m        *Manager
	home     string
	parent   string
	st       *store.Store
	run      *fakeRun
	inst     *fakeInst
	db       *fakeDB
	sup      *fakeSup
	sites    *fakeSites
	emitted  []string
	launched [][]string
	mu       sync.Mutex
}

// prog makes a fake program that FindProgram finds on the fake PATH.
func (e *tenv) prog(names ...string) {
	for _, n := range names {
		if runtime.GOOS == "windows" {
			n += ".exe"
		}
		os.WriteFile(filepath.Join(e.sup.binDir, n), []byte("x"), 0o755)
	}
}

func catalogEntries() []model.CatalogEntry {
	mk := func(id, kind, version, name string) model.CatalogEntry {
		return model.CatalogEntry{ID: id, Kind: kind, Version: version, Name: name, OS: runtime.GOOS, Arch: runtime.GOARCH}
	}
	return []model.CatalogEntry{
		mk("php-8.1.9", "php", "8.1.9", "PHP 8.1.9"),
		mk("php-8.3.1", "php", "8.3.1", "PHP 8.3.1"),
		mk("php-8.4.2", "php", "8.4.2", "PHP 8.4.2"),
		mk("php-9.0.0-other", "php", "9.0.0", "PHP 9 elsewhere"),
		mk("composer-2.8.0", "composer", "2.8.0", "Composer 2.8.0"),
		mk("mariadb-11.4.0", "mariadb", "11.4.0", "MariaDB 11.4.0"),
		mk("node-22.1.0", "node", "22.1.0", "Node.js 22.1.0"),
		mk("python-3.13.1", "python", "3.13.1", "Python 3.13.1"),
	}
}

// writeFrameworks writes the test list to <home>\frameworks.json.
func writeFrameworks(t *testing.T, home string, list ...frameworks.Framework) {
	t.Helper()
	b, err := json.Marshal(frameworks.File{Format: 1, Frameworks: list})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "frameworks.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func newEnv(t *testing.T, list ...frameworks.Framework) *tenv {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	e := &tenv{t: t, home: home, parent: t.TempDir(), st: st, run: &fakeRun{lines: map[string]string{}}, db: &fakeDB{}, sites: &fakeSites{}}
	e.sup = &fakeSup{binDir: t.TempDir(), running: map[string]bool{}, logs: map[string][]string{}}
	e.inst = &fakeInst{t: t, st: st, bin: t.TempDir(), catalog: catalogEntries(), fail: map[string]string{}, jobs: map[string]model.Job{}}
	if len(list) > 0 {
		writeFrameworks(t, home, list...)
	}
	e.m = &Manager{
		Home: home, St: st, Sup: e.sup, Installer: e.inst, Sites: e.sites, DB: e.db, Jobs: jobs.New(nil),
		Run: e.run.run, Catalog: func() ([]model.CatalogEntry, error) { return e.inst.catalog, nil },
		PortFree: func(int) bool { return true }, PollEvery: time.Millisecond, StartTimeout: 50 * time.Millisecond, InstallWait: 2 * time.Second,
		Emit: func(typ string, _ any) { e.mu.Lock(); e.emitted = append(e.emitted, typ); e.mu.Unlock() },
		Launch: func(bin string, args []string, dir string, env []string) error {
			e.mu.Lock()
			e.launched = append(e.launched, append([]string{bin, dir}, args...))
			e.mu.Unlock()
			return nil
		},
		Rand: func(n int) []byte {
			b := make([]byte, n)
			for i := range b {
				b[i] = byte(i*7 + 3)
			}
			return b
		},
	}
	return e
}

// install puts a runtime (with a real file) in the store, as if it was installed before.
func (e *tenv) install(kind, version string, def bool) model.Runtime {
	e.t.Helper()
	dir := filepath.Join(e.inst.bin, kind+version)
	os.MkdirAll(dir, 0o755)
	binary := filepath.Join(dir, "tool.exe")
	os.WriteFile(binary, []byte("x"), 0o755)
	rt := model.Runtime{ID: "rt_" + kind + version, Kind: kind, Version: version, Name: kind + " " + version, Binary: binary, Source: "installed", Default: def}
	if kind == "composer" {
		os.WriteFile(filepath.Join(dir, "composer.phar"), []byte("x"), 0o644)
	}
	if err := e.st.Update(func(d *store.Data) error { d.Runtimes = append(d.Runtimes, rt); return nil }); err != nil {
		e.t.Fatal(err)
	}
	return rt
}

func (e *tenv) addService(sv model.Service) {
	e.t.Helper()
	if sv.Settings == nil {
		sv.Settings = map[string]string{}
	}
	if err := e.st.Update(func(d *store.Data) error { d.Services = append(d.Services, sv); return nil }); err != nil {
		e.t.Fatal(err)
	}
}

func (e *tenv) req(fw string) Request {
	return Request{Framework: fw, Name: "My Shop", Parent: e.parent}
}

// finish waits for a job and returns it.
func (e *tenv) finish(j model.Job) model.Job {
	e.t.Helper()
	for range 2000 {
		cur, _ := e.m.Jobs.Get(j.ID)
		if cur.State != "running" {
			return cur
		}
		time.Sleep(time.Millisecond)
	}
	e.t.Fatal("the job did not finish")
	return model.Job{}
}

func (e *tenv) start(req Request) model.Job {
	e.t.Helper()
	j, err := e.m.Start(req)
	if err != nil {
		e.t.Fatalf("start: %v", err)
	}
	return e.finish(j)
}

func (e *tenv) calls() []string {
	e.db.mu.Lock()
	defer e.db.mu.Unlock()
	return append([]string(nil), e.db.calls...)
}

// ---- test frameworks

func phpApp() frameworks.Framework {
	return frameworks.Framework{
		ID: "app", Label: "App", Language: "php", Runtime: frameworks.Runtime{Kind: "php", Min: "8.2"}, Composer: true,
		Database: &frameworks.Database{Kinds: []string{"mariadb", "mysql"}, Default: "mariadb", Optional: true},
		Steps: []frameworks.Step{
			{Type: "run", Label: "Creating the app", Program: "composer", Args: []string{"create-project", "x/app", "{slug}"}, In: "parent"},
			{Type: "files", Label: "Writing index.php", Files: map[string]string{"public/index.php": "<?php echo '{name}';\n", "README.txt": "{slug} on {domain}\n"}},
		},
		Services: []frameworks.ServiceTpl{{Name: "{name}", Kind: "php", Port: 8000, UsesDatabase: true, Settings: map[string]string{"docroot": "public"}}},
		Site:     &frameworks.SiteTpl{Type: "php", Root: "public", HTTPS: true},
		Env: []frameworks.EnvLine{
			{Key: "APP_KEY", Value: "base64:{appKey}", IfMissing: true},
			{Key: "APP_URL", Value: "https://{domain}"},
			{Key: "DB_DATABASE", Value: "{db}"},
			{Key: "DB_USERNAME", Value: "{dbUser}"},
			{Key: "DB_PASSWORD", Value: "{dbPassword}"},
			{Key: "DB_PORT", Value: "{dbPort}"},
		},
		EnvFile: ".env",
	}
}

func nodeApp() frameworks.Framework {
	return frameworks.Framework{
		ID: "nodeapp", Label: "Node app", Language: "javascript", Runtime: frameworks.Runtime{Kind: "node", Min: "20"},
		Steps: []frameworks.Step{
			{Type: "files", Label: "Writing package.json", Files: map[string]string{"package.json": "{\"name\":\"{slug}\"}\n"}},
			{Type: "run", Label: "Installing the packages", Program: "npm", Args: []string{"install"}, In: "project", Env: map[string]string{"CI": "1"}},
		},
		Services: []frameworks.ServiceTpl{{Name: "{name}", Kind: "node", Port: 3000, Settings: map[string]string{"run": "npm", "npmScript": "dev"}}},
		Site:     &frameworks.SiteTpl{Type: "proxy", HTTPS: true},
	}
}

func pyApp() frameworks.Framework {
	return frameworks.Framework{
		ID: "pyapp", Label: "Py app", Language: "python", Runtime: frameworks.Runtime{Kind: "python", Min: "3.10"},
		Steps: []frameworks.Step{
			{Type: "files", Label: "Writing requirements.txt", Files: map[string]string{"requirements.txt": "flask\n# {venv}\n"}},
			{Type: "run", Label: "Installing the packages", Program: "pip", Args: []string{"install", "-r", "requirements.txt"}, In: "project"},
		},
		Services: []frameworks.ServiceTpl{{Name: "{name}", Kind: "python", Port: 5000, Settings: map[string]string{"run": "flask", "flaskApp": "app"}}},
		Site:     &frameworks.SiteTpl{Type: "proxy"},
	}
}

func htmlApp() frameworks.Framework {
	return frameworks.Framework{
		ID: "html", Label: "HTML", Language: "static", Runtime: frameworks.Runtime{Kind: ""},
		Steps: []frameworks.Step{{Type: "files", Label: "Writing the page", Files: map[string]string{"index.html": "<h1>{name}</h1>\n"}}},
		Site:  &frameworks.SiteTpl{Type: "static", Root: ".", HTTPS: true},
	}
}

// zipServer serves pkg.zip (files below one folder "wp/") and its sha1 as pkg.zip.sha1.
func zipServer(t *testing.T, badSum bool) (*httptest.Server, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"wp/index.php": "<?php // wordpress\n", "wp/wp-content/readme.txt": "hi\n"} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	sum := sha1.Sum(buf.Bytes())
	hexSum := hex.EncodeToString(sum[:])
	if badSum {
		hexSum = strings.Repeat("0", 40)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/pkg.zip", func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) })
	mux.HandleFunc("/pkg.zip.sha1", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "%s  pkg.zip\n", hexSum) })
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv, srv.URL
}

var errBoom = errors.New("boom")
