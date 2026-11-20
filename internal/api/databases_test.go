package api

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/dbtools"
	"zero2dev/internal/events"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
	"zero2dev/internal/winpick"
)

// dbSup is the real supervisor with the state of some services pretended
// (the tests run no database server).
type dbSup struct {
	*supervisor.Supervisor
	mu      sync.Mutex
	running map[string]bool
}

func (s *dbSup) State(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[id] {
		return model.StateRunning
	}
	return s.Supervisor.State(id)
}

// dbRun plays a MariaDB server with the database shop and the user root.
type dbRun struct {
	mu    sync.Mutex
	stdin []string
	args  []string
}

func (r *dbRun) run(ctx context.Context, c dbtools.Cmd) error {
	var in []byte
	if c.Stdin != nil {
		in, _ = io.ReadAll(c.Stdin)
	}
	r.mu.Lock()
	r.stdin = append(r.stdin, string(in))
	r.args = append(r.args, strings.Join(c.Args, " "))
	r.mu.Unlock()
	out := ""
	switch {
	case strings.Contains(string(in), "information_schema.SCHEMATA"):
		out = "mysql\t1\nshop\t2048\n"
	case strings.Contains(string(in), "DISTINCT User"):
		out = "root\n"
	case strings.Contains(string(in), "SELECT Host"):
		out = "localhost\n"
	case strings.Contains(c.Bin, "dump"):
		out = "-- dump\n"
	}
	if c.Stdout != nil {
		io.WriteString(c.Stdout, out)
	}
	return nil
}

type dbEnv struct {
	*tenv
	sup *dbSup
	run *dbRun
	db  string // id of the MariaDB service
	web string // id of a Node service
}

func newDBEnv(t *testing.T) *dbEnv {
	t.Helper()
	e := newEnv(t)
	bin := filepath.Join(e.home, "bin", "mariadb-11", "bin")
	os.MkdirAll(bin, 0o755)
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	for _, n := range []string{"mariadbd", "mariadb", "mariadb-dump"} {
		os.WriteFile(filepath.Join(bin, n+ext), nil, 0o755)
	}
	d := &dbEnv{tenv: e, sup: &dbSup{Supervisor: e.sup, running: map[string]bool{}}, run: &dbRun{}, db: "svc_db000001", web: "svc_web00001"}
	e.srv.Store.Update(func(s *store.Data) error {
		s.Runtimes = append(s.Runtimes, model.Runtime{ID: "rt_m", Kind: "mariadb", Version: "11.4.4", Binary: filepath.Join(bin, "mariadbd"+ext), Default: true})
		s.Services = append(s.Services,
			model.Service{ID: d.db, Name: "Shop DB", Kind: "mariadb", Port: 3307},
			model.Service{ID: d.web, Name: "Web", Kind: "node", Port: 3000})
		return nil
	})
	reg := jobs.New(e.hub)
	m := dbtools.New(e.srv.Store, d.sup, reg, e.home)
	m.Run = d.run.run
	e.srv.DB, e.srv.Jobs = m, reg
	return d
}

func (d *dbEnv) start() {
	d.sup.mu.Lock()
	d.sup.running[d.db] = true
	d.sup.mu.Unlock()
}

