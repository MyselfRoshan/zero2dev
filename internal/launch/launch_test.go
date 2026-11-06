package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if err := Start(os.Args[0], []string{"-test.run=^$"}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := Start(filepath.Join(t.TempDir(), "missing.exe"), nil, ""); err == nil || !strings.Contains(err.Error(), "missing.exe") {
		t.Fatalf("a missing program must be reported: %v", err)
	}
}
