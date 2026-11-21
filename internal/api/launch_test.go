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
	npp, codium, myCode := exe("notepad++.exe"), exe("VSCodium.exe"), exe("Code.exe")
	project := t.TempDir()
	e.srv.Store.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes,
			model.Runtime{ID: "rt_code", Kind: "vscode", Name: "VS Code", Binary: code, Source: "installed"},
			model.Runtime{ID: "rt_dbv", Kind: "dbeaver", Name: "DBeaver", Binary: dbeaver, Source: "installed"},
			model.Runtime{ID: "rt_heidi", Kind: "heidisql", Name: "HeidiSQL", Binary: heidi, Source: "installed"},
			model.Runtime{ID: "rt_npp", Kind: "notepadpp", Name: "Notepad++", Binary: npp, Source: "installed"},
			model.Runtime{ID: "rt_codium", Kind: "vscodium", Name: "VSCodium", Binary: codium, Source: "installed"},
			model.Runtime{ID: "rt_mycode", Kind: "vscode", Name: "My VS Code", Binary: myCode, Source: "registered"},
			model.Runtime{ID: "rt_node", Kind: "node", Name: "Node.js", Binary: node},
			model.Runtime{ID: "rt_gone", Kind: "vscode", Name: "Old Code", Binary: filepath.Join(project, "missing.exe")})
		d.Services = append(d.Services,
			model.Service{ID: "svc_pg", Name: "pg", Kind: "postgres", Port: 5432},
			model.Service{ID: "svc_redis", Name: "cache", Kind: "redis", Port: 6379},
			model.Service{ID: "svc_api", Name: "api", Kind: "node", Port: 3000})
		return nil
	})

	type call struct {
		bin, dir  string
		args, env []string
	}
	var calls []call
	e.srv.Launch = func(bin string, args []string, dir string, env []string) error {
		calls = append(calls, call{bin, dir, args, env})
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
	// Data of Zero2Dev-installed tools lives in <data>\<name>, never in bin.
	codeData := filepath.Join(e.home, "code")
	if c := last(); strings.Join(c.env, " ") != "VSCODE_PORTABLE="+codeData {
		t.Fatalf("VS Code must keep its data in %s: %+v", codeData, c.env)
	}
	if st, err := os.Stat(codeData); err != nil || !st.IsDir() {
		t.Fatal("data\\code must exist")
	}
	e.do("POST", "/api/runtimes/rt_codium/launch", nil, nil)
	if c := last(); strings.Join(c.env, " ") != "VSCODE_PORTABLE="+filepath.Join(e.home, "codium") {
		t.Fatalf("VSCodium %+v", c.env)
	}
	e.do("POST", "/api/runtimes/rt_npp/launch", map[string]string{"path": project}, nil)
	if c := last(); strings.Join(c.args, " ") != "-settingsDir="+filepath.Join(e.home, "notepadpp")+" "+project {
		t.Fatalf("Notepad++ %+v", c.args)
	}
	// A VS Code registered by path is the user's own install: its usual settings stay.
	e.do("POST", "/api/runtimes/rt_mycode/launch", map[string]string{"path": project}, nil)
	if c := last(); len(c.env) != 0 || strings.Join(c.args, " ") != project {
		t.Fatalf("registered editor must be left alone: %+v", c)
	}
	// No body: just open the program.
	if r := e.do("POST", "/api/runtimes/rt_code/launch", nil, nil); r.StatusCode != 204 || len(last().args) != 0 {
		t.Fatalf("no body: %d %+v", r.StatusCode, last())
	}
	// DBeaver opens a PostgreSQL service, with its workspace in the data folder.
	if r := e.do("POST", "/api/runtimes/rt_dbv/launch", map[string]string{"serviceId": "svc_pg"}, nil); r.StatusCode != 204 {
		t.Fatalf("dbeaver: %d", r.StatusCode)
	}
	ws := filepath.Join(e.home, "dbeaver", "workspace")
	if c := last(); c.args[0] != "-data" || c.args[1] != ws || c.args[2] != "-con" || !strings.Contains(c.args[3], "driver=PostgreSQL|host=127.0.0.1|port=5432|user=postgres") {
		t.Fatalf("dbeaver call %+v", c)
	}
	if st, err := os.Stat(ws); err != nil || !st.IsDir() {
		t.Fatal("workspace folder must exist")
	}
	heidiSettings := filepath.Join(e.home, "heidisql", "portable_settings.txt")
	if r := e.do("POST", "/api/runtimes/rt_heidi/launch", map[string]string{"serviceId": "svc_pg"}, nil); r.StatusCode != 204 ||
		last().args[0] != "--psettings="+heidiSettings || last().args[1] != "--nettype=8" {
		t.Fatalf("heidisql: %d %+v", r.StatusCode, last())
	}
	if _, err := os.Stat(heidiSettings); err != nil {
		t.Fatal("HeidiSQL's settings file must be created in data\\heidisql")
	}
	if err := os.WriteFile(heidiSettings, []byte("saved"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := e.do("POST", "/api/runtimes/rt_heidi/launch", nil, nil); r.StatusCode != 204 {
		t.Fatalf("heidisql relaunch: %d", r.StatusCode)
	}
	if b, _ := os.ReadFile(heidiSettings); string(b) != "saved" {
		t.Fatalf("a later launch must keep HeidiSQL's settings, got %q", b)
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

func TestLaunchAPIClients(t *testing.T) {
	e := newEnv(t)
	exe := func(name string) string {
		p := filepath.Join(t.TempDir(), name)
		os.WriteFile(p, []byte("x"), 0o755)
		return p
	}
	bruno, hopp, myBruno := exe("Bruno.exe"), exe("hoppscotch-desktop.exe"), exe("Bruno.exe")
	project := t.TempDir()
	e.srv.Store.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes,
			model.Runtime{ID: "rt_bruno", Kind: "bruno", Name: "Bruno", Binary: bruno, Source: "installed"},
			model.Runtime{ID: "rt_hopp", Kind: "hoppscotch", Name: "Hoppscotch", Binary: hopp, Source: "installed"},
			model.Runtime{ID: "rt_mybruno", Kind: "bruno", Name: "My Bruno", Binary: myBruno, Source: "registered"})
		d.Services = append(d.Services, model.Service{ID: "svc_pg", Name: "pg", Kind: "postgres", Port: 5432})
		return nil
	})
	type call struct {
		bin, dir  string
		args, env []string
	}
	var calls []call
	e.srv.Launch = func(bin string, args []string, dir string, env []string) error {
		calls = append(calls, call{bin, dir, args, env})
		return nil
	}
	last := func() call { return calls[len(calls)-1] }

	// Bruno: its data folder is a switch; a path or a service in the body is ignored.
	for _, body := range []map[string]string{nil, {"path": project}, {"serviceId": "svc_pg"}} {
		if r := e.do("POST", "/api/runtimes/rt_bruno/launch", body, nil); r.StatusCode != 204 {
			t.Fatalf("bruno %v: %d", body, r.StatusCode)
		}
		c := last()
		if c.bin != bruno || c.dir != filepath.Dir(bruno) || len(c.env) != 0 ||
			strings.Join(c.args, " ") != "--user-data-dir="+filepath.Join(e.home, "bruno") {
			t.Fatalf("bruno call %+v", c)
		}
	}
	if st, err := os.Stat(filepath.Join(e.home, "bruno")); err != nil || !st.IsDir() {
		t.Fatal("data bruno must exist")
	}

	// Hoppscotch: started in data\hoppscotch (the portable build keeps its data
	// in the working directory) with its WebView2 profile there too.
	if r := e.do("POST", "/api/runtimes/rt_hopp/launch", map[string]string{"path": project}, nil); r.StatusCode != 204 {
		t.Fatalf("hoppscotch: %d", r.StatusCode)
	}
	hd := filepath.Join(e.home, "hoppscotch")
	if c := last(); c.bin != hopp || c.dir != hd || len(c.args) != 0 ||
		strings.Join(c.env, " ") != "WEBVIEW2_USER_DATA_FOLDER="+filepath.Join(hd, "webview2") {
		t.Fatalf("hoppscotch call %+v", c)
	}
	if st, err := os.Stat(hd); err != nil || !st.IsDir() {
		t.Fatal("data hoppscotch must exist")
	}

	// Bruno registered by path is the user's own install: nothing is added.
	e.do("POST", "/api/runtimes/rt_mybruno/launch", nil, nil)
	if c := last(); len(c.args) != 0 || len(c.env) != 0 || c.dir != filepath.Dir(myBruno) {
		t.Fatalf("registered Bruno must be left alone: %+v", c)
	}
}
