package api

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestKindsEndpoint(t *testing.T) {
	e := newEnv(t)
	var profiles []struct {
		Kind    string `json:"kind"`
		Options []struct {
			Key string `json:"key"`
		} `json:"options"`
	}
	if resp := e.do("GET", "/api/kinds", nil, &profiles); resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	got := []string{}
	for _, p := range profiles {
		got = append(got, p.Kind)
	}
	for _, k := range model.ServiceKinds {
		if !slices.Contains(got, k) {
			t.Errorf("kind %s missing from /api/kinds", k)
		}
	}
}

func TestServiceSettingsValidationAndFolder(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	// A database needs no project folder.
	var db model.Service
	resp := e.do("POST", "/api/services", map[string]any{"name": "DB", "kind": "mariadb", "port": 3306, "settings": map[string]string{"maxConnections": "50"}}, &db)
	if resp.StatusCode != 201 || db.Cwd != "" || db.Settings["maxConnections"] != "50" || db.CustomArgs {
		t.Fatalf("mariadb without folder: %d %+v", resp.StatusCode, db)
	}
	// Bad settings are refused with the setting's label.
	var er struct {
		Error struct{ Code, Message string }
	}
	resp = e.do("POST", "/api/services", map[string]any{"name": "DB", "kind": "mariadb", "port": 3306, "settings": map[string]string{"maxConnections": "0"}}, &er)
	if resp.StatusCode != 400 || !strings.Contains(er.Error.Message, "Max connections") {
		t.Fatalf("bad setting: %d %+v", resp.StatusCode, er)
	}
	if c := e.errCode("POST", "/api/services", map[string]any{"name": "x", "kind": "mariadb", "port": 1, "settings": map[string]string{"bogus": "1"}}, 400); c != "bad_request" {
		t.Fatal(c)
	}
	// A Node service still needs its folder, and its script.
	if c := e.errCode("POST", "/api/services", map[string]any{"name": "n", "kind": "node", "settings": map[string]string{"script": "a.js"}}, 400); c != "bad_request" {
		t.Fatal(c)
	}
	e.do("POST", "/api/services", map[string]any{"name": "n", "kind": "node", "cwd": dir, "settings": map[string]string{}}, &er)
	if !strings.Contains(er.Error.Message, "Script is required") {
		t.Fatalf("node without script: %+v", er)
	}
	// An older client sends args and no settings: its args are the whole command line.
	var legacy model.Service
	e.do("POST", "/api/services", map[string]any{"name": "old", "kind": "node", "cwd": dir, "args": []string{"server.js"}}, &legacy)
	if !legacy.CustomArgs || legacy.Args[0] != "server.js" {
		t.Fatalf("legacy client: %+v", legacy)
	}
	// Guided settings with extra args.
	var guided model.Service
	e.do("POST", "/api/services", map[string]any{"name": "new", "kind": "node", "cwd": dir, "args": []string{"--x"}, "settings": map[string]string{"script": "a.js"}}, &guided)
	if guided.CustomArgs || guided.Settings["script"] != "a.js" {
		t.Fatalf("guided: %+v", guided)
	}
}

func TestPreviewAndCommand(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	var pv struct {
		Program  string   `json:"program"`
		Args     []string `json:"args"`
		Env      []string `json:"env"`
		Workdir  string   `json:"workdir"`
		DataDir  string   `json:"dataDir"`
		Files    []string `json:"files"`
		Warnings []string `json:"warnings"`
	}
	resp := e.do("POST", "/api/services/preview", map[string]any{"name": "p", "kind": "php", "cwd": dir, "port": 8000,
		"settings": map[string]string{"memoryLimit": "512M", "docroot": "public"}}, &pv)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	ini := filepath.Join(e.home, "php", "svc_new", "php.ini")
	if !slices.Contains(pv.Args, ini) || !slices.Contains(pv.Args, "127.0.0.1:8000") || !slices.Contains(pv.Args, filepath.Join(dir, "public")) {
		t.Fatalf("php preview args %q", pv.Args)
	}
	if len(pv.Files) != 1 || pv.Files[0] != ini || pv.DataDir != filepath.Join(e.home, "php", "svc_new") {
		t.Fatalf("files %q dataDir %q", pv.Files, pv.DataDir)
	}
	if len(pv.Warnings) == 0 { // no PHP installed in the test
		t.Fatal("want a warning about the missing runtime")
	}
	if _, err := os.Stat(filepath.Join(e.home, "php")); err == nil {
		t.Fatal("preview must not create anything")
	}
	if !slices.Contains(pv.Env, "PORT=8000") {
		t.Fatalf("env %q", pv.Env)
	}

	// Database: the workdir is its data folder, which also shows in the args.
	e.do("POST", "/api/services/preview", map[string]any{"name": "d", "kind": "redis", "port": 6379, "settings": map[string]string{"persistence": "none"}}, &pv)
	data := filepath.Join(e.home, "redis", "svc_new")
	if pv.Workdir != data || !slices.Contains(pv.Args, filepath.ToSlash(data)) || !slices.Contains(pv.Args, "--appendonly") {
		t.Fatalf("redis preview %+v", pv)
	}
	if c := e.errCode("POST", "/api/services/preview", map[string]any{"kind": "ruby"}, 400); c != "bad_request" {
		t.Fatal(c)
	}

	// A saved service.
	var svc model.Service
	e.do("POST", "/api/services", map[string]any{"name": "pg", "kind": "postgres", "port": 5432, "settings": map[string]string{"logStatement": "all"}}, &svc)
	e.do("GET", "/api/services/"+svc.ID+"/command", nil, &pv)
	if !slices.Contains(pv.Args, "log_statement=all") || !slices.Contains(pv.Args, filepath.Join(e.home, "postgres", svc.ID)) {
		t.Fatalf("command %q", pv.Args)
	}
	if c := e.errCode("GET", "/api/services/svc_nope/command", nil, 404); c != "not_found" {
		t.Fatal(c)
	}
}

