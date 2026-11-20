package dbtools

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// ---------- fakes ----------

// call is one recorded run of a client program.
type call struct {
	Prog  string // file name without .exe
	Args  []string
	Env   []string
	Stdin string
}

func (c call) args() string { return strings.Join(c.Args, " ") }

func (c call) env(key string) (string, bool) {
	for _, kv := range c.Env {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			return v, true
		}
	}
	return "", false
}

// fakeRunner records every Cmd and answers with respond (canned output, error).
type fakeRunner struct {
	mu      sync.Mutex
	calls   []call
	respond func(c call) (string, error)
}

func (f *fakeRunner) run(ctx context.Context, c Cmd) error {
	var in []byte
	if c.Stdin != nil {
		var err error
		if in, err = io.ReadAll(c.Stdin); err != nil {
			return err
		}
	}
	cl := call{Prog: progName(c.Bin), Args: c.Args, Env: c.Env, Stdin: string(in)}
	f.mu.Lock()
	f.calls = append(f.calls, cl)
	respond := f.respond
	f.mu.Unlock()
	out, err := defaultRespond(cl)
	if respond != nil {
		out, err = respond(cl)
	}
	if c.Stdout != nil && out != "" {
		if _, werr := io.WriteString(c.Stdout, out); werr != nil && err == nil {
			err = werr
		}
	}
	return err
}

func (f *fakeRunner) all() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call{}, f.calls...)
}

// find returns the calls whose stdin or args contain s.
func (f *fakeRunner) find(s string) []call {
	var out []call
	for _, c := range f.all() {
		if strings.Contains(c.Stdin, s) || strings.Contains(c.args(), s) {
			out = append(out, c)
		}
	}
	return out
}

const dumpText = "-- dump\nCREATE TABLE t (id int);\nINSERT INTO t VALUES (1);\n"

// defaultRespond plays a server with the databases shop (and the system ones)
// and the users root and shop.
func defaultRespond(c call) (string, error) {
	switch {
	case strings.Contains(c.Stdin, "information_schema.SCHEMATA"):
		return "information_schema\t0\nmysql\t100\nshop\t2048\nsys\t0\n", nil
	case strings.Contains(c.Stdin, "pg_database"):
		return "postgres\t7000\nshop\t4096\n", nil
	case strings.Contains(c.Stdin, "DISTINCT User FROM mysql.user"):
		return "\nmariadb.sys\nPUBLIC\nroot\nshop\n", nil
	case strings.Contains(c.Stdin, "pg_roles"):
		return "postgres\nshop\n", nil
	case strings.Contains(c.Stdin, "SELECT Host FROM mysql.user"):
		return "127.0.0.1\n::1\nlocalhost\n", nil
	case c.Prog == "mariadb-dump" || c.Prog == "mysqldump" || c.Prog == "pg_dump":
		return dumpText, nil
	case c.Prog == "redis-cli":
		return "OK\n", nil
	}
	return "", nil
}

type fakeSup struct {
	mu       sync.Mutex
	home     string
	bins     map[string]string // kind -> server binary
	states   map[string]string
	reserved map[string]string
	reserves int
	logs     []string
}

func (s *fakeSup) State(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.states[id]; st != "" {
		return st
	}
	return model.StateStopped
}

func (s *fakeSup) setState(id, st string) {
	s.mu.Lock()
	s.states[id] = st
	s.mu.Unlock()
}

func (s *fakeSup) RuntimeFor(svc model.Service) (*model.Runtime, string, error) {
	bin := s.bins[svc.Kind]
	if bin == "" {
		return nil, "", apperr.Conflict("no %s runtime registered and none found on PATH", svc.Kind)
	}
	return &model.Runtime{Kind: svc.Kind, Binary: bin}, bin, nil
}

func (s *fakeSup) DataDir(kind, id string) string { return filepath.Join(s.home, kind, id) }

func (s *fakeSup) Reserve(ids []string, why string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if w, ok := s.reserved[id]; ok {
			return nil, apperr.Conflict("%s is busy (%s)", id, w)
		}
	}
	s.reserves++
	for _, id := range ids {
		s.reserved[id] = why
	}
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, id := range ids {
			delete(s.reserved, id)
		}
	}, nil
}

func (s *fakeSup) Log(id, stream, text string) {
	s.mu.Lock()
	s.logs = append(s.logs, id+" "+stream+" "+text)
	s.mu.Unlock()
}

func (s *fakeSup) joinedLogs() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.logs, "\n")
}

type tenv struct {
	t   *testing.T
	m   *Manager
	sup *fakeSup
	run *fakeRunner
	st  *store.Store
	dir string
}

