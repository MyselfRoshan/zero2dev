package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestLaunchRuntime(t *testing.T) {
	e := newEnv(t)
	exe := func(name string) string {
		p := filepath.Join(t.TempDir(), name)
		os.WriteFile(p, []byte("x"), 0o755)
		return p
	}
	code, dbeaver, heidi, node := exe("Code.exe"), exe("dbeaver.exe"), exe("heidisql.exe"), exe("node.exe")
	project := t.TempDir()
	e.srv.Store.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes,
			model.Runtime{ID: "rt_code", Kind: "vscode", Name: "VS Code", Binary: code},
			model.Runtime{ID: "rt_dbv", Kind: "dbeaver", Name: "DBeaver", Binary: dbeaver},
			model.Runtime{ID: "rt_heidi", Kind: "heidisql", Name: "HeidiSQL", Binary: heidi},
			model.Runtime{ID: "rt_node", Kind: "node", Name: "Node.js", Binary: node},
			model.Runtime{ID: "rt_gone", Kind: "vscode", Name: "Old Code", Binary: filepath.Join(project, "missing.exe")})
		d.Services = append(d.Services,
			model.Service{ID: "svc_pg", Name: "pg", Kind: "postgres", Port: 5432},
			model.Service{ID: "svc_redis", Name: "cache", Kind: "redis", Port: 6379},
			model.Service{ID: "svc_api", Name: "api", Kind: "node", Port: 3000})
		return nil
	})

	type call struct {
		bin, dir string
		args     []string
	}
	var calls []call
	e.srv.Launch = func(bin string, args []string, dir string) error {
		calls = append(calls, call{bin, dir, args})
		return nil
	}
	last := func() call { return calls[len(calls)-1] }

	// Editor on a project folder.
	if r := e.do("POST", "/api/runtimes/rt_code/launch", map[string]string{"path": project}, nil); r.StatusCode != 204 {
		t.Fatalf("editor: %d", r.StatusCode)
	}
	if c := last(); c.bin != code || c.dir != project || strings.Join(c.args, " ") != project {
		t.Fatalf("editor call %+v", c)
	}
	// No body: just open the program.
	if r := e.do("POST", "/api/runtimes/rt_code/launch", nil, nil); r.StatusCode != 204 || len(last().args) != 0 {
		t.Fatalf("no body: %d %+v", r.StatusCode, last())
	}
	// DBeaver opens a PostgreSQL service, with its workspace in the data folder.
	if r := e.do("POST", "/api/runtimes/rt_dbv/launch", map[string]string{"serviceId": "svc_pg"}, nil); r.StatusCode != 204 {
		t.Fatalf("dbeaver: %d", r.StatusCode)
	}
	ws := filepath.Join(e.home, "tools", "dbeaver-workspace")
	if c := last(); c.args[0] != "-data" || c.args[1] != ws || c.args[2] != "-con" || !strings.Contains(c.args[3], "driver=PostgreSQL|host=127.0.0.1|port=5432|user=postgres") {
		t.Fatalf("dbeaver call %+v", c)
	}
	if st, err := os.Stat(ws); err != nil || !st.IsDir() {
		t.Fatal("workspace folder must exist")
	}
	if r := e.do("POST", "/api/runtimes/rt_heidi/launch", map[string]string{"serviceId": "svc_pg"}, nil); r.StatusCode != 204 || last().args[0] != "--nettype=8" {
		t.Fatalf("heidisql: %d %+v", r.StatusCode, last())
	}

	n := len(calls)
	e.errCode("POST", "/api/runtimes/rt_node/launch", nil, 400)                                          // a runtime, not a program
	e.errCode("POST", "/api/runtimes/rt_gone/launch", nil, 409)                                          // binary missing
	e.errCode("POST", "/api/runtimes/rt_nope/launch", nil, 404)                                          // unknown runtime
	e.errCode("POST", "/api/runtimes/rt_code/launch", map[string]string{"path": "relative"}, 400)        // relative path
	e.errCode("POST", "/api/runtimes/rt_code/launch", map[string]string{"path": project + "x"}, 400)     // missing path
	e.errCode("POST", "/api/runtimes/rt_code/launch", map[string]string{"serviceId": "svc_pg"}, 400)     // editors open folders
	e.errCode("POST", "/api/runtimes/rt_heidi/launch", map[string]string{"serviceId": "svc_redis"}, 400) // no Redis support
	e.errCode("POST", "/api/runtimes/rt_heidi/launch", map[string]string{"serviceId": "svc_api"}, 400)   // not a database
	e.errCode("POST", "/api/runtimes/rt_heidi/launch", map[string]string{"serviceId": "svc_x"}, 404)
	if len(calls) != n {
		t.Fatalf("refused requests must not launch anything: %+v", calls[n:])
	}

	e.srv.Launch = nil
	if c := e.errCode("POST", "/api/runtimes/rt_code/launch", nil, 501); c != "not_supported" {
		t.Fatal(c)
	}
}