func TestProjectScripts(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node .","dev":"vite","build":"vite build"}}`), 0o644)
	var out struct{ Scripts []string }
	e.do("GET", "/api/project/scripts?dir="+dir, nil, &out)
	if !slices.Equal(out.Scripts, []string{"build", "dev", "start"}) {
		t.Fatalf("scripts %q", out.Scripts)
	}
	e.do("GET", "/api/project/scripts?dir="+t.TempDir(), nil, &out)
	if out.Scripts == nil || len(out.Scripts) != 0 {
		t.Fatalf("no package.json: %q", out.Scripts)
	}
	if c := e.errCode("GET", "/api/project/scripts?dir=rel", nil, 400); c != "bad_request" {
		t.Fatal(c)
	}
}

func addPHP(t *testing.T, e *tenv, version string, exts ...string) model.Runtime {
	t.Helper()
	dir := filepath.Join(e.home, "bin", "php-"+version)
	os.MkdirAll(filepath.Join(dir, "ext"), 0o755)
	os.WriteFile(filepath.Join(dir, "php.exe"), nil, 0o755)
	for _, x := range exts {
		os.WriteFile(filepath.Join(dir, "ext", x), nil, 0o644)
	}
	rt := model.Runtime{ID: "rt_php" + strings.ReplaceAll(version, ".", ""), Kind: "php", Version: version, Name: "PHP " + version,
		Binary: filepath.Join(dir, "php.exe"), Source: "installed"}
	e.srv.Store.Update(func(d *store.Data) error { d.Runtimes = append(d.Runtimes, rt); return nil })
	return rt
}

func TestPHPExtensionsAndXdebug(t *testing.T) {
	e := newEnv(t)
	rt := addPHP(t, e, "8.4.13", "php_curl.dll", "php_mbstring.dll", "php_opcache.dll")
	var out struct {
		Extensions []struct {
			Name string
			Zend bool
		}
		Xdebug  bool
		Managed bool
	}
	e.do("GET", "/api/runtimes/"+rt.ID+"/php-extensions", nil, &out)
	if len(out.Extensions) != 3 || out.Extensions[0].Name != "curl" || !out.Extensions[2].Zend || out.Xdebug || !out.Managed {
		t.Fatalf("extensions %+v", out)
	}

	dll := []byte("MZ fake xdebug")
	sum := fmt.Sprintf("%x", sha256sum(dll))
	var page string
	pub := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download":
			fmt.Fprint(w, page)
		case "/files/php_xdebug-3.5.3-8.4-nts-vs17-x86_64.dll":
			w.Write(dll)
		default:
			http.NotFound(w, r)
		}
	}))
	defer pub.Close()
	page = `<a title="SHA256:&nbsp;` + sum + `" href='/files/php_xdebug-3.5.3-8.4-nts-vs17-x86_64.dll'>8.4</a>`
	e.srv.Installer.XdebugPage = pub.URL + "/download"
	e.srv.Installer.Client = pub.Client()

	var j model.Job
	if resp := e.do("POST", "/api/runtimes/"+rt.ID+"/xdebug", nil, &j); resp.StatusCode != 202 || j.Type != "xdebug" {
		t.Fatalf("start: %d %+v", resp.StatusCode, j)
	}
	j = waitJob(t, e, j.ID)
	if j.State != "done" || j.RuntimeID != rt.ID {
		t.Fatalf("job %+v", j)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(rt.Binary), "ext", "php_xdebug.dll")); string(b) != string(dll) {
		t.Fatal("xdebug not installed into ext")
	}
	e.do("GET", "/api/runtimes/"+rt.ID+"/php-extensions", nil, &out)
	if !out.Xdebug {
		t.Fatal("xdebug not listed after install")
	}

	// A wrong checksum installs nothing.
	rt2 := addPHP(t, e, "8.4.1")
	page = `<a title="SHA256:&nbsp;` + strings.Repeat("0", 64) + `" href='/files/php_xdebug-3.5.3-8.4-nts-vs17-x86_64.dll'>8.4</a>`
	e.do("POST", "/api/runtimes/"+rt2.ID+"/xdebug", nil, &j)
	if j = waitJob(t, e, j.ID); j.State != "failed" || !strings.Contains(j.Error, "checksum mismatch") {
		t.Fatalf("mismatch job %+v", j)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(rt2.Binary), "ext", "php_xdebug.dll")); err == nil {
		t.Fatal("a mismatching file must not be installed")
	}

	old := addPHP(t, e, "7.4.33")
	if c := e.errCode("POST", "/api/runtimes/"+old.ID+"/xdebug", nil, 400); c != "bad_request" {
		t.Fatal(c)
	}
	if c := e.errCode("GET", "/api/runtimes/rt_nope/php-extensions", nil, 404); c != "not_found" {
		t.Fatal(c)
	}
}

func sha256sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func TestShutdownEndpoint(t *testing.T) {
	e := newEnv(t)
	if c := e.errCode("POST", "/api/shutdown", nil, 501); c != "not_supported" {
		t.Fatal(c)
	}
	quit := make(chan struct{})
	e.srv.Quit = func() { close(quit) }
	if resp := e.do("POST", "/api/shutdown", nil, nil); resp.StatusCode != 202 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	select {
	case <-quit:
	case <-time.After(3 * time.Second):
		t.Fatal("Quit was not called")
	}
	// Without the token the endpoint is closed like every other.
	resp, _ := e.raw("POST", "/api/shutdown", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
}
