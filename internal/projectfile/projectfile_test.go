package projectfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/installer"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// ---- fakes

type fakeInst struct {
	t       *testing.T
	st      *store.Store
	bin     string
	catalog []model.CatalogEntry
	fail    map[string]string
	hook    func(id string) // runs when an install starts
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
	if f.hook != nil {
		f.hook(id)
	}
	jb := model.Job{ID: "job_" + id, Type: "install", State: "done", Percent: 100}
	if msg := f.fail[id]; msg != "" {
		jb.State, jb.Error = "failed", msg
	} else {
		dir := filepath.Join(f.bin, id)
		_ = os.MkdirAll(dir, 0o755)
		binary := filepath.Join(dir, "tool.exe")
		_ = os.WriteFile(binary, []byte("x"), 0o755)
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

type fakeSites struct{ ensured, regenerated int }

func (f *fakeSites) EnsureServers() (bool, error) { f.ensured++; return false, nil }
func (f *fakeSites) Regenerate() error            { f.regenerated++; return nil }

type fakeLog struct {
	mu    sync.Mutex
	lines []string
}

func (f *fakeLog) Log(id, stream, text string) {
	f.mu.Lock()
	f.lines = append(f.lines, id+" "+stream+" "+text)
	f.mu.Unlock()
}

type tenv struct {
	t       *testing.T
	home    string
	proj    string // the project's folder (the folder of the file)
	st      *store.Store
	inst    *fakeInst
	sites   *fakeSites
	log     *fakeLog
	m       *Manager
	mu      sync.Mutex
	emitted []string
	taken   map[int]bool // ports that "another program" holds
}

func catalogEntries() []model.CatalogEntry {
	mk := func(id, kind, version string) model.CatalogEntry {
		return model.CatalogEntry{ID: id, Kind: kind, Version: version, Name: kind + " " + version, OS: runtime.GOOS, Arch: runtime.GOARCH}
	}
	return []model.CatalogEntry{mk("php-8.1.9", "php", "8.1.9"), mk("php-8.3.1", "php", "8.3.1"), mk("node-22.1.0", "node", "22.1.0"), mk("mariadb-11.4.0", "mariadb", "11.4.0")}
}

func newEnv(t *testing.T) *tenv {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	e := &tenv{t: t, home: home, proj: filepath.Join(t.TempDir(), "shop"), st: st, sites: &fakeSites{}, log: &fakeLog{}, taken: map[int]bool{}}
	if err := os.MkdirAll(filepath.Join(e.proj, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.inst = &fakeInst{t: t, st: st, bin: t.TempDir(), catalog: catalogEntries(), fail: map[string]string{}, jobs: map[string]model.Job{}}
	e.m = &Manager{Home: home, St: st, Sup: e.log, Installer: e.inst, Sites: e.sites, Jobs: jobs.New(nil),
		Catalog:  func() ([]model.CatalogEntry, error) { return e.inst.catalog, nil },
		PortFree: func(p int) bool { return !e.taken[p] }, PollEvery: time.Millisecond, InstallWait: 2 * time.Second,
		Emit: func(typ string, _ any) { e.mu.Lock(); e.emitted = append(e.emitted, typ); e.mu.Unlock() }}
	return e
}

// install puts a runtime with a real file into the store.
func (e *tenv) install(kind, version string, def bool) model.Runtime {
	e.t.Helper()
	dir := filepath.Join(e.inst.bin, kind+version)
	_ = os.MkdirAll(dir, 0o755)
	binary := filepath.Join(dir, "tool.exe")
	_ = os.WriteFile(binary, []byte("x"), 0o755)
	rt := model.Runtime{ID: "rt_" + kind + version, Kind: kind, Version: version, Name: kind + " " + version, Binary: binary, Source: "installed", Default: def}
	if err := e.st.Update(func(d *store.Data) error { d.Runtimes = append(d.Runtimes, rt); return nil }); err != nil {
		e.t.Fatal(err)
	}
	return rt
}

func (e *tenv) mkdir(rel string) string {
	e.t.Helper()
	p := filepath.Join(e.proj, filepath.FromSlash(rel))
	if err := os.MkdirAll(p, 0o755); err != nil {
		e.t.Fatal(err)
	}
	return p
}

// shop fills the store with a project that has every kind of thing the file can hold, and returns
// the project's id. Secrets are placed all over it on purpose.
func (e *tenv) shop() string {
	e.t.Helper()
	php := e.install("php", "8.3.1", true)
	e.install("mariadb", "11.4.0", true)
	worker := e.mkdir("worker")
	other := filepath.Join(e.t.TempDir(), "elsewhere")
	_ = os.MkdirAll(other, 0o755)
	err := e.st.Update(func(d *store.Data) error {
		d.Services = []model.Service{
			{ID: "svc_db", Name: "MariaDB", Kind: "mariadb", Port: 3306, AdminPassword: "rootpw-s3cret", Settings: map[string]string{}, DependsOn: []string{}, RestartLimit: model.DefaultRestartLimit},
			{ID: "svc_app", Name: "My Shop", Kind: "php", Cwd: e.proj, Port: 8000, Settings: map[string]string{"docroot": "public"}, RuntimeID: php.ID,
				Env: map[string]string{"APP_ENV": "local", "DB_PASSWORD": "pw1", "api_key": "k1", "Github_Token": "t", "SESSION_LIFETIME": "120", "MY_AUTH_URL": "x",
					"PRIVATE_THING": "p", "X_SIGNATURE": "s", "COOKIE_NAME": "c", "DB_CREDENTIAL": "c", "APP_SECRET": "s", "DATA_DIR": `C:\data\shop`, "BASE_PATH": "/api"},
				EnvFile: ".env", DependsOn: []string{"svc_db"}, DatabaseID: "svc_db", Autostart: true, RestartOnCrash: true, RestartLimit: 3, HealthPath: "/up"},
			{ID: "svc_worker", Name: "Worker", Kind: "node", Cwd: worker, Port: 3000, Settings: map[string]string{"script": "dev"}, DependsOn: []string{"svc_app"}, RestartLimit: model.DefaultRestartLimit},
			{ID: "svc_far", Name: "Far away", Kind: "node", Cwd: other, Port: 3001, Settings: map[string]string{"script": "dev"}, DependsOn: []string{}, RestartLimit: model.DefaultRestartLimit},
			{ID: "svc_task", Name: "Task", Kind: "custom", Command: `C:\tools\run.exe`, Cwd: e.proj, Settings: map[string]string{}, DependsOn: []string{}},
			{ID: "svc_outside", Name: "Not in project", Kind: "node", Cwd: worker, Settings: map[string]string{}, DependsOn: []string{}},
		}
		d.Sites = []model.Site{
			{ID: "site_app", Name: "Shop", Domain: "shop.localhost", Type: "php", ServiceID: "svc_app", HTTPS: true, Enabled: true},
			{ID: "site_static", Name: "Docs", Domain: "docs.localhost", Type: "static", Root: filepath.Join(e.proj, "public"), Enabled: false},
			{ID: "site_far", Name: "Far", Domain: "far.localhost", Type: "proxy", ServiceID: "svc_far", Enabled: true},
			{ID: "site_ext", Name: "Ext", Domain: "ext.test", Type: "proxy", UpstreamPort: 9000, Enabled: true},
		}
		d.Projects = []model.Project{{ID: "prj_shop", Name: "My Shop", ServiceIDs: []string{"svc_db", "svc_app", "svc_worker", "svc_far", "svc_task"},
			SiteIDs: []string{"site_app", "site_static", "site_far", "site_ext"}}}
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return "prj_shop"
}

func readFileT(t *testing.T, p string) File {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return f
}

func (e *tenv) write(f File) string {
	e.t.Helper()
	b, _ := json.MarshalIndent(f, "", "  ")
	p := filepath.Join(e.proj, FileName)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *tenv) finish(j model.Job) model.Job {
	e.t.Helper()
	for range 3000 {
		cur, _ := e.m.Jobs.Get(j.ID)
		if cur.State != "running" {
			return cur
		}
		time.Sleep(time.Millisecond)
	}
	e.t.Fatal("the job did not finish")
	return model.Job{}
}

func hasLine(lines []string, sub string) bool {
	return slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, sub) })
}

// ---- export

func TestExportSchemaAndNeverWritten(t *testing.T) {
	e := newEnv(t)
	id := e.shop()
	res, err := e.m.Export(id, ExportRequest{Path: e.proj}) // a folder: <folder>\zero2dev.json
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != filepath.Join(e.proj, FileName) {
		t.Fatalf("path %s", res.Path)
	}
	raw, _ := os.ReadFile(res.Path)
	text := string(raw)
	if !strings.Contains(text, "\n  \"format\": 1,") {
		t.Fatalf("not indented JSON:\n%s", text)
	}
	// nothing that belongs to this PC or is secret
	for _, bad := range []string{"svc_", "site_", "prj_", "rt_", "pw1", "k1\"", "rootpw", "s3cret", `C:\`, "C:/", e.proj, filepath.ToSlash(e.proj),
		"DB_PASSWORD", "api_key", "Github_Token", "SESSION_LIFETIME", "MY_AUTH_URL", "PRIVATE_THING", "X_SIGNATURE", "COOKIE_NAME", "DB_CREDENTIAL", "APP_SECRET",
		"DATA_DIR", "adminPassword", "binary", "catalogId", "tool.exe", "elsewhere"} {
		if strings.Contains(text, bad) {
			t.Errorf("the file contains %q:\n%s", bad, text)
		}
	}
	f := readFileT(t, res.Path)
	if f.Format != 1 || f.Name != "My Shop" {
		t.Fatalf("%+v", f)
	}
	var refs []string
	for _, s := range f.Services {
		refs = append(refs, s.Ref)
	}
	// Far away (outside the folder) and Task (absolute command) are left out; refs come from names
	if !slices.Equal(refs, []string{"mariadb", "my-shop", "worker"}) {
		t.Fatalf("refs %v", refs)
	}
	db, app, worker := f.Services[0], f.Services[1], f.Services[2]
	if db.Cwd != "" || db.Port != 3306 || db.Runtime != nil {
		t.Fatalf("db %+v", db)
	}
	if app.Cwd != "." || app.Port != 8000 || app.Settings["docroot"] != "public" || app.EnvFile != ".env" || app.Health != "/up" ||
		!app.Autostart || !app.RestartOnCrash || app.RestartLimit != 3 || app.Database != "mariadb" || !slices.Equal(app.DependsOn, []string{"mariadb"}) {
		t.Fatalf("app %+v", app)
	}
	if app.Runtime == nil || *app.Runtime != (Runtime{Kind: "php", Min: "8.3"}) {
		t.Fatalf("app runtime %+v", app.Runtime)
	}
	if !slices.Equal(sortedEnv(app.Env), []string{"APP_ENV", "BASE_PATH"}) {
		t.Fatalf("env %v", app.Env)
	}
	if worker.Cwd != "worker" || !slices.Equal(worker.DependsOn, []string{"my-shop"}) {
		t.Fatalf("worker %+v", worker)
	}
	if !slices.Equal(f.Runtimes, []Runtime{{Kind: "mariadb"}, {Kind: "php", Min: "8.3"}, {Kind: "node"}}) {
		t.Fatalf("runtimes %+v", f.Runtimes)
	}
	// sites: label before the suffix; the site of the skipped service is left out
	if len(f.Sites) != 3 || f.Sites[0].Domain != "shop" || f.Sites[0].Service != "my-shop" || !f.Sites[0].HTTPS ||
		f.Sites[1].Domain != "docs" || f.Sites[1].Root != "public" || f.Sites[1].Enabled || f.Sites[2].Domain != "ext" || f.Sites[2].UpstreamPort != 9000 {
		t.Fatalf("sites %+v", f.Sites)
	}
	// every skipped thing is named
	for _, want := range []string{"DB_PASSWORD of My Shop", "api_key of My Shop", "Github_Token of My Shop", "SESSION_LIFETIME", "MY_AUTH_URL", "PRIVATE_THING", "X_SIGNATURE",
		"COOKIE_NAME", "DB_CREDENTIAL", "APP_SECRET", "DATA_DIR of My Shop was not written: its value is an absolute path", "administrator password of MariaDB",
		"The folder of Far away is outside", "Task was left out", "site far.localhost"} {
		if !hasLine(res.Warnings, want) {
			t.Errorf("no warning with %q in %v", want, res.Warnings)
		}
	}
	if hasLine(res.Warnings, "pw1") || hasLine(res.Warnings, "rootpw") {
		t.Fatalf("a warning carries a secret: %v", res.Warnings)
	}
	// stable: the same state gives the same bytes
	if _, err := e.m.Export(id, ExportRequest{Path: res.Path, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(res.Path); string(again) != text {
		t.Fatal("the export is not stable")
	}
}

func sortedEnv(m map[string]string) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	slices.Sort(k)
	return k
}

func TestExportRefsAreUniqueAndFileSubfolder(t *testing.T) {
	e := newEnv(t)
	e.mkdir("a")
	e.mkdir("b")
	err := e.st.Update(func(d *store.Data) error {
		for i, n := range []string{"API", "api", "!!!"} {
			d.Services = append(d.Services, model.Service{ID: "svc_" + string(rune('a'+i)), Name: n, Kind: "node", Cwd: filepath.Join(e.proj, "a"), Settings: map[string]string{}})
		}
		d.Projects = []model.Project{{ID: "prj_1", Name: "P", ServiceIDs: []string{"svc_a", "svc_b", "svc_c"}, SiteIDs: []string{}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// the file sits in a subfolder: the folder of the file is the base
	res, err := e.m.Export("prj_1", ExportRequest{Path: filepath.Join(e.proj, "a", "my.json")})
	if err != nil {
		t.Fatal(err)
	}
	f := readFileT(t, res.Path)
	if f.Services[0].Ref != "api" || f.Services[1].Ref != "api-2" || f.Services[2].Ref != "node" || f.Services[0].Cwd != "." {
		t.Fatalf("%+v", f.Services)
	}
	// a project whose folders are all outside the base writes no service
	res, err = e.m.Export("prj_1", ExportRequest{Path: filepath.Join(e.proj, "b", FileName)})
	if err != nil {
		t.Fatal(err)
	}
	if f := readFileT(t, res.Path); len(f.Services) != 0 || len(res.Warnings) != 3 {
		t.Fatalf("%+v %v", f, res.Warnings)
	}
}

func TestExportErrorsAndAtomicOverwrite(t *testing.T) {
	e := newEnv(t)
	id := e.shop()
	var ae *apperr.Error
	if _, err := e.m.Export("prj_nope", ExportRequest{Path: e.proj}); !asErr(err, &ae) || ae.Status != 404 {
		t.Fatalf("unknown project: %v", err)
	}
	for _, p := range []string{"", "relative/zero2dev.json", filepath.Join(e.proj, "nofolder", FileName)} {
		if _, err := e.m.Export(id, ExportRequest{Path: p}); !asErr(err, &ae) || ae.Status != 400 {
			t.Fatalf("path %q: %v", p, err)
		}
	}
	target := filepath.Join(e.proj, FileName)
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Export(id, ExportRequest{Path: target}); !asErr(err, &ae) || ae.Status != 409 || ae.Message != target+" exists" {
		t.Fatalf("exists: %v", err)
	}
	if _, err := e.m.Export(id, ExportRequest{Path: e.proj}); !asErr(err, &ae) || ae.Status != 409 {
		t.Fatalf("folder with a file: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "old" {
		t.Fatal("the existing file was changed without overwrite")
	}
	if _, err := e.m.Export(id, ExportRequest{Path: target, Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if f := readFileT(t, target); f.Name != "My Shop" {
		t.Fatalf("%+v", f)
	}
	// no temp file is left next to it
	entries, _ := os.ReadDir(e.proj)
	for _, en := range entries {
		if strings.Contains(en.Name(), ".tmp") {
			t.Fatalf("temp file %s left", en.Name())
		}
	}
}

func asErr(err error, out **apperr.Error) bool {
	ae, ok := err.(*apperr.Error)
	if ok {
		*out = ae
	}
	return ok
}

// ---- import: plan

func (e *tenv) plan(path string) Plan {
	e.t.Helper()
	p, err := e.m.Plan(ImportRequest{Path: path})
	if err != nil {
		e.t.Fatalf("plan: %v", err)
	}
	return p
}

func simple() File {
	return File{Format: 1, Name: "Demo", Runtimes: []Runtime{{Kind: "node"}}, Services: []Service{
		{Ref: "web", Name: "Web", Kind: "node", Cwd: ".", Port: 3000, Settings: map[string]string{"script": "dev"}}}, Sites: []Site{
		{Name: "Demo", Domain: "demo", Type: "proxy", Service: "web", Enabled: true}}}
}

func TestPlanProblems(t *testing.T) {
	e := newEnv(t)
	e.install("node", "22.1.0", true)
	e.mkdir("api")
	cases := []struct {
		name string
		mod  func(f *File)
		want string
	}{
		{"format", func(f *File) { f.Format = 2 }, "has format 2"},
		{"name", func(f *File) { f.Name = " " }, "name must be 1-64"},
		{"runtime kind", func(f *File) { f.Runtimes = []Runtime{{Kind: "cobol"}} }, `"cobol" is not a runtime kind`},
		{"runtime version", func(f *File) { f.Runtimes = []Runtime{{Kind: "php", Min: "eight"}} }, "must look like 8.2"},
		{"service kind", func(f *File) { f.Services[0].Kind = "ruby" }, `"ruby" is not a service kind`},
		{"no ref", func(f *File) { f.Services[0].Ref = "" }, "has no ref"},
		{"dup ref", func(f *File) { f.Services = append(f.Services, Service{Ref: "web", Name: "B", Kind: "node", Cwd: "."}) }, `"web" is used by more than one`},
		{"dependsOn", func(f *File) { f.Services[0].DependsOn = []string{"ghost"} }, `depends on "ghost"`},
		{"database", func(f *File) { f.Services[0].Database = "ghost" }, `database "ghost"`},
		{"database kind", func(f *File) { f.Services[0].Database = "web" }, "not a database service"},
		{"site service", func(f *File) { f.Sites[0].Service = "ghost" }, `points at "ghost"`},
		{"php site", func(f *File) { f.Sites[0].Type = "php" }, "not a php service"},
		{"site type", func(f *File) { f.Sites[0].Type = "ftp" }, `type "ftp"`},
		{"domain", func(f *File) { f.Sites[0].Domain = "no spaces" }, "is not usable"},
		{"absolute cwd", func(f *File) { f.Services[0].Cwd = `C:\Windows` }, "is not a relative path"},
		{"cwd outside", func(f *File) { f.Services[0].Cwd = "../x" }, "outside the folder of the project file"},
		{"cwd missing", func(f *File) { f.Services[0].Cwd = "nope" }, "does not exist"},
		{"cwd required", func(f *File) { f.Services[0].Cwd = "" }, "needs a folder"},
		{"static root", func(f *File) {
			f.Sites[0] = Site{Name: "S", Domain: "s", Type: "static", Root: "missing", Enabled: true}
		}, "of the site S does not exist"},
		{"port", func(f *File) { f.Services[0].Port = 70000 }, "port must be 0-65535"},
		{"envFile", func(f *File) { f.Services[0].EnvFile = "../.env" }, "envFile must be a path inside"},
		{"setting", func(f *File) { f.Services[0].Settings = map[string]string{"nonsense": "1"} }, "unknown setting"},
		{"loop", func(f *File) {
			f.Services = []Service{{Ref: "a", Name: "A", Kind: "node", Cwd: ".", DependsOn: []string{"b"}}, {Ref: "b", Name: "B", Kind: "node", Cwd: ".", DependsOn: []string{"a"}}}
			f.Sites = nil
		}, "depend on each other in a loop: a -> b -> a"},
		{"site without port", func(f *File) { f.Services[0].Port = 0 }, "has no port"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := simple()
			c.mod(&f)
			p := e.plan(e.write(f))
			if len(p.Problems) == 0 || !hasLine(p.Problems, c.want) {
				t.Fatalf("want a problem with %q, got %v", c.want, p.Problems)
			}
			var ae *apperr.Error
			_, err := e.m.Start(ImportRequest{Path: e.proj})
			if !asErr(err, &ae) || ae.Status != 409 || ae.Message != p.Problems[0] {
				t.Fatalf("start: %v (first problem %q)", err, p.Problems[0])
			}
		})
	}
	if st := e.st.Snapshot(); len(st.Projects) != 0 || len(st.Services) != 0 {
		t.Fatal("a refused import created something")
	}
}

func TestPlanFileProblems(t *testing.T) {
	e := newEnv(t)
	var ae *apperr.Error
	if _, err := e.m.Plan(ImportRequest{}); !asErr(err, &ae) || ae.Status != 400 {
		t.Fatalf("empty: %v", err)
	}
	if _, err := e.m.Plan(ImportRequest{Path: "rel/zero2dev.json"}); !asErr(err, &ae) || ae.Status != 400 {
		t.Fatalf("relative: %v", err)
	}
	if p := e.plan(e.proj); len(p.Problems) != 1 || !strings.Contains(p.Problems[0], "has no zero2dev.json") {
		t.Fatalf("empty folder: %+v", p)
	}
	if p := e.plan(filepath.Join(e.proj, "x.json")); len(p.Problems) != 1 || !strings.Contains(p.Problems[0], "does not exist") {
		t.Fatalf("missing file: %+v", p)
	}
	if err := os.WriteFile(filepath.Join(e.proj, FileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := e.plan(e.proj); len(p.Problems) != 1 || !strings.Contains(p.Problems[0], "not a valid project file") {
		t.Fatalf("bad json: %+v", p)
	}
	// every list is [] and never null
	var raw map[string]any
	b, _ := json.Marshal(e.plan(e.proj))
	_ = json.Unmarshal(b, &raw)
	for _, k := range []string{"services", "sites", "installs", "changes", "problems", "warnings"} {
		if _, ok := raw[k].([]any); !ok {
			t.Errorf("%s must be a list, got %v", k, raw[k])
		}
	}
}

func TestPlanSummaryInstallsAndChanges(t *testing.T) {
	e := newEnv(t)
	e.install("node", "22.1.0", true)
	e.mkdir("api")
	// this PC: port 3000 is held by another program, 3001 by a service; domain demo.localhost is a site
	_ = e.st.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{ID: "svc_x", Name: "X", Kind: "node", Port: 3001, Settings: map[string]string{}})
		d.Sites = append(d.Sites, model.Site{ID: "site_x", Name: "Old", Domain: "demo.localhost", Type: "proxy", UpstreamPort: 3001, Enabled: true})
		return nil
	})
	e.taken[3000] = true
	f := File{Format: 1, Name: "Demo", Runtimes: []Runtime{{Kind: "php", Min: "8.2"}}, Services: []Service{
		{Ref: "web", Name: "Web", Kind: "node", Cwd: ".", Port: 3000, Settings: map[string]string{"script": "dev"}, Runtime: &Runtime{Kind: "node", Min: "20"}},
		{Ref: "api", Name: "Api", Kind: "php", Cwd: "api", Port: 8000, Settings: map[string]string{"docroot": "public"}, DependsOn: []string{"web"}},
		{Ref: "task", Name: "Task", Kind: "custom", Command: "make", Cwd: "."}},
		Sites: []Site{{Name: "Demo", Domain: "demo", Type: "proxy", Service: "web", HTTPS: true, Enabled: true},
			{Name: "Api", Domain: "demo", Type: "php", Service: "api", Enabled: true},
			{Name: "Ext", Domain: "ext", Type: "proxy", UpstreamPort: 3000, Enabled: true}}}
	p := e.plan(e.write(f))
	if len(p.Problems) != 0 {
		t.Fatalf("%+v", p)
	}
	if p.Path != filepath.Join(e.proj, FileName) || p.Dir != e.proj || p.Name != "Demo" {
		t.Fatalf("%+v", p)
	}
	// 3000 -> 3002 (3001 is a service); the php runtime is missing: the newest one inside the range
	wantSvc := []string{"Web (Node.js, port 3002)", "Api (PHP, port 8000)", "Task (custom)"}
	if !slices.Equal(p.Services, wantSvc) {
		t.Fatalf("services %v", p.Services)
	}
	wantSites := []string{"https://demo-2.localhost -> Web", "http://demo-3.localhost -> Api", "http://ext.localhost -> localhost:3002"}
	if !slices.Equal(p.Sites, wantSites) {
		t.Fatalf("sites %v", p.Sites)
	}
	if !hasLine(p.Changes, "Port 3000 is taken: Web uses 3002") || !hasLine(p.Changes, "Domain demo.localhost is taken: Demo uses demo-2.localhost") ||
		!hasLine(p.Changes, "Domain demo.localhost is taken: Api uses demo-3.localhost") {
		t.Fatalf("changes %v", p.Changes)
	}
	// Node 22.1.0 is installed and fits "20 or newer"; PHP is not installed: the newest PHP
	// of the test catalog inside 8.2+ is 8.3.1
	if len(p.Installs) != 1 || p.Installs[0].CatalogID != "php-8.3.1" || p.Installs[0].Kind != "php" || !strings.Contains(p.Installs[0].Reason, "Api needs PHP 8.2 or newer") {
		t.Fatalf("installs %+v", p.Installs)
	}
	if st := e.st.Snapshot(); len(st.Projects) != 0 || len(st.Services) != 1 || len(e.inst.started) != 0 {
		t.Fatal("a plan changed something")
	}
	// a runtime that cannot be had is a problem
	f.Runtimes = []Runtime{{Kind: "php", Min: "99"}}
	f.Services[1].Runtime = nil
	if p := e.plan(e.write(f)); !hasLine(p.Problems, "No PHP version 99 or newer is available") {
		t.Fatalf("%+v", p)
	}
	// a missing .env is a warning, and the file never lists a secret
	f.Runtimes = nil
	f.Services[1].EnvFile = ".env"
	if p := e.plan(e.write(f)); !hasLine(p.Warnings, "Api reads .env, which does not exist yet") {
		t.Fatalf("%+v", p)
	}
	// the folder of an import is accepted as the path and the name can be replaced
	pp, err := e.m.Plan(ImportRequest{Path: e.proj, Name: "Renamed"})
	if err != nil || pp.Name != "Renamed" {
		t.Fatalf("%v %+v", err, pp)
	}
}

// ---- import: job

func (e *tenv) doImport(path string) model.Job {
	e.t.Helper()
	j, err := e.m.Start(ImportRequest{Path: path})
	if err != nil {
		e.t.Fatalf("start: %v", err)
	}
	return e.finish(j)
}

func TestRoundTripIntoFreshStore(t *testing.T) {
	src := newEnv(t)
	id := src.shop()
	res, err := src.m.Export(id, ExportRequest{Path: src.proj})
	if err != nil {
		t.Fatal(err)
	}
	// a fresh PC: the same folder, a fresh store with the runtimes only, other ports and a domain are free
	dst := newEnv(t)
	dst.proj = src.proj
	dst.install("php", "8.2.1", true)
	dst.install("mariadb", "10.11.0", true)
	dst.install("node", "20.1.0", true)
	before, _ := os.ReadDir(src.proj)
	job := dst.doImport(res.Path)
	if job.State != "done" || job.ProjectID == "" || job.Path != src.proj || job.Type != model.JobProjectImport {
		t.Fatalf("job %+v", job)
	}
	// the project folder is untouched
	if after, _ := os.ReadDir(src.proj); len(after) != len(before) {
		t.Fatal("the import wrote into the project folder")
	}
	st := dst.st.Snapshot()
	if len(st.Projects) != 1 || len(st.Services) != 3 || len(st.Sites) != 3 {
		t.Fatalf("%+v", st)
	}
	pr := st.Projects[0]
	if pr.ID != job.ProjectID || pr.Name != "My Shop" || len(pr.ServiceIDs) != 3 || len(pr.SiteIDs) != 3 {
		t.Fatalf("%+v", pr)
	}
	byName := map[string]model.Service{}
	for _, s := range st.Services {
		byName[s.Name] = s
		if !strings.HasPrefix(s.ID, "svc_") || s.ID == "svc_db" || s.ID == "svc_app" || s.State != model.StateStopped {
			t.Fatalf("service id %q %q", s.ID, s.State)
		}
	}
	db, app, worker := byName["MariaDB"], byName["My Shop"], byName["Worker"]
	if app.Cwd != src.proj || worker.Cwd != filepath.Join(src.proj, "worker") || db.Cwd != "" {
		t.Fatalf("cwd %q %q %q", app.Cwd, worker.Cwd, db.Cwd)
	}
	if app.Port != 8000 || db.Port != 3306 || worker.Port != 3000 {
		t.Fatalf("ports %d %d %d", app.Port, db.Port, worker.Port)
	}
	if app.DatabaseID != db.ID || !slices.Equal(app.DependsOn, []string{db.ID}) || !slices.Equal(worker.DependsOn, []string{app.ID}) {
		t.Fatalf("links %+v", app)
	}
	if app.Settings["docroot"] != "public" || app.Env["APP_ENV"] != "local" || app.Env["DB_PASSWORD"] != "" || app.EnvFile != ".env" ||
		!app.Autostart || !app.RestartOnCrash || app.RestartLimit != 3 || app.HealthPath != "/up" || db.AdminPassword != "" {
		t.Fatalf("app %+v", app)
	}
	// the service that named PHP 8.3 follows the installed 8.2? no: 8.3 is not installed, so PHP 8.3.1 was installed
	if len(dst.inst.started) != 1 || dst.inst.started[0] != "php-8.3.1" {
		t.Fatalf("installs %v", dst.inst.started)
	}
	php := rtOf(st, "php", "8.3.1")
	if app.RuntimeID != php.ID {
		t.Fatalf("runtime %q want %q", app.RuntimeID, php.ID)
	}
	if worker.RuntimeID != "" { // no runtime in the file: follows the default
		t.Fatalf("worker runtime %q", worker.RuntimeID)
	}
	sites := map[string]model.Site{}
	for _, s := range st.Sites {
		sites[s.Name] = s
	}
	if s := sites["Shop"]; s.Domain != "shop.localhost" || s.ServiceID != app.ID || !s.HTTPS || s.Type != "php" {
		t.Fatalf("%+v", s)
	}
	if s := sites["Docs"]; s.Root != filepath.Join(src.proj, "public") || s.Enabled || s.Type != "static" {
		t.Fatalf("%+v", s)
	}
	if s := sites["Ext"]; s.Domain != "ext.localhost" || s.UpstreamPort != 9000 {
		t.Fatalf("%+v", s)
	}
	// events and the site server
	if !slices.Contains(dst.emitted, "services.changed") || !slices.Contains(dst.emitted, "sites.changed") || !slices.Contains(dst.emitted, "projects.changed") || dst.sites.regenerated == 0 {
		t.Fatalf("emitted %v regenerated %d", dst.emitted, dst.sites.regenerated)
	}
	if len(dst.log.lines) != 3 {
		t.Fatalf("log %v", dst.log.lines)
	}
	// export again from the imported project: the same file (modulo nothing: no ids in it)
	res2, err := dst.m.Export(pr.ID, ExportRequest{Path: filepath.Join(src.proj, "again.json")})
	if err != nil {
		t.Fatal(err)
	}
	a, b := readFileT(t, res.Path), readFileT(t, res2.Path)
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(ab) != string(bb) {
		t.Fatalf("round trip differs:\n%s\n%s", ab, bb)
	}
}

func rtOf(d store.Data, kind, version string) model.Runtime {
	for _, r := range d.Runtimes {
		if r.Kind == kind && r.Version == version {
			return r
		}
	}
	return model.Runtime{}
}

func TestImportResolvesPortsDomainsAndRuntimes(t *testing.T) {
	e := newEnv(t)
	e.install("node", "18.0.0", false)
	n22 := e.install("node", "22.1.0", true)
	e.mkdir("api")
	_ = e.st.Update(func(d *store.Data) error {
		d.Sites = append(d.Sites, model.Site{ID: "site_x", Name: "Old", Domain: "demo.localhost", Type: "proxy", UpstreamPort: 4000, Enabled: true})
		d.Services = append(d.Services, model.Service{ID: "svc_x", Name: "X", Kind: "node", Port: 3000, Settings: map[string]string{}})
		return nil
	})
	f := simple()
	f.Services = append(f.Services, Service{Ref: "old", Name: "Old node", Kind: "node", Cwd: "api", Settings: map[string]string{"script": "dev"}, Runtime: &Runtime{Kind: "node", Max: "18"}},
		Service{Ref: "new", Name: "New node", Kind: "node", Cwd: "api", Settings: map[string]string{"script": "dev"}, Runtime: &Runtime{Kind: "node", Min: "22"}})
	f.Runtimes = nil
	job := e.doImport(e.write(f))
	if job.State != "done" {
		t.Fatalf("%+v", job)
	}
	st := e.st.Snapshot()
	by := map[string]model.Service{}
	for _, s := range st.Services {
		by[s.Name] = s
	}
	if by["Web"].Port != 3001 { // 3000 is taken by X
		t.Fatalf("port %d", by["Web"].Port)
	}
	if by["Web"].RuntimeID != "" {
		t.Fatalf("the default runtime is not pinned: %q", by["Web"].RuntimeID)
	}
	if by["Old node"].RuntimeID != "rt_node18.0.0" || by["New node"].RuntimeID != n22.ID {
		t.Fatalf("runtimes %q %q", by["Old node"].RuntimeID, by["New node"].RuntimeID)
	}
	var dom []string
	for _, s := range st.Sites {
		dom = append(dom, s.Domain)
	}
	slices.Sort(dom)
	if !slices.Equal(dom, []string{"demo-2.localhost", "demo.localhost"}) {
		t.Fatalf("domains %v", dom)
	}
	if len(e.inst.started) != 0 {
		t.Fatalf("installed %v although everything fits", e.inst.started)
	}
	// importing the same file again is fine: new ids, other ports and domains
	if job := e.doImport(e.proj); job.State != "done" {
		t.Fatalf("%+v", job)
	}
	if st := e.st.Snapshot(); len(st.Projects) != 2 || len(st.Services) != 7 {
		t.Fatalf("%d projects %d services", len(st.Projects), len(st.Services))
	}
}

func TestImportFailureLeavesNothing(t *testing.T) {
	e := newEnv(t)
	e.mkdir("api")
	f := simple()
	f.Services = append(f.Services, Service{Ref: "api", Name: "Api", Kind: "php", Cwd: "api", Port: 8000})
	f.Sites = nil
	path := e.write(f)

	// 1. an install fails: no service, no project
	e.inst.fail["node-22.1.0"] = "download failed"
	job := e.doImport(path)
	if job.State != "failed" || !strings.Contains(job.Error, "Installing node 22.1.0 failed: download failed") || !strings.HasSuffix(job.Error, "Nothing was created.") {
		t.Fatalf("%+v", job)
	}
	assertEmpty(t, e)
	delete(e.inst.fail, "node-22.1.0")

	// 2. the plan was fine, then a folder disappears while the runtimes install: nothing is created
	// (services and project go in one change, so the first services do not stay)
	e.inst.hook = func(id string) {
		if id == "php-8.3.1" {
			_ = os.RemoveAll(filepath.Join(e.proj, "api"))
		}
	}
	job = e.doImport(path)
	if job.State != "failed" || !strings.Contains(job.Error, "Api") || !strings.HasSuffix(job.Error, "Nothing was created.") {
		t.Fatalf("%+v", job)
	}
	assertEmpty(t, e)
	if len(e.emitted) != 0 {
		t.Fatalf("events after a failure: %v", e.emitted)
	}
}

func assertEmpty(t *testing.T, e *tenv) {
	t.Helper()
	if st := e.st.Snapshot(); len(st.Projects) != 0 || len(st.Services) != 0 || len(st.Sites) != 0 {
		t.Fatalf("a failed import left %d projects %d services %d sites", len(st.Projects), len(st.Services), len(st.Sites))
	}
}

func TestStartWithoutInstallerOrJobs(t *testing.T) {
	e := newEnv(t)
	path := e.write(simple()) // node is not installed
	e.m.Installer = nil
	var ae *apperr.Error
	if _, err := e.m.Start(ImportRequest{Path: path}); !asErr(err, &ae) || ae.Status != 409 {
		t.Fatalf("%v", err)
	}
	e.m.Jobs = nil
	if _, err := e.m.Start(ImportRequest{Path: path}); err == nil {
		t.Fatal("want an error")
	}
}

func TestIsSecretKey(t *testing.T) {
	for _, k := range []string{"DB_PASSWORD", "password", "AppSecret", "GITHUB_TOKEN", "api_key", "AWS_CREDENTIALS", "BASIC_AUTH", "PRIVATE_KEY", "X_SIGNATURE", "COOKIE", "SESSION_ID"} {
		if !IsSecretKey(k) {
			t.Errorf("%s should be secret", k)
		}
	}
	for _, k := range []string{"APP_ENV", "PORT", "DATABASE_URL", "NODE_OPTIONS", "DEBUG"} {
		if IsSecretKey(k) {
			t.Errorf("%s should not be secret", k)
		}
	}
}