func (d *dbEnv) waitJob(id string) model.Job {
	d.t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		var j model.Job
		d.do("GET", "/api/jobs/"+id, nil, &j)
		if j.State != "running" {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	d.t.Fatalf("job %s did not finish", id)
	return model.Job{}
}

func errMessage(e *tenv, method, path string, body any, status int) string {
	e.t.Helper()
	var er struct {
		Error struct{ Code, Message string }
	}
	if resp := e.do(method, path, body, &er); resp.StatusCode != status {
		e.t.Fatalf("%s %s: %d %+v, want %d", method, path, resp.StatusCode, er, status)
	}
	return er.Error.Message
}

func TestDatabaseRoutesNotConfigured(t *testing.T) {
	e := newEnv(t)
	if e.errCode("GET", "/api/services/x/database", nil, 501) != "not_supported" {
		t.Fatal("no manager: 501")
	}
}

func TestDatabaseInfoAndCreate(t *testing.T) {
	d := newDBEnv(t)
	base := "/api/services/" + d.db + "/database"
	var info dbtools.Info
	if resp := d.do("GET", base, nil, &info); resp.StatusCode != 200 || info.Running || info.Kind != "mariadb" || info.User != "root" ||
		info.Databases == nil || len(info.Databases) != 0 || !info.Supports.Dumps {
		t.Fatalf("stopped info %d %+v", resp.StatusCode, info)
	}
	if msg := errMessage(d.tenv, "GET", "/api/services/"+d.web+"/database", nil, 400); msg != "Web is not a database" {
		t.Fatalf("not a database: %q", msg)
	}
	d.errCode("GET", "/api/services/nope/database", nil, 404)
	if msg := errMessage(d.tenv, "POST", base+"/databases", map[string]string{"name": "blog"}, 409); msg != "start Shop DB first" {
		t.Fatalf("stopped: %q", msg)
	}
	d.start()
	d.do("GET", base, nil, &info)
	if !info.Running || len(info.Databases) != 1 || info.Databases[0].Name != "shop" || info.Databases[0].SizeBytes != 2048 {
		t.Fatalf("running info %+v", info)
	}
	if msg := errMessage(d.tenv, "POST", base+"/databases", map[string]string{"name": "bad-name"}, 400); msg != "database name must be 1-64 letters, digits or _" {
		t.Fatalf("bad name: %q", msg)
	}
	d.errCode("POST", base+"/databases", map[string]string{"name": "shop"}, 409)
	var created map[string]string
	if resp := d.do("POST", base+"/databases", map[string]string{"name": "blog"}, &created); resp.StatusCode != 201 || created["name"] != "blog" {
		t.Fatalf("create %d %v", resp.StatusCode, created)
	}
	if msg := errMessage(d.tenv, "POST", base+"/users", map[string]string{"user": "x y", "password": "pw"}, 400); msg != "user name must be 1-64 letters, digits or _" {
		t.Fatalf("bad user: %q", msg)
	}
	if resp := d.do("POST", base+"/users", map[string]string{"user": "app", "password": "pw", "database": "shop"}, &created); resp.StatusCode != 201 ||
		created["user"] != "app" || created["database"] != "shop" {
		t.Fatalf("user %d %v", resp.StatusCode, created)
	}
	for _, a := range d.run.args {
		if strings.Contains(a, "pw") {
			t.Fatalf("password on a command line: %s", a)
		}
	}
}

func TestDatabaseImportExportJobs(t *testing.T) {
	d := newDBEnv(t)
	base := "/api/services/" + d.db + "/database"
	dump := filepath.Join(d.home, "in.sql")
	os.WriteFile(dump, []byte("CREATE TABLE t (id int);\n"), 0o644)
	d.start()
	d.errCode("POST", base+"/import", map[string]any{"database": "shop", "filePath": filepath.Join(d.home, "missing.sql")}, 400)
	d.errCode("POST", base+"/import", map[string]any{"database": "blog", "filePath": dump}, 400)

	var j model.Job
	if resp := d.do("POST", base+"/import", map[string]any{"database": "blog", "filePath": dump, "create": true}, &j); resp.StatusCode != 202 ||
		j.Type != model.JobDBImport || j.ServiceID != d.db {
		t.Fatalf("import %d %+v", resp.StatusCode, j)
	}
	if done := d.waitJob(j.ID); done.State != "done" {
		t.Fatalf("import job %+v", done)
	}

	out := filepath.Join(d.home, "out.sql")
	if resp := d.do("POST", base+"/export", map[string]any{"database": "shop", "filePath": out}, &j); resp.StatusCode != 202 || j.Type != model.JobDBExport {
		t.Fatalf("export %d %+v", resp.StatusCode, j)
	}
	if done := d.waitJob(j.ID); done.State != "done" || done.Path != out {
		t.Fatalf("export job %+v", done)
	}
	if msg := errMessage(d.tenv, "POST", base+"/export", map[string]any{"database": "shop", "filePath": out}, 409); msg != out+" exists" {
		t.Fatalf("exists: %q", msg)
	}
	if resp := d.do("POST", base+"/export", map[string]any{"database": "shop", "filePath": out, "overwrite": true}, &j); resp.StatusCode != 202 {
		t.Fatalf("overwrite %d", resp.StatusCode)
	}
	d.waitJob(j.ID)
}

func TestDatabasePasswordAndReset(t *testing.T) {
	d := newDBEnv(t)
	base := "/api/services/" + d.db + "/database"
	sub := d.hub.Subscribe()
	defer d.hub.Unsubscribe(sub)
	if msg := errMessage(d.tenv, "PUT", base+"/password", map[string]string{"password": "s3cret"}, 409); msg != "start Shop DB first" {
		t.Fatalf("stopped: %q", msg)
	}
	d.start()
	var sv model.Service
	if resp := d.do("PUT", base+"/password", map[string]string{"password": "s3cret"}, &sv); resp.StatusCode != 200 || sv.AdminPassword != "s3cret" || sv.State != "stopped" {
		t.Fatalf("password %d %+v", resp.StatusCode, sv)
	}
	if !gotEvent(sub, "services.changed") {
		t.Fatal("services.changed after the password change")
	}
	// A service update keeps it (it is not part of ServiceInput).
	in := model.ServiceInput{Name: "Shop DB 2", Kind: "mariadb", Port: 3307}
	var upd model.Service
	if resp := d.do("PUT", "/api/services/"+d.db, in, &upd); resp.StatusCode != 200 || upd.AdminPassword != "s3cret" {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update %d %s", resp.StatusCode, b)
	}
	var info dbtools.Info
	if d.do("GET", base, nil, &info); !info.HasPassword {
		t.Fatal("hasPassword")
	}

	if msg := errMessage(d.tenv, "POST", base+"/reset", nil, 409); msg != "stop Shop DB 2 first" {
		t.Fatalf("running reset: %q", msg)
	}
	d.sup.mu.Lock()
	d.sup.running[d.db] = false
	d.sup.mu.Unlock()
	data := filepath.Join(d.home, "mariadb", d.db)
	os.MkdirAll(data, 0o755)
	os.WriteFile(filepath.Join(data, "ibdata1"), []byte("x"), 0o644)
	if resp := d.do("POST", base+"/reset", nil, &sv); resp.StatusCode != 200 || sv.AdminPassword != "" || sv.ID != d.db {
		t.Fatalf("reset %d %+v", resp.StatusCode, sv)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("data folder must be deleted")
	}
	if !gotEvent(sub, "services.changed") {
		t.Fatal("services.changed after the reset")
	}
	if msg := errMessage(d.tenv, "POST", "/api/services/"+d.web+"/database/reset", nil, 400); msg != "Web is not a database" {
		t.Fatalf("%q", msg)
	}
}

// gotEvent reads sub's frames until one of type typ arrives (or a second passes).
func gotEvent(sub *events.Sub, typ string) bool {
	timeout := time.After(time.Second)
	for {
		select {
		case f, ok := <-sub.C:
			if !ok {
				return false
			}
			if strings.Contains(string(f), typ) {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

func TestFileDialogRoute(t *testing.T) {
	e := newEnv(t)
	e.errCode("POST", "/api/dialog/file", map[string]any{}, 501)
	var got winpick.FileOptions
	e.srv.PickFile = func(o winpick.FileOptions) (string, bool, error) {
		got = o
		if o.Save {
			return `C:\dumps\shop.sql`, true, nil
		}
		return "", false, nil
	}
	var res struct {
		Path      string `json:"path"`
		Cancelled bool   `json:"cancelled"`
	}
	body := map[string]any{"save": true, "defaultName": "shop.sql", "start": `C:\dumps`, "filters": []map[string]string{{"name": "SQL dumps", "pattern": "*.sql;*.sql.gz"}}}
	if resp := e.do("POST", "/api/dialog/file", body, &res); resp.StatusCode != 200 || res.Path != `C:\dumps\shop.sql` || res.Cancelled {
		t.Fatalf("%d %+v", resp.StatusCode, res)
	}
	if !got.Save || got.Title != "Save as" || got.DefaultName != "shop.sql" || len(got.Filters) != 1 || got.Start != `C:\dumps` {
		t.Fatalf("options %+v", got)
	}
	if resp := e.do("POST", "/api/dialog/file", nil, &res); resp.StatusCode != 200 || !res.Cancelled || res.Path != "" || got.Title != "Select a file" {
		t.Fatalf("cancel %d %+v", resp.StatusCode, res)
	}
	for _, bad := range []map[string]any{
		{"title": strings.Repeat("x", 101)},
		{"title": "a\nb"},
		{"defaultName": `a\b.sql`},
		{"defaultName": "c:x"},
		{"filters": []map[string]string{{"name": "", "pattern": "*.sql"}}},
		{"filters": make([]map[string]string, 11)},
	} {
		e.errCode("POST", "/api/dialog/file", bad, 400)
	}
	e.srv.PickFile = func(winpick.FileOptions) (string, bool, error) { return "", false, winpick.ErrBusy }
	e.errCode("POST", "/api/dialog/file", nil, 409)
}
