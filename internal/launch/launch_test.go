package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/model"
)

func TestConnectArgs(t *testing.T) {
	maria := model.Service{Name: "Shop DB", Kind: "mariadb", Port: 3307}
	pg := model.Service{Name: "pg", Kind: "postgres", Port: 5432}
	cases := []struct {
		tool string
		svc  model.Service
		want string
	}{
		{"heidisql", maria, "--nettype=0 --description=Shop DB --host=127.0.0.1 --port=3307 --user=root"},
		{"heidisql", pg, "--nettype=8 --description=pg --host=127.0.0.1 --port=5432 --user=postgres --databases=postgres"},
		{"dbeaver", maria, "-con driver=MariaDB|host=127.0.0.1|port=3307|user=root|name=Shop DB|connect=true"},
		{"dbeaver", model.Service{Name: "m", Kind: "mysql", Port: 3306}, "-con driver=MySQL|host=127.0.0.1|port=3306|user=root|name=m|connect=true"},
		{"dbeaver", pg, "-con driver=PostgreSQL|host=127.0.0.1|port=5432|user=postgres|name=pg|connect=true|database=postgres"},
	}
	for _, c := range cases {
		got, err := ConnectArgs(c.tool, c.svc)
		if err != nil || strings.Join(got, " ") != c.want {
			t.Errorf("%s %s:\n got  %q %v\n want %q", c.tool, c.svc.Kind, strings.Join(got, " "), err, c.want)
		}
	}
	if _, err := ConnectArgs("heidisql", model.Service{Kind: "redis", Port: 6379}); err != ErrNoConnection {
		t.Errorf("redis: %v", err)
	}
	if _, err := ConnectArgs("dbeaver", model.Service{Kind: "mysql"}); err == nil {
		t.Error("no port must be refused")
	}
	if _, err := ConnectArgs("vscode", maria); err != ErrNoConnection {
		t.Errorf("editor: %v", err)
	}
	if DBUser("redis") != "" || DBUser("mysql") != "root" {
		t.Error("DBUser")
	}
}

func TestStartReturnsWithoutWaiting(t *testing.T) {
	// The test binary with a filter that matches nothing exits at once.
	if err := Start(os.Args[0], []string{"-test.run=^$"}, t.TempDir(), []string{"Z2D_X=1"}); err != nil {
		t.Fatal(err)
	}
	if err := Start(filepath.Join(t.TempDir(), "missing.exe"), nil, "", nil); err == nil || !strings.Contains(err.Error(), "missing.exe") {
		t.Fatalf("a missing program must be reported: %v", err)
	}
}

func TestConnectArgsWithPassword(t *testing.T) {
	maria := model.Service{Name: "Shop DB", Kind: "mariadb", Port: 3307, AdminPassword: "s3cret"}
	got, _ := ConnectArgs("heidisql", maria)
	if strings.Join(got, " ") != "--nettype=0 --description=Shop DB --host=127.0.0.1 --port=3307 --user=root --password=s3cret" {
		t.Errorf("heidisql: %q", got)
	}
	got, _ = ConnectArgs("dbeaver", maria)
	if strings.Join(got, " ") != "-con driver=MariaDB|host=127.0.0.1|port=3307|user=root|password=s3cret|name=Shop DB|connect=true" {
		t.Errorf("dbeaver: %q", got)
	}
	// "|" cannot be written in DBeaver's -con string: DBeaver asks instead.
	maria.AdminPassword = "a|b"
	got, _ = ConnectArgs("dbeaver", maria)
	if strings.Contains(strings.Join(got, " "), "password") {
		t.Errorf("dbeaver with |: %q", got)
	}
}

func TestToolDataArgsAPIClients(t *testing.T) {
	home := t.TempDir()
	args, env, err := ToolDataArgs(model.Runtime{Kind: "bruno", Source: "installed"}, home)
	want := "--user-data-dir=" + filepath.Join(home, "bruno")
	if err != nil || len(args) != 1 || args[0] != want || len(env) != 0 {
		t.Fatalf("bruno: %v %v %v", args, env, err)
	}
	hopp := model.Runtime{Kind: "hoppscotch", Source: "installed"}
	args, env, err = ToolDataArgs(hopp, home)
	if err != nil || len(args) != 0 || len(env) != 1 || env[0] != "WEBVIEW2_USER_DATA_FOLDER="+filepath.Join(home, "hoppscotch", "webview2") {
		t.Fatalf("hoppscotch: %v %v %v", args, env, err)
	}
	if d := ToolWorkDir(hopp, home); d != filepath.Join(home, "hoppscotch") {
		t.Fatalf("work dir %q", d)
	}
	if st, err := os.Stat(filepath.Join(home, "hoppscotch")); err != nil || !st.IsDir() {
		t.Fatal("data hoppscotch must be created")
	}
	for _, rt := range []model.Runtime{{Kind: "bruno", Source: "registered"}, {Kind: "hoppscotch", Source: "registered"}, {Kind: "vscode", Source: "installed"}} {
		if d := ToolWorkDir(rt, home); d != "" {
			t.Errorf("%s/%s must not get a work dir, got %q", rt.Kind, rt.Source, d)
		}
	}
	if a, e, err := ToolDataArgs(model.Runtime{Kind: "bruno", Source: "registered"}, home); a != nil || e != nil || err != nil {
		t.Fatalf("own install untouched: %v %v %v", a, e, err)
	}
}

// TestHelperEnvProcess is run by TestStartDropsElectronRunAsNode as the started program.
func TestHelperEnvProcess(t *testing.T) {
	out := os.Getenv("Z2D_ENV_OUT")
	if out == "" {
		t.Skip("helper")
	}
	_, set := os.LookupEnv("ELECTRON_RUN_AS_NODE")
	os.WriteFile(out, []byte(strings.Join([]string{map[bool]string{true: "set", false: "unset"}[set], os.Getenv("Z2D_EXTRA")}, " ")), 0o644)
}

func TestStartDropsElectronRunAsNode(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env.txt")
	t.Setenv("ELECTRON_RUN_AS_NODE", "1")
	t.Setenv("Z2D_ENV_OUT", out)
	if err := Start(os.Args[0], []string{"-test.run=^TestHelperEnvProcess$"}, t.TempDir(), []string{"Z2D_EXTRA=yes"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
			if string(b) != "unset yes" {
				t.Fatalf("child env: %q", b)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the started program wrote nothing")
}
