package supervisor

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// dbHelper lets the test binary act as a fake database, picked by the name it
// was started as (copies are made under the real tool names):
//   - mariadbd: listens on --port and exits cleanly when it receives "shutdown"
//   - mariadb-install-db: creates --datadir with a marker file
//   - mariadb-admin: sends "shutdown" to --port
//
// It reports whether it handled the process (TestMain then returns).
func dbHelper() bool {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	arg := func(prefix string) string {
		for _, a := range os.Args[1:] {
			if v, ok := strings.CutPrefix(a, prefix); ok {
				return v
			}
		}
		return ""
	}
	switch name {
	case "mariadbd":
		l, err := net.Listen("tcp", "127.0.0.1:"+arg("--port="))
		if err != nil {
			os.Exit(2)
		}
		println("ready for connections") // stderr, like a real server
		for {
			c, err := l.Accept()
			if err != nil {
				os.Exit(3)
			}
			line, _ := bufio.NewReader(c).ReadString('\n')
			c.Close()
			if strings.TrimSpace(line) == "shutdown" {
				println("shutdown complete")
				os.Exit(0)
			}
		}
	case "mariadb-install-db":
		dir := arg("--datadir=")
		if os.Getenv("Z2D_FAIL_INIT") == "1" {
			os.MkdirAll(dir, 0o755) // half-made folder must be cleaned up
			println("cannot create the system tables")
			os.Exit(1)
		}
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "ibdata1"), []byte("x"), 0o644)
		println("Creation of the database was successful")
		os.Exit(0)
	case "mariadb-admin":
		c, err := net.Dial("tcp", "127.0.0.1:"+arg("--port="))
		if err != nil {
			os.Exit(4)
		}
		io.WriteString(c, "shutdown\n")
		c.Close()
		os.Exit(0)
	}
	return false
}

// fakeMariaDB copies the test binary under the MariaDB tool names.
func fakeMariaDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	self, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	for _, n := range []string{"mariadbd", "mariadb-install-db", "mariadb-admin"} {
		if err := os.WriteFile(filepath.Join(dir, n+ext), self, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "mariadbd"+ext)
}

func TestDefaultDBArgsAndPlan(t *testing.T) {
	maria := defaultDBArgs("mariadb", `C:\z\databases\svc_1`)
	if maria[0] != "--no-defaults" || maria[1] != `--datadir=C:\z\databases\svc_1` || maria[2] != "--port={port}" || maria[3] != "--bind-address=127.0.0.1" {
		t.Fatalf("mariadb %v", maria)
	}
	if my := defaultDBArgs("mysql", "D"); my[len(my)-1] != "--mysqlx=OFF" {
		t.Fatalf("mysql %v", my)
	}
	if pg := strings.Join(defaultDBArgs("postgres", "D"), " "); pg != "-D D -p {port} -c listen_addresses=127.0.0.1" {
		t.Fatal(pg)
	}
	if rd := strings.Join(defaultDBArgs("redis", `C:\a\b`), " "); rd != "--port {port} --bind 127.0.0.1 --dir C:/a/b" && runtime.GOOS == "windows" {
		t.Fatal(rd)
	}
	if defaultDBArgs("node", "D") != nil {
		t.Fatal("not a database")
	}

	s := New(t.TempDir(), nil, nil)
	bin := fakeMariaDB(t)
	p := s.planDB(model.Service{ID: "svc_1", Kind: "mariadb", Port: 3307}, bin, false)
	if p.data != filepath.Join(s.home, "databases", "svc_1") || filepath.Base(p.initBin) == "" || !strings.HasPrefix(filepath.Base(p.initBin), "mariadb-install-db") {
		t.Fatalf("plan %+v", p)
	}
	if !strings.HasPrefix(filepath.Base(p.stopBin), "mariadb-admin") || !strings.Contains(strings.Join(p.stopArgs, " "), "--port=3307 --protocol=tcp shutdown") {
		t.Fatalf("stop %+v", p)
	}
	custom := s.planDB(model.Service{ID: "svc_1", Kind: "mariadb", Port: 3307}, bin, true)
	if custom.data != "" || custom.initBin != "" || custom.stopBin == "" {
		t.Fatalf("custom args: no setup, but still a clean stop: %+v", custom)
	}
	if s.planDB(model.Service{Kind: "node"}, bin, false) != nil {
		t.Fatal("not a database")
	}
	// No tools next to the binary: nothing to run, the folder is still created on start.
	lone := filepath.Join(t.TempDir(), "postgres.exe")
	if p := s.planDB(model.Service{ID: "x", Kind: "postgres", Port: 5432}, lone, false); p.initBin != "" || p.stopBin != "" || p.data == "" {
		t.Fatalf("postgres without tools %+v", p)
	}
}