// newEnv: services m (mariadb), p (postgres), r (redis), y (mysql), n (node),
// each kind with a server binary and its client programs in <dir>/<kind>/bin.
func newEnv(t *testing.T) *tenv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	sup := &fakeSup{home: filepath.Join(dir, "data"), bins: map[string]string{}, states: map[string]string{}, reserved: map[string]string{}}
	progs := map[string][]string{
		"mariadb":  {"mariadbd", "mariadb", "mariadb-dump"},
		"mysql":    {"mysqld", "mysql", "mysqldump"},
		"postgres": {"postgres", "psql", "pg_dump"},
		"redis":    {"redis-server", "redis-cli"},
	}
	for kind, names := range progs {
		bdir := filepath.Join(dir, kind, "bin")
		os.MkdirAll(bdir, 0o755)
		for _, n := range names {
			os.WriteFile(filepath.Join(bdir, exe(n)), nil, 0o755)
		}
		sup.bins[kind] = filepath.Join(bdir, exe(names[0]))
	}
	st.Update(func(d *store.Data) error {
		d.Services = append(d.Services,
			model.Service{ID: "m", Name: "Shop DB", Kind: "mariadb", Port: 3307},
			model.Service{ID: "p", Name: "PG", Kind: "postgres", Port: 5432},
			model.Service{ID: "r", Name: "Cache", Kind: "redis", Port: 6379},
			model.Service{ID: "y", Name: "My", Kind: "mysql", Port: 3306},
			model.Service{ID: "n", Name: "Web", Kind: "node", Port: 3000})
		return nil
	})
	run := &fakeRunner{}
	m := New(st, sup, jobs.New(nil), dir)
	m.Run = run.run
	return &tenv{t: t, m: m, sup: sup, run: run, st: st, dir: dir}
}

func (e *tenv) running(ids ...string) {
	for _, id := range ids {
		e.sup.setState(id, model.StateRunning)
	}
}

func (e *tenv) svc(id string) model.Service {
	for _, s := range e.st.Snapshot().Services {
		if s.ID == id {
			return s
		}
	}
	e.t.Fatalf("no service %s", id)
	return model.Service{}
}

func (e *tenv) wait(j model.Job) model.Job {
	e.t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		got, _ := e.m.Jobs.Get(j.ID)
		if got.State != "running" {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("job %s did not finish", j.ID)
	return j
}

// wantErr checks the HTTP status and part of the message of err.
func wantErr(t *testing.T, err error, status int, msg string) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("want %d %q, got %v", status, msg, err)
	}
	if ae.Status != status || !strings.Contains(ae.Message, msg) {
		t.Fatalf("want %d %q, got %d %q", status, msg, ae.Status, ae.Message)
	}
}

// noSecret fails when s appears in any argument of any recorded call.
func (e *tenv) noSecretInArgs(s string) {
	e.t.Helper()
	for _, c := range e.run.all() {
		if strings.Contains(c.args(), s) {
			e.t.Fatalf("%s got the password on its command line: %q", c.Prog, c.Args)
		}
	}
	if strings.Contains(e.sup.joinedLogs(), s) {
		e.t.Fatalf("the password was logged:\n%s", e.sup.joinedLogs())
	}
}

// ---------- tests ----------

func TestInfo(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	info, err := e.m.Info(ctx, "m")
	if err != nil || info.Running || len(info.Databases) != 0 || info.Databases == nil || info.Users == nil || info.User != "root" ||
		!info.Supports.Databases || !info.Supports.Dumps || !info.Supports.Reset || info.DataDir != filepath.Join(e.sup.home, "mariadb", "m") {
		t.Fatalf("stopped: %+v %v", info, err)
	}
	if len(e.run.all()) != 0 {
		t.Fatal("a stopped server must not be asked")
	}
	e.running("m", "p", "r")
	info, err = e.m.Info(ctx, "m")
	if err != nil || !info.Running || len(info.Databases) != 1 || info.Databases[0] != (Database{"shop", 2048}) ||
		strings.Join(info.Users, ",") != "root,shop" {
		t.Fatalf("running: %+v %v", info, err)
	}
	c := e.run.all()[0]
	if c.Prog != "mariadb" || !strings.HasPrefix(c.args(), "--no-defaults --host=127.0.0.1 --port=3307 --protocol=tcp --user=root") {
		t.Fatalf("client call %+v", c)
	}
	if v, ok := c.env("MYSQL_PWD"); !ok || v != "" {
		t.Fatalf("MYSQL_PWD must always be set (empty without a password): %q", c.Env)
	}
	info, _ = e.m.Info(ctx, "p")
	if info.User != "postgres" || len(info.Databases) != 1 || info.Databases[0].Name != "shop" || strings.Join(info.Users, ",") != "postgres,shop" {
		t.Fatalf("postgres: %+v", info)
	}
	if c := e.run.find("pg_database")[0]; c.Prog != "psql" || !strings.Contains(c.args(), "--dbname=postgres") || !strings.Contains(c.args(), "--no-password") {
		t.Fatalf("psql call %+v", c)
	}
	n := len(e.run.all())
	info, _ = e.m.Info(ctx, "r")
	if !info.Running || info.User != "" || len(info.Databases) != 0 || info.Supports.Databases || info.Supports.Dumps || !info.Supports.Password || len(e.run.all()) != n {
		t.Fatalf("redis: %+v", info)
	}
	_, err = e.m.Info(ctx, "n")
	wantErr(t, err, 400, "Web is not a database")
	_, err = e.m.Info(ctx, "nope")
	wantErr(t, err, 404, "not found")
}

