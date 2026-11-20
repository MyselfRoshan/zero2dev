package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/fsutil"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

// The test binary doubles as a service that idles ("idle" mode).
func TestMain(m *testing.M) {
	if os.Getenv("Z2D_HELPER") == "idle" {
		fmt.Println("idle up")
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(m.Run())
}

type recorder struct {
	mu     sync.Mutex
	events []string
	data   []any
}

func (r *recorder) Publish(typ string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, typ)
	r.data = append(r.data, data)
}

func (r *recorder) has(typ string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.events, typ)
}

type fakeSites struct {
	mu sync.Mutex
	n  int
}

func (f *fakeSites) Regenerate() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return nil
}

func (f *fakeSites) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

type tenv struct {
	t     *testing.T
	home  string
	st    *store.Store
	sup   *supervisor.Supervisor
	reg   *jobs.Registry
	m     *Manager
	sites *fakeSites
	ev    *recorder
}

const (
	dbID     = "svc_db"
	phpID    = "svc_php"
	appID    = "svc_app"
	workerID = "svc_worker"
	prjID    = "prj_shop"
	siteID   = "site_shop"
)

// newEnv builds a home with a project Shop: a MariaDB service (with a data
// folder), a PHP service (with a php.ini), an idle helper app and a worker
// that depends on the app and uses the database, and one php site.
func newEnv(t *testing.T) *tenv {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	ev := &recorder{}
	sup := supervisor.New(home, st, ev)
	sup.KillGrace = time.Second
	sup.HealthTimeout = 10 * time.Second
	reg := jobs.New(ev)
	fs := &fakeSites{}
	cat := func() ([]model.CatalogEntry, error) {
		return []model.CatalogEntry{{ID: "mariadb-11.4.4", Kind: "mariadb", Version: "11.4.4", OS: runtime.GOOS, Arch: runtime.GOARCH}}, nil
	}
	m := New(home, "test", st, sup, reg, fs, ev.Publish, cat)
	m.portFree = func(int) bool { return true }
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sup.StopAll(ctx)
	})
	e := &tenv{t: t, home: home, st: st, sup: sup, reg: reg, m: m, sites: fs, ev: ev}
	cwd := t.TempDir()
	e.update(func(d *store.Data) {
		d.Runtimes = append(d.Runtimes,
			model.Runtime{ID: "rt_maria", Kind: "mariadb", Version: "11.4.4", CatalogID: "mariadb-11.4.4", Binary: filepath.Join(home, "bin", "mariadb-11.4.4", "bin", "mariadbd.exe"), Default: true},
			model.Runtime{ID: "rt_php", Kind: "php", Version: "8.3.14", CatalogID: "php-8.3.14", Binary: filepath.Join(home, "bin", "php-8.3.14", "php-cgi.exe"), Default: true},
		)
		d.Services = append(d.Services,
			model.Service{ID: dbID, Name: "Shop DB", Kind: "mariadb", Port: 3306, AdminPassword: "secret"},
			model.Service{ID: phpID, Name: "Shop PHP", Kind: "php", Port: 9000, Cwd: cwd},
			model.Service{ID: appID, Name: "Shop App", Kind: "custom", Command: os.Args[0], Args: []string{"-test.run=^$"}, Cwd: cwd,
				Env: map[string]string{"Z2D_HELPER": "idle"}},
			model.Service{ID: workerID, Name: "Shop Worker", Kind: "custom", Command: "worker.exe", Cwd: cwd, Port: 5000, DependsOn: []string{appID}, DatabaseID: dbID},
		)
		d.Sites = append(d.Sites, model.Site{ID: siteID, Name: "Shop", Domain: "shop.localhost", Type: "php", ServiceID: phpID, Enabled: true})
		d.Projects = append(d.Projects, model.Project{ID: prjID, Name: "Shop", ServiceIDs: []string{dbID, phpID, appID, workerID}, SiteIDs: []string{siteID}})
	})
	e.write(filepath.Join(sup.DataDir("mariadb", dbID), "ibdata1"), "v1")
	e.write(filepath.Join(sup.DataDir("mariadb", dbID), "shop", "t.ibd"), "table v1")
	e.write(m.phpIni(phpID), "ini v1")
	return e
}