func dbSetup(t *testing.T) (*Supervisor, *store.Store, string, int) {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(home, st, &recorder{})
	s.KillGrace, s.DBGrace, s.HealthTimeout = 800*time.Millisecond, 5*time.Second, 5*time.Second
	port := freePort(t)
	bin := fakeMariaDB(t)
	st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_m", Kind: "mariadb", Binary: bin, Default: true})
		d.Services = append(d.Services, model.Service{ID: "svc_db", Name: "db", Kind: "mariadb", Cwd: home, Port: port, Env: map[string]string{}})
		return nil
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.StopAll(ctx)
	})
	return s, st, home, port
}

func TestDatabaseFirstStartAndCleanStop(t *testing.T) {
	s, _, home, _ := dbSetup(t)
	data := filepath.Join(home, "databases", "svc_db")

	if err := s.Start("svc_db"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, "svc_db", "running", 5*time.Second)
	if _, err := os.Stat(filepath.Join(data, "ibdata1")); err != nil {
		t.Fatal("first start must create the database files")
	}
	if err := s.StopWait("svc_db"); err != nil {
		t.Fatal(err)
	}
	logs := joinLogs(s.Logs("svc_db", 200))
	if !strings.Contains(logs, "creating the database files with mariadb-install-db") ||
		!strings.Contains(logs, "asking mariadb-admin to shut the server down") ||
		!strings.Contains(logs, "shutdown complete") || strings.Contains(logs, "did not shut down in time") {
		t.Fatalf("logs:\n%s", logs)
	}
	if s.State("svc_db") != "stopped" {
		t.Fatalf("state %s", s.State("svc_db"))
	}

	// Second start: the files exist, no new setup.
	os.WriteFile(filepath.Join(data, "keep"), []byte("x"), 0o644)
	if err := s.Start("svc_db"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, "svc_db", "running", 5*time.Second)
	if strings.Count(joinLogs(s.Logs("svc_db", 400)), "creating the database files") != 1 {
		t.Fatal("setup must run only once")
	}
	if _, err := os.Stat(filepath.Join(data, "keep")); err != nil {
		t.Fatal("existing data must be kept")
	}
}

func TestDatabaseFailedSetupCleansUp(t *testing.T) {
	s, st, home, _ := dbSetup(t)
	st.Update(func(d *store.Data) error {
		d.Services[0].Env["Z2D_FAIL_INIT"] = "1"
		return nil
	})
	err := s.Start("svc_db")
	if err == nil || !strings.Contains(err.Error(), "creating the database files failed") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "databases", "svc_db")); !os.IsNotExist(err) {
		t.Fatal("a failed setup must not leave a half-made data folder")
	}
	if !strings.Contains(joinLogs(s.Logs("svc_db", 50)), "cannot create the system tables") {
		t.Fatal("the setup tool's output must be in the log")
	}
	if s.State("svc_db") != "stopped" {
		t.Fatalf("state %s", s.State("svc_db"))
	}
}

func joinLogs(ls []model.LogLine) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}