func TestMissingClientProgram(t *testing.T) {
	e := newEnv(t)
	e.running("m")
	os.Remove(filepath.Join(e.dir, "mariadb", "bin", exe("mariadb")))
	_, err := e.m.Info(context.Background(), "m")
	wantErr(t, err, 409, exe("mariadb")+" was not found")
	// The mysql client is accepted in its place.
	os.WriteFile(filepath.Join(e.dir, "mariadb", "bin", exe("mysql")), nil, 0o755)
	if _, err := e.m.Info(context.Background(), "m"); err != nil {
		t.Fatal(err)
	}
	if e.run.all()[0].Prog != "mysql" {
		t.Fatal("mysql must be used when mariadb is missing")
	}
}

func TestCreateDatabase(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	wantErr(t, e.m.CreateDatabase(ctx, "m", "new"), 409, "start Shop DB first")
	e.running("m", "p", "r")
	for _, bad := range []string{"", "a-b", "x;DROP", strings.Repeat("a", 65), "näme", "a b"} {
		wantErr(t, e.m.CreateDatabase(ctx, "m", bad), 400, "database name must be 1-64 letters, digits or _")
	}
	for _, sys := range []string{"mysql", "sys", "information_schema", "performance_schema", "postgres", "template0", "TEMPLATE1"} {
		wantErr(t, e.m.CreateDatabase(ctx, "m", sys), 400, "system database")
	}
	wantErr(t, e.m.CreateDatabase(ctx, "m", "SHOP"), 409, "database SHOP exists")
	wantErr(t, e.m.CreateDatabase(ctx, "r", "x"), 400, "Redis")
	wantErr(t, e.m.CreateDatabase(ctx, "n", "x"), 400, "Web is not a database")
	if err := e.m.CreateDatabase(ctx, "m", "new_1"); err != nil {
		t.Fatal(err)
	}
	if c := e.run.find("CREATE DATABASE"); len(c) != 1 || c[0].Stdin != "CREATE DATABASE `new_1` CHARACTER SET utf8mb4;\n" {
		t.Fatalf("calls %+v", c)
	}
	if err := e.m.CreateDatabase(ctx, "p", "new_2"); err != nil {
		t.Fatal(err)
	}
	if c := e.run.find(`CREATE DATABASE "new_2"`); len(c) != 1 || c[0].Prog != "psql" || !strings.Contains(c[0].args(), "--set=ON_ERROR_STOP=1") {
		t.Fatalf("postgres calls %+v", c)
	}
	if !strings.Contains(e.sup.joinedLogs(), "m system create database new_1: mariadb --no-defaults") {
		t.Fatalf("log:\n%s", e.sup.joinedLogs())
	}
}

func TestCreateUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.running("m", "p")
	const pw = `it's a \pass`
	wantErr(t, e.m.CreateUser(ctx, "m", "bad-name", pw, ""), 400, "user name must be 1-64 letters, digits or _")
	wantErr(t, e.m.CreateUser(ctx, "m", "app", "", ""), 400, "password must be 1-128")
	wantErr(t, e.m.CreateUser(ctx, "m", "app", "a\nb", ""), 400, "password must be")
	wantErr(t, e.m.CreateUser(ctx, "m", "app", strings.Repeat("x", 129), ""), 400, "password must be")
	wantErr(t, e.m.CreateUser(ctx, "m", "shop", pw, ""), 409, "user shop exists")
	wantErr(t, e.m.CreateUser(ctx, "m", "app", pw, "missing"), 400, "database missing does not exist")
	wantErr(t, e.m.CreateUser(ctx, "m", "app", pw, "mysql"), 400, "system database")
	if err := e.m.CreateUser(ctx, "m", "app", pw, "shop"); err != nil {
		t.Fatal(err)
	}
	c := e.run.find("CREATE USER")
	want := "CREATE USER 'app'@'%' IDENTIFIED BY 'it''s a \\\\pass';\nGRANT ALL PRIVILEGES ON `shop`.* TO 'app'@'%';\n" +
		"CREATE USER 'app'@'localhost' IDENTIFIED BY 'it''s a \\\\pass';\nGRANT ALL PRIVILEGES ON `shop`.* TO 'app'@'localhost';\nFLUSH PRIVILEGES;\n"
	if len(c) != 1 || c[0].Stdin != want {
		t.Fatalf("mariadb SQL:\n%s\nwant:\n%s", c[0].Stdin, want)
	}
	if err := e.m.CreateUser(ctx, "p", "app", pw, "shop"); err != nil {
		t.Fatal(err)
	}
	c = e.run.find("CREATE ROLE")
	if len(c) != 1 || c[0].Stdin != "CREATE ROLE \"app\" LOGIN PASSWORD 'it''s a \\pass';\nGRANT ALL PRIVILEGES ON DATABASE \"shop\" TO \"app\";\n" {
		t.Fatalf("postgres SQL %q", c[0].Stdin)
	}
	if g := e.run.find("GRANT ALL ON SCHEMA public"); len(g) != 1 || !strings.Contains(g[0].args(), "--dbname=shop") {
		t.Fatalf("schema grant %+v", g)
	}
	e.noSecretInArgs(pw)
}