func (e *tenv) update(fn func(d *store.Data)) {
	e.t.Helper()
	if err := e.st.Update(func(d *store.Data) error { fn(d); return nil }); err != nil {
		e.t.Fatal(err)
	}
}

func (e *tenv) write(path, content string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *tenv) read(path string) string {
	e.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(b)
}

func (e *tenv) service(id string) (model.Service, bool) {
	for _, sv := range e.st.Snapshot().Services {
		if sv.ID == id {
			return sv, true
		}
	}
	return model.Service{}, false
}

func (e *tenv) project(id string) (model.Project, bool) {
	for _, p := range e.st.Snapshot().Projects {
		if p.ID == id {
			return p, true
		}
	}
	return model.Project{}, false
}

// wait waits for a job to end and returns it.
func (e *tenv) wait(j model.Job, err error) model.Job {
	e.t.Helper()
	if err != nil {
		e.t.Fatalf("job did not start: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		cur, ok := e.reg.Get(j.ID)
		if !ok {
			e.t.Fatalf("job %s unknown", j.ID)
		}
		if cur.State != "running" {
			return cur
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("job %s did not finish", j.ID)
	return model.Job{}
}

func (e *tenv) done(j model.Job, err error) model.Job {
	e.t.Helper()
	got := e.wait(j, err)
	if got.State != "done" {
		e.t.Fatalf("job %s %s: %s", got.Type, got.State, got.Error)
	}
	return got
}

func (e *tenv) take(name string) Manifest {
	e.t.Helper()
	j := e.done(e.m.Take(ScopeProject, prjID, TakeOptions{Name: name}))
	d, err := e.m.Get(j.SnapshotID)
	if err != nil {
		e.t.Fatal(err)
	}
	return d.Manifest
}

func wantStatus(t *testing.T, err error, status int, contains string) *apperr.Error {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Status != status {
		t.Fatalf("want %d, got %v", status, err)
	}
	if contains != "" && !strings.Contains(ae.Message, contains) {
		t.Fatalf("message %q does not contain %q", ae.Message, contains)
	}
	return ae
}

func (e *tenv) startApp() {
	e.t.Helper()
	if err := e.sup.Start(appID); err != nil {
		e.t.Fatal(err)
	}
	e.waitState(appID, model.StateRunning)
}

func (e *tenv) waitState(id, state string) {
	e.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for e.sup.State(id) != state {
		if time.Now().After(deadline) {
			e.t.Fatalf("%s is %s, want %s", id, e.sup.State(id), state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	mf := Manifest{
		Format: Format, ID: "snp_1a2b3c4d", Name: "Before the upgrade", Scope: ScopeProject, OwnerID: "prj_1", CreatedAt: now,
		Version: "0.0.5", SizeBytes: 42, Project: &model.Project{ID: "prj_1", Name: "Shop", ServiceIDs: []string{"svc_1"}, SiteIDs: []string{}},
		Sites: []model.Site{{ID: "site_1", Name: "Shop", Domain: "shop.localhost", Type: "proxy", UpstreamPort: 3000, Enabled: true}},
		Services: []ServiceEntry{{
			Service: model.Service{ID: "svc_1", Name: "DB", Kind: "mariadb", Args: []string{}, Env: map[string]string{}, Settings: map[string]string{"x": "y"}, DependsOn: []string{}, AdminPassword: "pw"},
			Runtime: &RuntimeRef{Kind: "mariadb", Version: "11.4.4", CatalogID: "mariadb-11.4.4", Binary: `C:\z\mariadbd.exe`, FollowedDefault: true},
			HasData: true,
		}},
		Warnings: []string{"w"},
	}
	if err := writeManifest(dir, mf); err != nil {
		t.Fatal(err)
	}
	got, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, mf) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, mf)
	}
	// Empty lists come back as [], not null; a newer format is refused.
	if err := writeManifest(dir, Manifest{Format: Format, ID: "snp_x"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := readManifest(dir); got.Sites == nil || got.Services == nil || got.Warnings == nil {
		t.Fatalf("nil lists: %+v", got)
	}
	if err := writeManifest(dir, Manifest{Format: 2, ID: "snp_x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(dir); err == nil {
		t.Fatal("format 2 should be refused")
	}
}

func TestTakeProject(t *testing.T) {
	e := newEnv(t)
	j, err := e.m.Take(ScopeProject, prjID, TakeOptions{Name: "  First  "})
	if err != nil {
		t.Fatal(err)
	}
	if j.Type != model.JobSnapshot || j.ProjectID != prjID || !strings.HasPrefix(j.SnapshotID, "snp_") || j.ServiceID != "" {
		t.Fatalf("job: %+v", j)
	}
	e.done(j, nil)
	list, err := e.m.List(prjID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %+v", err, list)
	}
	mf := list[0]
	if mf.ID != j.SnapshotID || mf.Name != "First" || mf.Scope != ScopeProject || mf.OwnerID != prjID || mf.Version != "test" || mf.Format != 1 {
		t.Fatalf("manifest: %+v", mf)
	}
	if mf.Project == nil || mf.Project.Name != "Shop" || len(mf.Sites) != 1 || len(mf.Services) != 4 || mf.SizeBytes == 0 {
		t.Fatalf("contents: %+v", mf)
	}
	byID := map[string]ServiceEntry{}
	for _, se := range mf.Services {
		byID[se.Service.ID] = se
	}
	db := byID[dbID]
	if !db.HasData || db.PHPIni || db.Runtime == nil || *db.Runtime != (RuntimeRef{Kind: "mariadb", Version: "11.4.4", CatalogID: "mariadb-11.4.4",
		Binary: filepath.Join(e.home, "bin", "mariadb-11.4.4", "bin", "mariadbd.exe"), FollowedDefault: true}) {
		t.Fatalf("db entry: %+v %+v", db, db.Runtime)
	}
	if db.Service.AdminPassword != "secret" || db.Service.State != "" {
		t.Fatalf("db service: %+v", db.Service)
	}
	if php := byID[phpID]; !php.PHPIni || php.HasData || php.Runtime == nil || php.Runtime.Version != "8.3.14" {
		t.Fatalf("php entry: %+v", php)
	}
	if app := byID[appID]; app.Runtime != nil || app.HasData {
		t.Fatalf("app entry: %+v", app)
	}
	dir := filepath.Join(e.home, "snapshots", prjID, mf.ID)
	if e.read(filepath.Join(dir, "data", dbID, "ibdata1")) != "v1" || e.read(filepath.Join(dir, "data", dbID, "shop", "t.ibd")) != "table v1" ||
		e.read(filepath.Join(dir, "php", phpID, "php.ini")) != "ini v1" {
		t.Fatal("snapshot files missing")
	}
	if _, err := os.Stat(dir + ".partial"); !os.IsNotExist(err) {
		t.Fatal(".partial left behind")
	}
	if !e.ev.has("snapshots.changed") || !e.ev.has("job.done") {
		t.Fatalf("events: %v", e.ev.events)
	}

	// Newest first; the owner may be deleted and the list still works.
	time.Sleep(5 * time.Millisecond)
	second := e.take("Second")
	e.update(func(d *store.Data) { d.Projects = nil })
	list, _ = e.m.List(prjID)
	if len(list) != 2 || list[0].ID != second.ID {
		t.Fatalf("order: %+v", list)
	}
	if l, err := e.m.List("prj_none"); err != nil || l == nil || len(l) != 0 {
		t.Fatalf("empty list: %v %v", l, err)
	}
	if l, _ := e.m.List(`..\x`); len(l) != 0 {
		t.Fatal("bad owner id should list nothing")
	}
}

func TestTakeValidation(t *testing.T) {
	e := newEnv(t)
	_, err := e.m.Take(ScopeProject, "prj_none", TakeOptions{Name: "x"})
	wantStatus(t, err, 404, "project prj_none not found")
	_, err = e.m.Take(ScopeService, "svc_none", TakeOptions{Name: "x"})
	wantStatus(t, err, 404, "service svc_none not found")
	_, err = e.m.Take(ScopeService, phpID, TakeOptions{Name: "x"})
	wantStatus(t, err, 400, "Shop PHP is not a database")
	for _, n := range []string{"", "   ", strings.Repeat("a", 65), "a\nb"} {
		_, err = e.m.Take(ScopeProject, prjID, TakeOptions{Name: n})
		wantStatus(t, err, 400, "name must be 1-64 characters")
	}
	_, err = e.m.Take("other", prjID, TakeOptions{Name: "x"})
	wantStatus(t, err, 400, "")

	// A held service is busy.
	release, err := e.sup.Reserve([]string{dbID}, "resetting it")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.m.Take(ScopeService, dbID, TakeOptions{Name: "x"})
	wantStatus(t, err, 409, "busy")
	release()

	// Custom args: settings kept, data not, with a warning.
	e.update(func(d *store.Data) { d.Services[0].CustomArgs = true })
	j := e.done(e.m.Take(ScopeService, dbID, TakeOptions{Name: "custom"}))
	d, _ := e.m.Get(j.SnapshotID)
	if d.Services[0].HasData || len(d.Warnings) != 1 || !strings.Contains(d.Warnings[0], "own command line") || d.Project != nil {
		t.Fatalf("custom args: %+v", d.Manifest)
	}
	if j.ServiceID != dbID || j.ProjectID != "" {
		t.Fatalf("service job: %+v", j)
	}
}

func TestTakeAndRestoreProject(t *testing.T) {
	e := newEnv(t)
	mf := e.take("Before")

	// Change everything the snapshot covers.
	dataDir := e.sup.DataDir("mariadb", dbID)
	e.write(filepath.Join(dataDir, "ibdata1"), "v2")
	e.write(filepath.Join(dataDir, "new.ibd"), "new")
	e.write(e.m.phpIni(phpID), "ini v2")
	e.update(func(d *store.Data) {
		// a new default MariaDB, so a restore must pin the old one
		d.Runtimes[0].Default = false
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_maria2", Kind: "mariadb", Version: "11.8.1", CatalogID: "mariadb-11.8.1", Binary: "x", Default: true})
		d.Services[0].Name, d.Services[0].Port, d.Services[0].AdminPassword = "Renamed", 3399, "changed"
		d.Services = slices.DeleteFunc(d.Services, func(sv model.Service) bool { return sv.ID == phpID })
		d.Services = append(d.Services, model.Service{ID: "svc_new", Name: "Added later", Kind: "custom", Command: "x"})
		d.Sites[0].Domain = "other.localhost"
		d.Sites = append(d.Sites, model.Site{ID: "site_new", Name: "New", Domain: "new.localhost", Type: "proxy", UpstreamPort: 1, Enabled: true})
		d.Projects[0].ServiceIDs = append(d.Projects[0].ServiceIDs, "svc_new")
		d.Projects[0].SiteIDs = append(d.Projects[0].SiteIDs, "site_new")
		d.Projects[0].Name = "Shop renamed"
	})

	j, err := e.m.Restore(mf.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if j.Type != model.JobSnapshotRestore || j.SnapshotID != mf.ID || j.ProjectID != prjID {
		t.Fatalf("job: %+v", j)
	}
	e.done(j, nil)

	if e.read(filepath.Join(dataDir, "ibdata1")) != "v1" || e.read(filepath.Join(dataDir, "shop", "t.ibd")) != "table v1" {
		t.Fatal("data not restored")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "new.ibd")); !os.IsNotExist(err) {
		t.Fatal("a file added later survived the restore")
	}
	if _, err := os.Stat(dataDir + ".restore-old"); !os.IsNotExist(err) {
		t.Fatal(".restore-old left behind")
	}
	if e.read(e.m.phpIni(phpID)) != "ini v1" {
		t.Fatal("php.ini not restored")
	}
	db, _ := e.service(dbID)
	if db.Name != "Shop DB" || db.Port != 3306 || db.AdminPassword != "secret" || db.RuntimeID != "rt_maria" {
		t.Fatalf("db settings: %+v", db)
	}
	php, ok := e.service(phpID)
	if !ok || php.RuntimeID != "rt_php" || php.Port != 9000 {
		t.Fatalf("deleted service not recreated: %+v", php)
	}
	if app, _ := e.service(appID); app.RuntimeID != "" {
		t.Fatalf("custom command pinned: %+v", app)
	}
	if _, ok := e.service("svc_new"); !ok {
		t.Fatal("a service added later must not be deleted")
	}
	p, _ := e.project(prjID)
	if p.Name != "Shop" || !slices.Equal(p.ServiceIDs, []string{dbID, phpID, appID, workerID}) || !slices.Equal(p.SiteIDs, []string{siteID}) {
		t.Fatalf("project: %+v", p)
	}
	sites := e.st.Snapshot().Sites
	if len(sites) != 2 || sites[0].Domain != "shop.localhost" {
		t.Fatalf("sites: %+v", sites)
	}
	if e.sites.count() == 0 {
		t.Fatal("sites not regenerated")
	}
	for _, ev := range []string{"services.changed", "sites.changed", "projects.changed", "snapshots.changed"} {
		if !e.ev.has(ev) {
			t.Fatalf("no %s event", ev)
		}
	}

	// The deleted project comes back too.
	e.update(func(d *store.Data) { d.Projects = nil })
	e.done(e.m.Restore(mf.ID, false))
	if p, ok := e.project(prjID); !ok || len(p.ServiceIDs) != 4 {
		t.Fatalf("project not recreated: %+v", p)
	}
}

func TestRunningServices(t *testing.T) {
	e := newEnv(t)
	e.startApp()
	_, err := e.m.Take(ScopeProject, prjID, TakeOptions{Name: "x"})
	ae := wantStatus(t, err, 409, "Shop App is running; stop it first")
	if ae.Details["running"] != appID {
		t.Fatalf("details: %+v", ae.Details)
	}

	// restart: stopped, copied, started again.
	j := e.done(e.m.Take(ScopeProject, prjID, TakeOptions{Name: "hot", Restart: true}))
	e.waitState(appID, model.StateRunning)
	if _, err := e.m.Get(j.SnapshotID); err != nil {
		t.Fatal(err)
	}

	_, err = e.m.Restore(j.SnapshotID, false)
	wantStatus(t, err, 409, "stop it first, or restore with restart")
	e.write(filepath.Join(e.sup.DataDir("mariadb", dbID), "ibdata1"), "v2")
	e.done(e.m.Restore(j.SnapshotID, true))
	e.waitState(appID, model.StateRunning)
	if e.read(filepath.Join(e.sup.DataDir("mariadb", dbID), "ibdata1")) != "v1" {
		t.Fatal("not restored")
	}
}

func TestRestoreRollsBackOnFailedCopy(t *testing.T) {
	e := newEnv(t)
	db2 := "svc_db2"
	e.update(func(d *store.Data) {
		d.Services = append(d.Services, model.Service{ID: db2, Name: "Cache", Kind: "postgres", Port: 5432})
		d.Projects[0].ServiceIDs = append(d.Projects[0].ServiceIDs, db2)
	})
	dir1, dir2 := e.sup.DataDir("mariadb", dbID), e.sup.DataDir("postgres", db2)
	e.write(filepath.Join(dir2, "PG_VERSION"), "pg v1")
	mf := e.take("Before")

	e.write(filepath.Join(dir1, "ibdata1"), "v2")
	e.write(filepath.Join(dir2, "PG_VERSION"), "pg v2")
	e.write(e.m.phpIni(phpID), "ini v2")
	e.update(func(d *store.Data) { d.Services[0].Name = "Renamed" })

	// The second copy writes half its files, then fails.
	e.m.copyTree = func(src, dst string, fn func(int64, int64)) error {
		if strings.Contains(src, db2) {
			e.write(filepath.Join(dst, "half"), "x")
			return errors.New("disk full")
		}
		return fsutil.CopyTreeProgress(src, dst, fn)
	}
	got := e.wait(e.m.Restore(mf.ID, false))
	if got.State != "failed" || !strings.Contains(got.Error, "disk full") {
		t.Fatalf("job: %+v", got)
	}
	if e.read(filepath.Join(dir1, "ibdata1")) != "v2" || e.read(filepath.Join(dir2, "PG_VERSION")) != "pg v2" {
		t.Fatal("data folders not put back")
	}
	if _, err := os.Stat(filepath.Join(dir2, "half")); !os.IsNotExist(err) {
		t.Fatal("half copy left behind")
	}
	for _, d := range []string{dir1, dir2} {
		if _, err := os.Stat(d + ".restore-old"); !os.IsNotExist(err) {
			t.Fatal(".restore-old left behind")
		}
	}
	if e.read(e.m.phpIni(phpID)) != "ini v2" {
		t.Fatal("php.ini changed by a failed restore")
	}
	if db, _ := e.service(dbID); db.Name != "Renamed" {
		t.Fatal("state changed by a failed restore")
	}
	// The services are free again.
	if err := e.sup.Busy(dbID); err != nil {
		t.Fatal(err)
	}

	// A failed take leaves no snapshot behind.
	e.m.copyTree = func(string, string, func(int64, int64)) error { return errors.New("boom") }
	got = e.wait(e.m.Take(ScopeProject, prjID, TakeOptions{Name: "broken"}))
	if got.State != "failed" {
		t.Fatalf("take: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(e.home, "snapshots", prjID, got.SnapshotID+".partial")); !os.IsNotExist(err) {
		t.Fatal(".partial left behind")
	}
	if l, _ := e.m.List(prjID); len(l) != 1 {
		t.Fatalf("list after failed take: %d", len(l))
	}
}

func TestMissingRuntime(t *testing.T) {
	e := newEnv(t)
	mf := e.take("Before")
	d, _ := e.m.Get(mf.ID)
	if d.Missing == nil || len(d.Missing) != 0 {
		t.Fatalf("missing: %+v", d.Missing)
	}
	e.update(func(d *store.Data) {
		d.Runtimes = slices.DeleteFunc(d.Runtimes, func(r model.Runtime) bool { return r.Kind == "mariadb" || r.Kind == "php" })
	})
	d, _ = e.m.Get(mf.ID)
	if len(d.Missing) != 2 {
		t.Fatalf("missing: %+v", d.Missing)
	}
	want := []Missing{
		{Kind: "mariadb", Version: "11.4.4", CatalogID: "mariadb-11.4.4", Installable: true, Services: []string{"Shop DB"}},
		{Kind: "php", Version: "8.3.14", CatalogID: "php-8.3.14", Installable: false, Services: []string{"Shop PHP"}},
	}
	if !reflect.DeepEqual(d.Missing, want) {
		t.Fatalf("missing:\n got %+v\nwant %+v", d.Missing, want)
	}
	_, err := e.m.Restore(mf.ID, false)
	ae := wantStatus(t, err, 409, "install them first")
	if ae.Details["missing"] != "mariadb 11.4.4, php 8.3.14" {
		t.Fatalf("details: %+v", ae.Details)
	}

	// The same version installed again (another catalog id) matches by kind and
	// version; a registered program matches by its binary.
	e.update(func(d *store.Data) {
		d.Runtimes = append(d.Runtimes,
			model.Runtime{ID: "rt_m2", Kind: "mariadb", Version: "11.4.4", Binary: "elsewhere"},
			model.Runtime{ID: "rt_p2", Kind: "php", Version: "", Binary: filepath.Join(e.home, "bin", "php-8.3.14", "php-cgi.exe")})
	})
	d, _ = e.m.Get(mf.ID)
	if len(d.Missing) != 0 {
		t.Fatalf("missing after reinstall: %+v", d.Missing)
	}
	e.done(e.m.Restore(mf.ID, false))
	if db, _ := e.service(dbID); db.RuntimeID != "rt_m2" {
		t.Fatalf("pinned to %q", db.RuntimeID)
	}
	if php, _ := e.service(phpID); php.RuntimeID != "rt_p2" {
		t.Fatalf("pinned to %q", php.RuntimeID)
	}
}

func TestServiceSnapshot(t *testing.T) {
	e := newEnv(t)
	j, err := e.m.Take(ScopeService, dbID, TakeOptions{Name: "DB only"})
	e.done(j, err)
	if j.ServiceID != dbID {
		t.Fatalf("job: %+v", j)
	}
	d, _ := e.m.Get(j.SnapshotID)
	if d.Scope != ScopeService || d.Project != nil || len(d.Services) != 1 || len(d.Sites) != 0 || !d.Services[0].HasData {
		t.Fatalf("manifest: %+v", d.Manifest)
	}
	e.write(filepath.Join(e.sup.DataDir("mariadb", dbID), "ibdata1"), "v2")
	e.update(func(d *store.Data) {
		d.Services[0].Name = "Renamed"
		d.Services[1].Name = "PHP renamed"
		d.Projects[0].ServiceIDs = []string{phpID}
	})
	e.done(e.m.Restore(j.SnapshotID, false))
	if e.read(filepath.Join(e.sup.DataDir("mariadb", dbID), "ibdata1")) != "v1" {
		t.Fatal("data not restored")
	}
	if db, _ := e.service(dbID); db.Name != "Shop DB" {
		t.Fatal("service not restored")
	}
	if php, _ := e.service(phpID); php.Name != "PHP renamed" {
		t.Fatal("a service snapshot restored another service")
	}
	if p, _ := e.project(prjID); !slices.Equal(p.ServiceIDs, []string{phpID}) {
		t.Fatalf("a service snapshot changed the project: %+v", p)
	}
	// Deleted service: created again with its old id.
	e.update(func(d *store.Data) {
		d.Services = slices.DeleteFunc(d.Services, func(sv model.Service) bool { return sv.ID == dbID })
	})
	if l, _ := e.m.List(dbID); len(l) != 1 {
		t.Fatal("snapshots of a deleted service must stay")
	}
	e.done(e.m.Restore(j.SnapshotID, false))
	if _, ok := e.service(dbID); !ok {
		t.Fatal("not recreated")
	}

	_, err = e.m.Clone(j.SnapshotID, CloneOptions{Name: "x"})
	wantStatus(t, err, 400, "only project snapshots can be cloned")
}

func TestClone(t *testing.T) {
	e := newEnv(t)
	mf := e.take("Before")
	// 3307 is taken by something outside Zero2Dev, 9001 by another service.
	e.m.portFree = func(p int) bool { return p != 3307 }
	e.update(func(d *store.Data) {
		d.Services = append(d.Services, model.Service{ID: "svc_other", Name: "Other", Kind: "custom", Command: "x", Port: 9001})
		d.Sites = append(d.Sites, model.Site{ID: "site_proxy", Name: "Proxy", Domain: "api.localhost", Type: "proxy", UpstreamPort: 5000, Enabled: true})
		d.Projects[0].SiteIDs = append(d.Projects[0].SiteIDs, "site_proxy")
	})
	mf = e.take("With proxy")

	j, err := e.m.Clone(mf.ID, CloneOptions{Name: "Shop 2"})
	if err != nil {
		t.Fatal(err)
	}
	if j.Type != model.JobSnapshotClone || j.SnapshotID != mf.ID {
		t.Fatalf("job: %+v", j)
	}
	j = e.done(j, nil)
	p, ok := e.project(j.ProjectID)
	if !ok || p.ID == prjID || p.Name != "Shop 2" || len(p.ServiceIDs) != 4 || len(p.SiteIDs) != 2 {
		t.Fatalf("project: %+v", p)
	}
	newDB, newPHP, newApp, newWorker := p.ServiceIDs[0], p.ServiceIDs[1], p.ServiceIDs[2], p.ServiceIDs[3]
	for _, id := range p.ServiceIDs {
		if id == dbID || id == phpID || id == appID || id == workerID || !strings.HasPrefix(id, "svc_") {
			t.Fatalf("ids not new: %v", p.ServiceIDs)
		}
	}
	db, _ := e.service(newDB)
	php, _ := e.service(newPHP)
	app, _ := e.service(newApp)
	worker, _ := e.service(newWorker)
	if db.Name != "Shop DB" || db.Port != 3308 || db.AdminPassword != "secret" || db.RuntimeID != "rt_maria" {
		t.Fatalf("db copy: %+v", db)
	}
	if php.Port != 9002 || worker.Port != 5001 || app.Cwd == "" {
		t.Fatalf("ports: php %d worker %d", php.Port, worker.Port)
	}
	if !slices.Equal(worker.DependsOn, []string{newApp}) || worker.DatabaseID != newDB {
		t.Fatalf("links: %+v", worker)
	}
	if e.read(filepath.Join(e.sup.DataDir("mariadb", newDB), "ibdata1")) != "v1" {
		t.Fatal("data not copied")
	}
	sites := map[string]model.Site{}
	for _, s := range e.st.Snapshot().Sites {
		sites[s.ID] = s
	}
	phpSite, proxySite := sites[p.SiteIDs[0]], sites[p.SiteIDs[1]]
	if phpSite.Domain != "shop-copy.localhost" || phpSite.ServiceID != newPHP || phpSite.ID == siteID {
		t.Fatalf("php site: %+v", phpSite)
	}
	if proxySite.Domain != "api-copy.localhost" || proxySite.UpstreamPort != 5001 {
		t.Fatalf("proxy site: %+v", proxySite)
	}
	// The original is untouched.
	if orig, _ := e.service(dbID); orig.Port != 3306 {
		t.Fatal("original changed")
	}
	if !e.ev.has("projects.changed") || e.sites.count() == 0 {
		t.Fatal("no events or regenerate")
	}

	// A second clone: the next free names and ports.
	j = e.done(e.m.Clone(mf.ID, CloneOptions{Name: "Shop 3"}))
	p3, _ := e.project(j.ProjectID)
	var domains []string
	for _, s := range e.st.Snapshot().Sites {
		if slices.Contains(p3.SiteIDs, s.ID) {
			domains = append(domains, s.Domain)
		}
	}
	if !slices.Equal(domains, []string{"shop-copy-2.localhost", "api-copy-2.localhost"}) {
		t.Fatalf("domains: %v", domains)
	}
	db3, _ := e.service(p3.ServiceIDs[0])
	if db3.Port != 3309 {
		t.Fatalf("port: %d", db3.Port)
	}

	// Own suffix; validation.
	j = e.done(e.m.Clone(mf.ID, CloneOptions{Name: "Staging", DomainSuffix: "-Staging"}))
	p4, _ := e.project(j.ProjectID)
	for _, s := range e.st.Snapshot().Sites {
		if s.ID == p4.SiteIDs[0] && s.Domain != "shop-staging.localhost" {
			t.Fatalf("suffix: %s", s.Domain)
		}
	}
	_, err = e.m.Clone(mf.ID, CloneOptions{Name: ""})
	wantStatus(t, err, 400, "name must be 1-64 characters")
	for _, s := range []string{"-copy-", "a.b", "x y", strings.Repeat("a", 33)} {
		_, err = e.m.Clone(mf.ID, CloneOptions{Name: "x", DomainSuffix: s})
		wantStatus(t, err, 400, "domainSuffix")
	}
	_, err = e.m.Clone("snp_none", CloneOptions{Name: "x"})
	wantStatus(t, err, 404, "snapshot snp_none not found")
}

func TestDelete(t *testing.T) {
	e := newEnv(t)
	mf := e.take("Before")
	release := e.m.claim(mf.ID, "restoring it")
	wantStatus(t, e.m.Delete(mf.ID), 409, "in use")
	release()
	if err := e.m.Delete(mf.ID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.m.Delete(mf.ID), 404, "")
	_, err := e.m.Get(mf.ID)
	wantStatus(t, err, 404, "")
	if _, err := os.Stat(filepath.Join(e.home, "snapshots", prjID)); !os.IsNotExist(err) {
		t.Fatal("empty owner folder left")
	}
	_, err = e.m.Get(`..\..\state`)
	wantStatus(t, err, 404, "")
}

func TestUniqueDomain(t *testing.T) {
	used := map[string]bool{"shop-copy.localhost": true, "shop-copy-2.localhost": true}
	if got := uniqueDomain("shop.localhost", "-copy", used); got != "shop-copy-3.localhost" {
		t.Fatal(got)
	}
	if got := uniqueDomain("a.b.test", "-x", nil); got != "a-x.b.test" {
		t.Fatal(got)
	}
	long := strings.Repeat("a", 63) + ".localhost"
	got := uniqueDomain(long, "-copy", map[string]bool{strings.Repeat("a", 58) + "-copy.localhost": true})
	first, _, _ := strings.Cut(got, ".")
	if len(first) > 63 || !strings.HasSuffix(first, "-2") {
		t.Fatal(got)
	}
}

func TestNextPort(t *testing.T) {
	m := &Manager{portFree: func(p int) bool { return p != 8081 }}
	if p, _ := m.nextPort(8079, map[int]bool{8080: true}); p != 8082 {
		t.Fatal(p)
	}
	if p, _ := m.nextPort(65535, nil); p != 1024 {
		t.Fatal(p)
	}
	m.portFree = func(int) bool { return false }
	if _, err := m.nextPort(3000, nil); err == nil {
		t.Fatal("want an error")
	}
}