func TestCreateUserErrorHidesPassword(t *testing.T) {
	e := newEnv(t)
	e.running("m")
	e.run.respond = func(c call) (string, error) {
		if strings.Contains(c.Stdin, "CREATE USER") {
			return "", errors.New("mariadb: ERROR 1064 near 'Sup3rSecret' at line 1")
		}
		return defaultRespond(c)
	}
	err := e.m.CreateUser(context.Background(), "m", "app", "Sup3rSecret", "")
	if err == nil || strings.Contains(err.Error(), "Sup3rSecret") || !strings.Contains(err.Error(), "ERROR 1064 near '********'") {
		t.Fatalf("got %v", err)
	}
	e.noSecretInArgs("Sup3rSecret")
}

func writeGzip(t *testing.T, path, text string) {
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	zw.Write([]byte(text))
	zw.Close()
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImport(t *testing.T) {
	e := newEnv(t)
	plain := filepath.Join(e.dir, "shop.sql")
	os.WriteFile(plain, []byte(dumpText), 0o644)
	gz := filepath.Join(e.dir, "shop.dump") // gzip whatever the name
	writeGzip(t, gz, dumpText+"-- gz\n")

	_, err := e.m.Import("m", "shop", plain, false)
	wantErr(t, err, 409, "start Shop DB first")
	e.running("m", "p", "r")
	_, err = e.m.Import("m", "shop", "relative.sql", false)
	wantErr(t, err, 400, "absolute")
	_, err = e.m.Import("m", "shop", filepath.Join(e.dir, "missing.sql"), false)
	wantErr(t, err, 400, "does not exist")
	_, err = e.m.Import("m", "other", plain, false)
	wantErr(t, err, 400, "database other does not exist")
	_, err = e.m.Import("m", "mysql", plain, false)
	wantErr(t, err, 400, "system database")
	_, err = e.m.Import("r", "x", plain, false)
	wantErr(t, err, 400, "Redis")

	j, err := e.m.Import("m", "shop", plain, false)
	if err != nil || j.Type != model.JobDBImport || j.ServiceID != "m" || j.State != "running" || j.Step != "Importing" {
		t.Fatalf("job %+v %v", j, err)
	}
	if done := e.wait(j); done.State != "done" || done.Percent != 100 {
		t.Fatalf("done %+v", done)
	}
	imp := e.run.find("--database=shop")
	if len(imp) != 1 || imp[0].Stdin != dumpText || !strings.Contains(imp[0].args(), "--max-allowed-packet=1073741824") {
		t.Fatalf("import call %+v", imp)
	}

	// gzip, into a database created first.
	j, _ = e.m.Import("m", "fresh", gz, true)
	if done := e.wait(j); done.State != "done" {
		t.Fatalf("gz %+v", done)
	}
	calls := e.run.all()
	if c := calls[len(calls)-2]; !strings.Contains(c.Stdin, "CREATE DATABASE `fresh`") {
		t.Fatalf("create first: %+v", c)
	}
	if c := calls[len(calls)-1]; c.Stdin != dumpText+"-- gz\n" || !strings.Contains(c.args(), "--database=fresh") {
		t.Fatalf("gz import %+v", c)
	}

	// PostgreSQL stops at the first error; the job fails with the server's message.
	e.run.respond = func(c call) (string, error) {
		if strings.Contains(c.args(), "--dbname=shop") {
			return "", errors.New(`psql:<stdin>:2: ERROR:  relation "t" already exists`)
		}
		return defaultRespond(c)
	}
	j, _ = e.m.Import("p", "shop", plain, false)
	done := e.wait(j)
	if done.State != "failed" || !strings.Contains(done.Error, `relation "t" already exists`) {
		t.Fatalf("failed job %+v", done)
	}
	if c := e.run.find("--dbname=shop"); !strings.Contains(c[0].args(), "--set=ON_ERROR_STOP=1") || c[0].Prog != "psql" {
		t.Fatalf("psql import %+v", c[0])
	}
	if !strings.Contains(e.sup.joinedLogs(), "import "+plain+" into shop: psql") {
		t.Fatalf("log:\n%s", e.sup.joinedLogs())
	}
}

func TestImportOneJobPerService(t *testing.T) {
	e := newEnv(t)
	e.running("m")
	plain := filepath.Join(e.dir, "shop.sql")
	os.WriteFile(plain, []byte(dumpText), 0o644)
	gate := make(chan struct{})
	e.run.respond = func(c call) (string, error) {
		if strings.Contains(c.args(), "--database=shop") {
			<-gate
		}
		return defaultRespond(c)
	}
	j, err := e.m.Import("m", "shop", plain, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.m.Import("m", "shop", plain, false)
	wantErr(t, err, 409, "another job")
	// Reset is refused while the job works (the server must also be stopped).
	e.sup.setState("m", model.StateStopped)
	_, err = e.m.Reset("m")
	wantErr(t, err, 409, "a job is working on Shop DB")
	close(gate)
	e.wait(j)
}

func TestExport(t *testing.T) {
	e := newEnv(t)
	e.running("m", "p")
	out := filepath.Join(e.dir, "out.sql")
	_, err := e.m.Export("m", "shop", filepath.Join(e.dir, "nofolder", "x.sql"), false)
	wantErr(t, err, 400, "folder")
	_, err = e.m.Export("m", "missing", out, false)
	wantErr(t, err, 400, "database missing does not exist")
	_, err = e.m.Export("m", "sys", out, false)
	wantErr(t, err, 400, "system database")

	j, err := e.m.Export("m", "shop", out, false)
	if err != nil || j.Type != model.JobDBExport || j.ServiceID != "m" {
		t.Fatalf("%+v %v", j, err)
	}
	done := e.wait(j)
	if done.State != "done" || done.Path != out {
		t.Fatalf("done %+v", done)
	}
	if b, _ := os.ReadFile(out); string(b) != dumpText {
		t.Fatalf("file %q", b)
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("the temporary file must be gone")
	}
	if c := e.run.find("--single-transaction"); len(c) != 1 || c[0].Prog != "mariadb-dump" ||
		!strings.HasSuffix(c[0].args(), "--single-transaction --routines --triggers --max-allowed-packet=1073741824 shop") {
		t.Fatalf("dump call %+v", c)
	}

	_, err = e.m.Export("m", "shop", out, false)
	wantErr(t, err, 409, out+" exists")

	// A failed export leaves the existing file alone and no .tmp behind.
	e.run.respond = func(c call) (string, error) {
		if c.Prog == "mariadb-dump" {
			return "partial", errors.New("mariadb-dump: Got error: 1045: Access denied")
		}
		return defaultRespond(c)
	}
	j, _ = e.m.Export("m", "shop", out, true)
	if done := e.wait(j); done.State != "failed" || !strings.Contains(done.Error, "Access denied") || done.Path != "" {
		t.Fatalf("failed %+v", done)
	}
	if b, _ := os.ReadFile(out); string(b) != dumpText {
		t.Fatal("a failed export must not touch the existing file")
	}
	if _, err := os.Stat(out + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("a failed export must not leave the temporary file")
	}
	e.run.respond = nil

	// .gz is compressed; overwrite replaces; PostgreSQL uses pg_dump --no-owner.
	gz := filepath.Join(e.dir, "out.sql.gz")
	os.WriteFile(gz, []byte("old"), 0o644)
	j, _ = e.m.Export("p", "shop", gz, true)
	if done := e.wait(j); done.State != "done" || done.Path != gz {
		t.Fatalf("gz %+v", done)
	}
	f, _ := os.Open(gz)
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); string(b) != dumpText {
		t.Fatalf("gz content %q", b)
	}
	if c := e.run.find("--no-owner"); len(c) != 1 || c[0].Prog != "pg_dump" || !strings.Contains(c[0].args(), "--dbname=shop") {
		t.Fatalf("pg_dump %+v", c)
	}
}

func TestSetPassword(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, err := e.m.SetPassword(ctx, "m", "x")
	wantErr(t, err, 409, "start Shop DB first")
	e.running("m", "p", "r")
	_, err = e.m.SetPassword(ctx, "m", "a\nb")
	wantErr(t, err, 400, "password must be")
	_, err = e.m.SetPassword(ctx, "n", "x")
	wantErr(t, err, 400, "not a database")

	const pw = "n3w'pw"
	sv, err := e.m.SetPassword(ctx, "m", pw)
	if err != nil || sv.AdminPassword != pw || e.svc("m").AdminPassword != pw {
		t.Fatalf("%+v %v", sv, err)
	}
	alter := e.run.find("ALTER USER")
	want := "ALTER USER 'root'@'127.0.0.1' IDENTIFIED BY 'n3w''pw';\nALTER USER 'root'@'::1' IDENTIFIED BY 'n3w''pw';\nALTER USER 'root'@'localhost' IDENTIFIED BY 'n3w''pw';\nFLUSH PRIVILEGES;\n"
	if len(alter) != 1 || alter[0].Stdin != want {
		t.Fatalf("alter %q", alter[0].Stdin)
	}
	// From now on the clients log in with it.
	e.m.Info(ctx, "m")
	all := e.run.all()
	if v, _ := all[len(all)-1].env("MYSQL_PWD"); v != pw {
		t.Fatalf("MYSQL_PWD %q", v)
	}
	// Removing it: IDENTIFIED BY '' while logging in with the old one.
	sv, err = e.m.SetPassword(ctx, "m", "")
	if err != nil || sv.AdminPassword != "" {
		t.Fatal(err)
	}
	last := e.run.find("ALTER USER")
	if l := last[len(last)-1]; !strings.Contains(l.Stdin, "IDENTIFIED BY '';") {
		t.Fatalf("remove %q", l.Stdin)
	} else if v, _ := l.env("MYSQL_PWD"); v != pw {
		t.Fatalf("must log in with the old password, got %q", v)
	}

	if _, err := e.m.SetPassword(ctx, "p", pw); err != nil {
		t.Fatal(err)
	}
	if c := e.run.find("ALTER USER postgres"); len(c) != 1 || c[0].Stdin != "ALTER USER postgres WITH PASSWORD 'n3w''pw';\n" {
		t.Fatalf("postgres %+v", c)
	}
	e.m.Info(ctx, "p")
	all = e.run.all()
	if v, _ := all[len(all)-1].env("PGPASSWORD"); v != pw {
		t.Fatal("PGPASSWORD")
	}

	if _, err := e.m.SetPassword(ctx, "r", pw); err != nil {
		t.Fatal(err)
	}
	c := e.run.find("requirepass")
	if len(c) != 1 || c[0].Prog != "redis-cli" || c[0].args() != "-h 127.0.0.1 -p 6379 -x CONFIG SET requirepass" || c[0].Stdin != pw {
		t.Fatalf("redis %+v", c)
	}
	if _, ok := c[0].env("REDISCLI_AUTH"); ok {
		t.Fatal("no REDISCLI_AUTH before a password exists")
	}
	if _, err := e.m.SetPassword(ctx, "r", ""); err != nil {
		t.Fatal(err)
	}
	c = e.run.find("requirepass")
	if v, _ := c[1].env("REDISCLI_AUTH"); v != pw || c[1].Stdin != "" || e.svc("r").AdminPassword != "" {
		t.Fatalf("redis remove %+v", c[1])
	}
	e.noSecretInArgs(pw)

	// A Redis error reply fails it, and nothing is stored.
	e.run.respond = func(c call) (string, error) { return "ERR unknown\n", nil }
	_, err = e.m.SetPassword(ctx, "r", "zzz")
	wantErr(t, err, 409, "ERR unknown")
	if e.svc("r").AdminPassword != "" {
		t.Fatal("a failed change must not be stored")
	}
}

func TestReset(t *testing.T) {
	e := newEnv(t)
	e.st.Update(func(d *store.Data) error {
		d.Services[0].AdminPassword = "pw"
		d.Services = append(d.Services, model.Service{ID: "c", Name: "Custom", Kind: "mariadb", Port: 3310, CustomArgs: true})
		return nil
	})
	data := filepath.Join(e.sup.home, "mariadb", "m")
	os.MkdirAll(filepath.Join(data, "shop"), 0o755)
	os.WriteFile(filepath.Join(data, "ibdata1"), []byte("x"), 0o644)

	e.running("m")
	_, err := e.m.Reset("m")
	wantErr(t, err, 409, "stop Shop DB first")
	e.sup.setState("m", model.StateStopped)
	_, err = e.m.Reset("c")
	wantErr(t, err, 409, "own command line")
	_, err = e.m.Reset("n")
	wantErr(t, err, 400, "not a database")

	release, _ := e.sup.Reserve([]string{"m"}, "taking a snapshot")
	_, err = e.m.Reset("m")
	wantErr(t, err, 409, "busy")
	release()

	sv, err := e.m.Reset("m")
	if err != nil || sv.AdminPassword != "" || e.svc("m").AdminPassword != "" {
		t.Fatalf("%+v %v", sv, err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("the data folder must be gone")
	}
	if len(e.sup.reserved) != 0 || e.sup.reserves != 2 {
		t.Fatalf("reserve %v %d", e.sup.reserved, e.sup.reserves)
	}
	if !strings.Contains(e.sup.joinedLogs(), "reset: deleting the data folder "+data) {
		t.Fatal("log")
	}
	// Reset with no data folder is fine too.
	if _, err := e.m.Reset("m"); err != nil {
		t.Fatal(err)
	}
}

func TestListRemoteAndCopyFromServer(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	src := Target{Kind: "mysql", Host: "10.0.0.5", Port: 3306, User: "admin", Password: "remote-pw", ClientDir: filepath.Join(e.dir, "mysql", "bin")}
	dbs, err := e.m.ListRemote(ctx, src)
	if err != nil || len(dbs) != 1 || dbs[0].Name != "shop" {
		t.Fatalf("%+v %v", dbs, err)
	}
	c := e.run.all()[0]
	if c.Prog != "mysql" || !strings.Contains(c.args(), "--host=10.0.0.5 --port=3306 --protocol=tcp --user=admin") {
		t.Fatalf("list call %+v", c)
	}
	if v, _ := c.env("MYSQL_PWD"); v != "remote-pw" {
		t.Fatal("remote password")
	}
	_, err = e.m.ListRemote(ctx, Target{Kind: "redis", Port: 6379})
	wantErr(t, err, 400, "MariaDB, MySQL or PostgreSQL")
	_, err = e.m.ListRemote(ctx, Target{Kind: "mysql"})
	wantErr(t, err, 400, "port")

	err = e.m.CopyFromServer(ctx, src, []string{"blog"}, "m", nil)
	wantErr(t, err, 409, "start Shop DB first")
	e.running("m", "p")
	wantErr(t, e.m.CopyFromServer(ctx, src, []string{"blog"}, "p", nil), 400, "cannot be copied into PG")
	wantErr(t, e.m.CopyFromServer(ctx, src, []string{"shop"}, "m", nil), 409, "shop already exists in Shop DB")
	wantErr(t, e.m.CopyFromServer(ctx, src, []string{"mysql"}, "m", nil), 400, "system database")
	wantErr(t, e.m.CopyFromServer(ctx, src, nil, "m", nil), 400, "at least one")

	var steps []string
	progress := func(p int, s string) { steps = append(steps, fmt.Sprintf("%d %s", p, s)) }
	if err := e.m.CopyFromServer(ctx, src, []string{"blog", "wiki"}, "m", progress); err != nil {
		t.Fatal(err)
	}
	if strings.Join(steps, ",") != "0 Copying blog,50 Copying wiki,100 Copied" {
		t.Fatalf("steps %q", steps)
	}
	dump := e.run.find("--single-transaction")
	if len(dump) != 2 || dump[0].Prog != "mysqldump" || !strings.Contains(dump[0].args(), "--host=10.0.0.5") || !strings.HasSuffix(dump[0].args(), " blog") {
		t.Fatalf("dump %+v", dump)
	}
	imp := e.run.find("--database=blog")
	if len(imp) != 1 || imp[0].Prog != "mariadb" || imp[0].Stdin != dumpText || !strings.Contains(imp[0].args(), "--host=127.0.0.1 --port=3307") {
		t.Fatalf("import %+v", imp)
	}
	if v, _ := imp[0].env("MYSQL_PWD"); v != "" {
		t.Fatal("the import uses the service's own password")
	}
	if len(e.run.find("CREATE DATABASE `wiki`")) != 1 {
		t.Fatal("each database is created first")
	}
	e.noSecretInArgs("remote-pw")

	// A failed import drops what this copy created.
	e.run.respond = func(c call) (string, error) {
		if strings.Contains(c.args(), "--database=b2") {
			io.WriteString(io.Discard, c.Stdin)
			return "", errors.New("mariadb: ERROR 1146 at line 3: Table doesn't exist")
		}
		return defaultRespond(c)
	}
	err = e.m.CopyFromServer(ctx, src, []string{"a2", "b2"}, "m", nil)
	if err == nil || !strings.Contains(err.Error(), "importing b2 failed") || !strings.Contains(err.Error(), "ERROR 1146") {
		t.Fatalf("got %v", err)
	}
	if d := e.run.find("DROP DATABASE"); len(d) != 1 || d[0].Stdin != "DROP DATABASE IF EXISTS `a2`;\nDROP DATABASE IF EXISTS `b2`;\n" {
		t.Fatalf("drop %+v", d)
	}

	// A failed dump is reported as the dump's error.
	e.run.respond = func(c call) (string, error) {
		if c.Prog == "mysqldump" {
			return "", errors.New("mysqldump: Got error: 1045: Access denied for user 'admin'")
		}
		return defaultRespond(c)
	}
	err = e.m.CopyFromServer(ctx, src, []string{"c3"}, "m", nil)
	if err == nil || !strings.Contains(err.Error(), "dumping c3 from 10.0.0.5:3306 failed") || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("got %v", err)
	}
}

func TestCopyUsesDefaultRuntimeClients(t *testing.T) {
	e := newEnv(t)
	e.running("p")
	src := Target{Kind: "postgres", Host: "db.local", Port: 5433, Password: "x"}
	if err := e.m.CopyFromServer(context.Background(), src, []string{"blog"}, "p", nil); err != nil {
		t.Fatal(err)
	}
	d := e.run.find("--no-owner")
	if len(d) != 1 || d[0].Prog != "pg_dump" || !strings.Contains(d[0].args(), "--host=db.local --port=5433 --username=postgres") {
		t.Fatalf("pg_dump %+v", d)
	}
	if v, _ := d[0].env("PGPASSWORD"); v != "x" {
		t.Fatal("PGPASSWORD")
	}
}

func TestDropDatabases(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	wantErr(t, e.m.DropDatabases(ctx, "p", []string{"blog"}), 409, "start PG first")
	e.running("p")
	wantErr(t, e.m.DropDatabases(ctx, "p", []string{"postgres"}), 400, "system database")
	if err := e.m.DropDatabases(ctx, "p", []string{"blog", "wiki"}); err != nil {
		t.Fatal(err)
	}
	if d := e.run.find("DROP DATABASE"); len(d) != 1 || d[0].Stdin != "DROP DATABASE IF EXISTS \"blog\";\nDROP DATABASE IF EXISTS \"wiki\";\n" {
		t.Fatalf("%+v", d)
	}
}

func TestWriteAtomicAndOpenDump(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.sql.GZ")
	if err := writeAtomic(p, func(w io.Writer) error { _, err := io.WriteString(w, "hello"); return err }); err != nil {
		t.Fatal(err)
	}
	var read int64
	r, c, err := openDump(p, func(n int64) { read = n })
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	c.Close()
	st, _ := os.Stat(p)
	if string(b) != "hello" || read != st.Size() {
		t.Fatalf("%q read %d of %d", b, read, st.Size())
	}
	// A file that starts like gzip but is not.
	bad := filepath.Join(dir, "bad.sql")
	os.WriteFile(bad, []byte{0x1f, 0x8b, 0, 0}, 0o644)
	if _, _, err := openDump(bad, nil); err == nil {
		t.Fatal("a broken gzip file must be refused")
	}
}

// ---------- the default runner, with the test binary as the program ----------

func TestMain(m *testing.M) {
	if os.Getenv("Z2D_DBTOOLS_HELPER") == "1" {
		in, _ := io.ReadAll(os.Stdin)
		fmt.Print("stdin=" + string(in) + " pwd=" + os.Getenv("MYSQL_PWD") + " args=" + strings.Join(os.Args[1:], ","))
		if os.Getenv("Z2D_FAIL") == "1" {
			for i := 1; i <= 12; i++ {
				fmt.Fprintf(os.Stderr, "line %d\n", i)
			}
			fmt.Fprint(os.Stderr, "ERROR 1045: Access denied")
			os.Exit(1)
		}
		if os.Getenv("Z2D_SLEEP") == "1" {
			time.Sleep(10 * time.Second)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestDefaultRunner(t *testing.T) {
	self, _ := os.Executable()
	var out bytes.Buffer
	err := DefaultRunner(context.Background(), Cmd{Bin: self, Args: []string{"-test.run=^$", "a b"},
		Env: []string{"Z2D_DBTOOLS_HELPER=1", "MYSQL_PWD=pw"}, Stdin: strings.NewReader("SELECT 1;"), Stdout: &out})
	if err != nil || out.String() != "stdin=SELECT 1; pwd=pw args=-test.run=^$,a b" {
		t.Fatalf("%q %v", out.String(), err)
	}
	err = DefaultRunner(context.Background(), Cmd{Bin: self, Env: []string{"Z2D_DBTOOLS_HELPER=1", "Z2D_FAIL=1"}})
	if err == nil || !strings.Contains(err.Error(), "line 6; line 7") || strings.Contains(err.Error(), "line 5;") ||
		!strings.HasSuffix(err.Error(), "ERROR 1045: Access denied") {
		t.Fatalf("stderr tail: %v", err)
	}
	start := time.Now()
	err = DefaultRunner(context.Background(), Cmd{Bin: self, Env: []string{"Z2D_DBTOOLS_HELPER=1", "Z2D_SLEEP=1"}, Timeout: 300 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "timed out") || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout: %v after %s", err, time.Since(start))
	}
	err = DefaultRunner(context.Background(), Cmd{Bin: filepath.Join(t.TempDir(), "missing.exe")})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing program: %v", err)
	}
}
