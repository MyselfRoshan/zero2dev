package api

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"zero2dev/internal/catalog"
	"zero2dev/internal/config"
	"zero2dev/internal/events"
	"zero2dev/internal/installer"
	"zero2dev/internal/model"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

// Test binary doubles as a fake service ("idle" mode).
func TestMain(m *testing.M) {
	if os.Getenv("Z2D_HELPER") == "idle" {
		fmt.Println("hello from helper")
		select {}
	}
	os.Exit(m.Run())
}

const devOrigin = "http://localhost:5173"

type tenv struct {
	t     *testing.T
	ts    *httptest.Server
	token string
	home  string
	port  int
	srv   *Server
	hub   *events.Hub
	sup   *supervisor.Supervisor
}

func newEnv(t *testing.T) *tenv {
	t.Helper()
	home := t.TempDir()
	ts := httptest.NewUnstartedServer(nil)
	port := ts.Listener.Addr().(*net.TCPAddr).Port
	cfg := &config.Config{Home: home, Port: port, Token: strings.Repeat("ab", 32), DevOrigin: devOrigin, HomeSource: config.SourceExe, ExeDir: filepath.Join(home, "exe"), BinDir: filepath.Join(home, "bin")}
	st, err := store.Open(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	hub := events.New()
	sup := supervisor.New(home, st, hub)
	sup.KillGrace = time.Second
	rt := runtimes.New(home, st, hub)
	site := sites.New(home, st, hub, sup)
	rt.OnChange = func() { site.EnsureCaddy() }
	srv := New(Deps{
		Cfg: cfg, Store: st, Hub: hub, Sup: sup, Runtimes: rt, Installer: installer.New(home, st, rt, hub), Sites: site,
		Catalog: func() ([]model.CatalogEntry, error) { return catalog.Load(home) },
		UI: fstest.MapFS{
			"index.html":    {Data: []byte("<html>index</html>")},
			"assets/app.js": {Data: []byte("console.log(1)")},
			"assets/a.css":  {Data: []byte("body{}")},
		},
		Version: "test", Started: time.Now(),
	})
	ts.Config.Handler = srv
	ts.Start()
	t.Cleanup(func() { hub.Close(); sup.StopAll(t.Context()); ts.Close() })
	return &tenv{t: t, ts: ts, token: cfg.Token, home: home, port: port, srv: srv, hub: hub, sup: sup}
}

// do sends an authenticated request and decodes the JSON response into out.
func (e *tenv) do(method, path string, body any, out any) *http.Response {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("%s %s: bad JSON %q: %v", method, path, raw, err)
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp
}

func (e *tenv) errCode(method, path string, body any, wantStatus int) string {
	e.t.Helper()
	var er struct {
		Error struct{ Code, Message string }
	}
	resp := e.do(method, path, body, &er)
	if resp.StatusCode != wantStatus {
		e.t.Fatalf("%s %s: status %d, want %d (%+v)", method, path, resp.StatusCode, wantStatus, er)
	}
	if er.Error.Code == "" || er.Error.Message == "" {
		e.t.Fatalf("%s %s: error body malformed: %+v", method, path, er)
	}
	return er.Error.Code
}

func (e *tenv) raw(method, path string, hdr map[string]string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.ts.URL+path, nil)
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func errorCodeOf(body string) string {
	var er struct{ Error struct{ Code string } }
	json.Unmarshal([]byte(body), &er)
	return er.Error.Code
}

// ---------- middleware ----------

func TestAuthRequired(t *testing.T) {
	e := newEnv(t)
	for name, hdr := range map[string]map[string]string{
		"none":      {},
		"wrong":     {"Authorization": "Bearer nope"},
		"basic":     {"Authorization": "Basic " + e.token},
		"no-bearer": {"Authorization": e.token},
		"empty":     {"Authorization": "Bearer "},
	} {
		resp, body := e.raw("GET", "/api/status", hdr)
		if resp.StatusCode != 401 || errorCodeOf(body) != "unauthorized" {
			t.Errorf("%s: got %d %s", name, resp.StatusCode, body)
		}
		if resp.Header.Get("Content-Type") != "application/json" {
			t.Errorf("%s: content-type %q", name, resp.Header.Get("Content-Type"))
		}
	}
	resp, _ := e.raw("GET", "/api/status", map[string]string{"Authorization": "Bearer " + e.token})
	if resp.StatusCode != 200 {
		t.Fatalf("valid token: %d", resp.StatusCode)
	}
	// unknown API paths still require auth first, then are 404 JSON
	if resp, _ := e.raw("GET", "/api/nope", nil); resp.StatusCode != 401 {
		t.Fatal("unauth unknown path must be 401")
	}
	e.errCode("GET", "/api/nope", nil, 404)
}

func TestHostCheckBlocksDNSRebinding(t *testing.T) {
	e := newEnv(t)
	auth := map[string]string{"Authorization": "Bearer " + e.token}
	bad := []string{"evil.com", "evil.com:" + fmt.Sprint(e.port), "127.0.0.1.evil.com:" + fmt.Sprint(e.port),
		"127.0.0.1", "127.0.0.1:1", "localhost", "[::1]:" + fmt.Sprint(e.port), "0.0.0.0:" + fmt.Sprint(e.port)}
	for _, h := range bad {
		hdr := map[string]string{"Host": h, "Authorization": auth["Authorization"]}
		for _, p := range []string{"/api/status", "/", "/assets/app.js"} {
			resp, body := e.raw("GET", p, hdr)
			if resp.StatusCode != 403 || errorCodeOf(body) != "forbidden_host" {
				t.Errorf("Host %q %s: got %d %s", h, p, resp.StatusCode, body)
			}
		}
	}
	for _, h := range []string{fmt.Sprintf("127.0.0.1:%d", e.port), fmt.Sprintf("localhost:%d", e.port), fmt.Sprintf("LOCALHOST:%d", e.port)} {
		if resp, _ := e.raw("GET", "/api/status", map[string]string{"Host": h, "Authorization": auth["Authorization"]}); resp.StatusCode != 200 {
			t.Errorf("Host %q should be accepted: %d", h, resp.StatusCode)
		}
	}
}

func TestOriginCheckAndCORS(t *testing.T) {
	e := newEnv(t)
	auth := "Bearer " + e.token
	for _, o := range []string{"http://evil.com", "null", "https://127.0.0.1:" + fmt.Sprint(e.port), "http://127.0.0.1:1"} {
		resp, body := e.raw("GET", "/api/status", map[string]string{"Origin": o, "Authorization": auth})
		if resp.StatusCode != 403 || errorCodeOf(body) != "forbidden_origin" {
			t.Errorf("Origin %q: %d %s", o, resp.StatusCode, body)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("Origin %q must not get CORS headers", o)
		}
	}
	// same-origin: allowed, no CORS headers
	same := fmt.Sprintf("http://127.0.0.1:%d", e.port)
	resp, _ := e.raw("GET", "/api/status", map[string]string{"Origin": same, "Authorization": auth})
	if resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("same origin: %d acao=%q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	// dev origin: allowed + reflected
	resp, _ = e.raw("GET", "/api/status", map[string]string{"Origin": devOrigin, "Authorization": auth})
	if resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != devOrigin {
		t.Fatalf("dev origin: %d acao=%q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	// preflight from the dev origin needs no token
	resp, _ = e.raw("OPTIONS", "/api/services", map[string]string{"Origin": devOrigin, "Access-Control-Request-Method": "POST"})
	if resp.StatusCode != 204 || !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("preflight: %d %v", resp.StatusCode, resp.Header)
	}
	// preflight from a foreign origin is refused
	resp, _ = e.raw("OPTIONS", "/api/services", map[string]string{"Origin": "http://evil.com"})
	if resp.StatusCode != 403 {
		t.Fatalf("foreign preflight: %d", resp.StatusCode)
	}
}

func TestStaticServing(t *testing.T) {
	e := newEnv(t)
	cases := []struct{ path, ct, cache, body string }{
		{"/", "text/html; charset=utf-8", "no-store", "index"},
		{"/index.html", "text/html; charset=utf-8", "no-store", "index"},
		{"/some/spa/route", "text/html; charset=utf-8", "no-store", "index"},
		{"/assets/app.js", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable", "console"},
		{"/assets/a.css", "text/css; charset=utf-8", "public, max-age=31536000, immutable", "body"},
	}
	for _, c := range cases { // no token needed
		resp, body := e.raw("GET", c.path, nil)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != c.ct || resp.Header.Get("Cache-Control") != c.cache || !strings.Contains(body, c.body) {
			t.Errorf("%s: %d ct=%q cc=%q body=%q", c.path, resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"), body)
		}
	}
	if resp, _ := e.raw("GET", "/assets/missing.js", nil); resp.StatusCode != 404 {
		t.Fatalf("missing asset must 404, got %d", resp.StatusCode)
	}
	if resp, _ := e.raw("GET", "/../../etc/passwd", nil); resp.StatusCode == 200 {
		if _, body := e.raw("GET", "/../../etc/passwd", nil); strings.Contains(body, "root:") {
			t.Fatal("path traversal")
		}
	}
}

func TestStrictJSONAndErrors(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	good := map[string]any{"name": "a", "kind": "custom", "command": "x", "cwd": dir}
	if c := e.errCode("POST", "/api/services", `{"name":"a","bogus":1}`, 400); c != "bad_request" {
		t.Fatal(c)
	}
	e.errCode("POST", "/api/services", `not json`, 400)
	e.errCode("POST", "/api/services", ``, 400)
	e.errCode("POST", "/api/services", `{"name":"a"} {"x":1}`, 400)
	big := map[string]any{"name": "a", "kind": "custom", "command": strings.Repeat("x", 2<<20), "cwd": dir}
	e.errCode("POST", "/api/services", big, 400)
	_ = good
	e.errCode("GET", "/api/services/svc_missing", nil, 404)
	e.errCode("DELETE", "/api/services/svc_missing", nil, 404)
	e.errCode("POST", "/api/services/svc_missing/start", nil, 404)
	e.errCode("GET", "/api/jobs/job_missing", nil, 404)
}

// ---------- services ----------

func TestServiceCRUDAndValidation(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	invalid := []map[string]any{
		{"name": "", "kind": "custom", "command": "x", "cwd": dir},
		{"name": strings.Repeat("n", 65), "kind": "custom", "command": "x", "cwd": dir},
		{"name": "a", "kind": "ruby", "command": "x", "cwd": dir},
		{"name": "a", "kind": "custom", "cwd": dir},
		{"name": "a", "kind": "custom", "command": "x", "cwd": "relative/dir"},
		{"name": "a", "kind": "custom", "command": "x", "cwd": filepath.Join(dir, "nope")},
		{"name": "a", "kind": "custom", "command": "x", "cwd": dir, "port": 70000},
		{"name": "a", "kind": "custom", "command": "x", "cwd": dir, "healthPath": "health"},
		{"name": "a", "kind": "custom", "command": "x", "cwd": dir, "runtimeId": "rt_none"},
		{"name": "a", "kind": "custom", "command": "x", "cwd": dir, "env": map[string]string{"A=B": "1"}},
	}
	for i, in := range invalid {
		if c := e.errCode("POST", "/api/services", in, 400); c != "bad_request" {
			t.Errorf("case %d: code %s", i, c)
		}
	}
	var created model.Service
	resp := e.do("POST", "/api/services", map[string]any{"name": "Zeta", "kind": "custom", "command": "x", "cwd": dir + "/../" + filepath.Base(dir), "port": 3000}, &created)
	if resp.StatusCode != 201 || !strings.HasPrefix(created.ID, "svc_") || created.State != "stopped" || created.Cwd != dir || created.Args == nil || created.Env == nil {
		t.Fatalf("create: %d %+v", resp.StatusCode, created)
	}
	// JSON shape: arrays/objects never null, startedAt present as null
	raw := e.rawBody("GET", "/api/services/"+created.ID)
	for _, want := range []string{`"args":[]`, `"env":{}`, `"startedAt":null`, `"pid":0`, `"lastError":""`} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
	e.do("POST", "/api/services", map[string]any{"name": "alpha", "kind": "node", "cwd": dir}, nil)
	var list []model.Service
	e.do("GET", "/api/services", nil, &list)
	if len(list) != 2 || list[0].Name != "alpha" || list[1].Name != "Zeta" {
		t.Fatalf("sorted by name: %+v", list)
	}
	var upd model.Service
	resp = e.do("PUT", "/api/services/"+created.ID, map[string]any{"name": "Zeta2", "kind": "custom", "command": "y", "cwd": dir, "port": 3001}, &upd)
	if resp.StatusCode != 200 || upd.Name != "Zeta2" || upd.Port != 3001 || upd.ID != created.ID {
		t.Fatalf("update: %+v", upd)
	}
	e.errCode("PUT", "/api/services/svc_none", map[string]any{"name": "a", "kind": "custom", "command": "x", "cwd": dir}, 404)
	if resp := e.do("DELETE", "/api/services/"+created.ID, nil, nil); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	e.errCode("GET", "/api/services/"+created.ID, nil, 404)
	var st struct {
		Counts map[string]int
		OS     string
	}
	e.do("GET", "/api/status", nil, &st)
	if st.Counts["services"] != 1 || st.OS == "" {
		t.Fatalf("status %+v", st)
	}
}

func (e *tenv) rawBody(method, path string) string {
	resp := e.do(method, path, nil, nil)
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func helperService(t *testing.T, e *tenv, port int) model.Service {
	var sv model.Service
	e.do("POST", "/api/services", map[string]any{
		"name": "helper", "kind": "custom", "command": os.Args[0], "args": []string{"-test.run=^$"},
		"cwd": t.TempDir(), "env": map[string]string{"Z2D_HELPER": "idle"},
	}, &sv)
	if sv.ID == "" {
		t.Fatal("create helper failed")
	}
	return sv
}

func TestServiceLifecycleOverAPI(t *testing.T) {
	e := newEnv(t)
	sv := helperService(t, e, 0)
	var started model.Service
	resp := e.do("POST", "/api/services/"+sv.ID+"/start", nil, &started)
	if resp.StatusCode != 202 || started.State != "starting" || started.Pid == 0 {
		t.Fatalf("start: %d %+v", resp.StatusCode, started)
	}
	e.errCode("POST", "/api/services/"+sv.ID+"/start", nil, 409)
	e.errCode("PUT", "/api/services/"+sv.ID, map[string]any{"name": "x", "kind": "custom", "command": "x", "cwd": t.TempDir()}, 409)
	waitFor(t, func() bool {
		var cur model.Service
		e.do("GET", "/api/services/"+sv.ID, nil, &cur)
		return cur.State == "running" && cur.StartedAt != nil
	})
	var logs struct{ Lines []model.LogLine }
	waitFor(t, func() bool {
		e.do("GET", "/api/services/"+sv.ID+"/logs?tail=10", nil, &logs)
		for _, l := range logs.Lines {
			if l.Stream == "stdout" && l.Text == "hello from helper" {
				return true
			}
		}
		return false
	})
	e.errCode("GET", "/api/services/"+sv.ID+"/logs?tail=abc", nil, 400)
	var all struct{ Lines []model.LogLine }
	e.do("GET", "/api/services/"+sv.ID+"/logs?tail=1", nil, &all)
	if len(all.Lines) != 1 {
		t.Fatalf("tail=1 returned %d", len(all.Lines))
	}
	var restarted model.Service
	if resp := e.do("POST", "/api/services/"+sv.ID+"/restart", nil, &restarted); resp.StatusCode != 202 || restarted.Pid == 0 || restarted.Pid == started.Pid {
		t.Fatalf("restart: %d %+v", resp.StatusCode, restarted)
	}
	var stopped model.Service
	if resp := e.do("POST", "/api/services/"+sv.ID+"/stop", nil, &stopped); resp.StatusCode != 202 || (stopped.State != "stopping" && stopped.State != "stopped") {
		t.Fatalf("stop: %d %+v", resp.StatusCode, stopped)
	}
	waitFor(t, func() bool {
		var cur model.Service
		e.do("GET", "/api/services/"+sv.ID, nil, &cur)
		return cur.State == "stopped"
	})
	// stop on a stopped service is a no-op 202
	if resp := e.do("POST", "/api/services/"+sv.ID+"/stop", nil, nil); resp.StatusCode != 202 {
		t.Fatalf("idempotent stop: %d", resp.StatusCode)
	}
	if resp := e.do("DELETE", "/api/services/"+sv.ID, nil, nil); resp.StatusCode != 204 {
		t.Fatal("delete")
	}
}

func TestStartPortConflictIs409(t *testing.T) {
	e := newEnv(t)
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port
	var sv model.Service
	e.do("POST", "/api/services", map[string]any{"name": "p", "kind": "custom", "command": os.Args[0], "cwd": t.TempDir(), "port": port}, &sv)
	var er struct {
		Error struct{ Code, Message string }
	}
	resp := e.do("POST", "/api/services/"+sv.ID+"/start", nil, &er)
	if resp.StatusCode != 409 || er.Error.Code != "conflict" || !strings.Contains(er.Error.Message, fmt.Sprint(port)) {
		t.Fatalf("%d %+v", resp.StatusCode, er)
	}
}

func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// ---------- runtimes / catalog ----------

func TestRuntimesAPI(t *testing.T) {
	e := newEnv(t)
	e.errCode("POST", "/api/runtimes/register", map[string]any{"kind": "node", "binary": "relative"}, 400)
	e.errCode("POST", "/api/runtimes/register", map[string]any{"kind": "node", "binary": filepath.Join(t.TempDir(), "nope")}, 400)
	e.errCode("POST", "/api/runtimes/register", map[string]any{"kind": "rust", "binary": os.Args[0]}, 400)
	var rt model.Runtime
	resp := e.do("POST", "/api/runtimes/register", map[string]any{"kind": "node", "binary": os.Args[0], "version": "9.9.9"}, &rt)
	if resp.StatusCode != 201 || rt.Source != "registered" || rt.Version != "9.9.9" || rt.Name != "Node.js 9.9.9" || !strings.HasPrefix(rt.ID, "rt_") {
		t.Fatalf("%d %+v", resp.StatusCode, rt)
	}
	e.errCode("POST", "/api/runtimes/register", map[string]any{"kind": "node", "binary": os.Args[0]}, 409)
	var list []model.Runtime
	e.do("GET", "/api/runtimes", nil, &list)
	if len(list) != 1 {
		t.Fatalf("list %+v", list)
	}
	// referenced runtime cannot be deleted
	var sv model.Service
	e.do("POST", "/api/services", map[string]any{"name": "n", "kind": "node", "args": []string{"x"}, "cwd": t.TempDir(), "runtimeId": rt.ID}, &sv)
	e.errCode("DELETE", "/api/runtimes/"+rt.ID, nil, 409)
	e.do("DELETE", "/api/services/"+sv.ID, nil, nil)
	if resp := e.do("DELETE", "/api/runtimes/"+rt.ID, nil, nil); resp.StatusCode != 204 {
		t.Fatalf("delete %d", resp.StatusCode)
	}
	// the registered file itself must not have been touched
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Fatal("registered binary removed!")
	}
	e.errCode("DELETE", "/api/runtimes/"+rt.ID, nil, 404)

	var det []map[string]any
	e.do("GET", "/api/runtimes/detect", nil, &det)
	if det == nil {
		t.Fatal("detect must return an array")
	}
	e.errCode("POST", "/api/runtimes/install", map[string]any{"catalogId": "nope"}, 404)
}

func TestRegisteringCaddyCreatesService(t *testing.T) {
	e := newEnv(t)
	var rt model.Runtime
	e.do("POST", "/api/runtimes/register", map[string]any{"kind": "caddy", "binary": os.Args[0], "version": "2.8.4"}, &rt)
	var list []model.Service
	e.do("GET", "/api/services", nil, &list)
	if len(list) != 1 || list[0].Name != "Caddy" || list[0].Kind != "caddy" || list[0].Port != 0 {
		t.Fatalf("auto service: %+v", list)
	}
}

func TestCatalogAPI(t *testing.T) {
	e := newEnv(t)
	var es []model.CatalogEntry
	e.do("GET", "/api/catalog", nil, &es)
	if len(es) == 0 {
		t.Fatal("empty catalog")
	}
	supported := 0
	for _, c := range es {
		if c.Supported {
			supported++
		}
		if c.Installed {
			t.Fatal("nothing installed yet")
		}
	}
	if supported == 0 {
		t.Fatal("expected some supported entries on linux/amd64 or windows/amd64")
	}
}

// ---------- sites + settings ----------

func TestSitesAndSettingsAPI(t *testing.T) {
	e := newEnv(t)
	root := t.TempDir()
	var site model.SiteView
	resp := e.do("POST", "/api/sites", map[string]any{"name": "Docs", "domain": "docs.localhost", "type": "static", "root": root}, &site)
	if resp.StatusCode != 201 || !site.Enabled || site.URL != "http://docs.localhost:8080" {
		t.Fatalf("%d %+v", resp.StatusCode, site)
	}
	e.errCode("POST", "/api/sites", map[string]any{"name": "Dup", "domain": "docs.localhost", "type": "proxy", "upstreamPort": 3000}, 409)
	e.errCode("POST", "/api/sites", map[string]any{"name": "Bad", "domain": "evil.com", "type": "proxy", "upstreamPort": 3000}, 400)
	e.errCode("POST", "/api/sites", map[string]any{"name": "X", "domain": "x.localhost", "type": "static", "root": root, "extra": 1}, 400)
	cf, _ := os.ReadFile(filepath.Join(e.home, "config", "Caddyfile"))
	if !strings.Contains(string(cf), "http://docs.localhost:8080 {") || !strings.Contains(string(cf), "root * "+root) {
		t.Fatalf("Caddyfile:\n%s", cf)
	}
	var up model.SiteView
	e.do("PUT", "/api/sites/"+site.ID, map[string]any{"name": "Docs", "domain": "docs.localhost", "type": "static", "root": root, "https": true}, &up)
	if !up.HTTPS || up.URL != "https://docs.localhost:8443" {
		t.Fatalf("%+v", up)
	}

	var set model.Settings
	e.do("GET", "/api/settings", nil, &set)
	if set.APIPort != e.port || set.HomeDir != e.home || set.ProxyHTTPPort != 8080 || !set.OpenBrowserOnStart {
		t.Fatalf("%+v", set)
	}
	// echo back whole object with changes; apiPort/homeDir ignored
	resp = e.do("PUT", "/api/settings", map[string]any{"apiPort": 1, "homeDir": "/x", "domainSuffix": "localhost", "proxyHttpPort": 9080, "proxyHttpsPort": 9443, "openBrowserOnStart": false}, &set)
	if resp.StatusCode != 200 || set.ProxyHTTPPort != 9080 || set.APIPort != e.port || set.HomeDir != e.home || set.OpenBrowserOnStart {
		t.Fatalf("%d %+v", resp.StatusCode, set)
	}
	cf, _ = os.ReadFile(filepath.Join(e.home, "config", "Caddyfile"))
	if !strings.Contains(string(cf), "http_port 9080") || !strings.Contains(string(cf), "https_port 9443") {
		t.Fatal("Caddyfile not regenerated for new ports")
	}
	e.errCode("PUT", "/api/settings", map[string]any{"proxyHttpPort": 0}, 400)
	e.errCode("PUT", "/api/settings", map[string]any{"proxyHttpPort": 9443}, 400)
	e.errCode("PUT", "/api/settings", map[string]any{"domainSuffix": "bad suffix!"}, 400)

	if resp := e.do("DELETE", "/api/sites/"+site.ID, nil, nil); resp.StatusCode != 204 {
		t.Fatal("delete site")
	}
	e.errCode("DELETE", "/api/sites/"+site.ID, nil, 404)
	e.errCode("PUT", "/api/sites/"+site.ID, map[string]any{"name": "a", "domain": "a.localhost", "type": "proxy", "upstreamPort": 1}, 404)
}

// ---------- events ----------

func TestEventStreamTicketFlow(t *testing.T) {
	e := newEnv(t)
	var tk struct{ Ticket string }
	if resp := e.do("POST", "/api/events/ticket", nil, &tk); resp.StatusCode != 200 || tk.Ticket == "" {
		t.Fatal("no ticket")
	}
	// without ticket / with bogus ticket: 401, regardless of bearer token
	e.errCode("GET", "/api/events", nil, 401)
	e.errCode("GET", "/api/events?ticket=bogus", nil, 401)
	// ticket endpoint itself needs the bearer token
	if resp, _ := e.raw("POST", "/api/events/ticket", nil); resp.StatusCode != 401 {
		t.Fatal("ticket endpoint must require auth")
	}

	req, _ := http.NewRequest("GET", e.ts.URL+"/api/events?ticket="+tk.Ticket, nil) // no Authorization header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream: %d %v", resp.StatusCode, resp.Header)
	}
	// ticket is single use
	e.errCode("GET", "/api/events?ticket="+tk.Ticket, nil, 401)

	lines := make(chan string, 100)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	waitFor(t, func() bool { return e.hub.SubscriberCount() == 1 })

	// drive real events: create + start a service
	sv := helperService(t, e, 0)
	e.do("POST", "/api/services/"+sv.ID+"/start", nil, nil)
	seen := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for !(seen["services.changed"] && seen["service.status"] && seen["log.line"] && seen["status-has-id"]) {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed early, seen %v", seen)
			}
			if strings.HasPrefix(l, "event: ") {
				seen[strings.TrimPrefix(l, "event: ")] = true
			}
			if strings.HasPrefix(l, "data: ") && strings.Contains(l, sv.ID) && strings.Contains(l, "state") {
				seen["status-has-id"] = true
			}
		case <-deadline:
			t.Fatalf("timed out, seen %v", seen)
		}
	}
}

func TestStatusPortableFields(t *testing.T) {
	e := newEnv(t)
	var st map[string]any
	e.do("GET", "/api/status", nil, &st)
	if st["homeSource"] != "exe" || st["portable"] != true || st["exeDir"] != filepath.Join(e.home, "exe") || st["homeDir"] != e.home || st["version"] != "test" {
		t.Fatalf("%v", st)
	}
	e.srv.Cfg.HomeSource = config.SourceEnv
	e.do("GET", "/api/status", nil, &st)
	if st["homeSource"] != "env" || st["portable"] != false {
		t.Fatalf("%v", st)
	}
}

func TestSettingsTrustAndProxy(t *testing.T) {
	e := newEnv(t)
	var set model.Settings
	e.do("GET", "/api/settings", nil, &set)
	if set.TrustLocalCA || set.DownloadProxy != "" {
		t.Fatalf("defaults: %+v", set)
	}
	cf := func() string { b, _ := os.ReadFile(filepath.Join(e.home, "config", "Caddyfile")); return string(b) }
	e.do("PUT", "/api/settings", map[string]any{"trustLocalCA": true}, &set)
	if !set.TrustLocalCA || set.ProxyHTTPPort != 8080 || strings.Contains(cf(), "skip_install_trust") || !strings.Contains(cf(), "admin off") {
		t.Fatalf("%+v\n%s", set, cf())
	}
	e.do("PUT", "/api/settings", map[string]any{"trustLocalCA": false}, &set)
	if !strings.Contains(cf(), "skip_install_trust") {
		t.Fatal("skip_install_trust must return")
	}
	e.do("PUT", "/api/settings", map[string]any{"downloadProxy": " http://proxy.corp:3128 "}, &set)
	if set.DownloadProxy != "http://proxy.corp:3128" || set.TrustLocalCA {
		t.Fatalf("partial PUT must keep other fields: %+v", set)
	}
	for _, bad := range []string{"proxy.corp:3128", "ftp://x:1", "http://", "http://x:70000/p"} {
		e.errCode("PUT", "/api/settings", map[string]any{"downloadProxy": bad}, 400)
	}
	e.do("PUT", "/api/settings", map[string]any{"downloadProxy": ""}, &set)
	if set.DownloadProxy != "" {
		t.Fatal("clearing the proxy")
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, b := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(b))
	}
	zw.Close()
	return buf.Bytes()
}

func TestImportAndInstallerLogAPI(t *testing.T) {
	e := newEnv(t)
	z := zipBytes(t, map[string]string{"caddy": "BIN"})
	h := sha256.Sum256(z)
	sum := hex.EncodeToString(h[:])
	entry := fmt.Sprintf(`[{"id":"caddy-t","kind":"caddy","version":"2.8.4","name":"Caddy 2.8.4","url":"https://example.invalid/dl/caddy_t.zip",
	 "checksum":{"type":"shasums","url":"https://example.invalid/sums.txt"},"binaryRelPath":"caddy","stripComponents":0,"os":%q,"arch":%q,"homepage":"https://example.invalid/"}]`, runtime.GOOS, runtime.GOARCH)
	os.WriteFile(filepath.Join(e.home, "catalog.json"), []byte(entry), 0o644)

	var es []model.CatalogEntry
	e.do("GET", "/api/catalog", nil, &es)
	if len(es) != 1 || es[0].FileName != "caddy_t.zip" || es[0].Homepage != "https://example.invalid/" || !es[0].Supported {
		t.Fatalf("catalog fields: %+v", es)
	}
	file := filepath.Join(t.TempDir(), "caddy_t.zip")
	os.WriteFile(file, z, 0o644)

	e.errCode("POST", "/api/runtimes/import", map[string]any{"catalogId": "caddy-t", "filePath": "rel.zip"}, 400)
	e.errCode("POST", "/api/runtimes/import", map[string]any{"catalogId": "nope", "filePath": file}, 404)
	e.errCode("POST", "/api/runtimes/import", map[string]any{"catalogId": "caddy-t", "filePath": file, "sha256": "zz"}, 400)
	e.errCode("POST", "/api/runtimes/import", map[string]any{"catalogId": "caddy-t", "filePath": file, "bogus": 1}, 400)

	var job model.Job
	resp := e.do("POST", "/api/runtimes/import", map[string]any{"catalogId": "caddy-t", "filePath": file, "sha256": sum}, &job)
	if resp.StatusCode != 202 || job.Type != "import" || !strings.HasPrefix(job.ID, "job_") {
		t.Fatalf("%d %+v", resp.StatusCode, job)
	}
	waitFor(t, func() bool { e.do("GET", "/api/jobs/"+job.ID, nil, &job); return job.State != "running" })
	if job.State != "done" || job.RuntimeID == "" {
		t.Fatalf("%+v", job)
	}
	e.errCode("POST", "/api/runtimes/import", map[string]any{"catalogId": "caddy-t", "filePath": file, "sha256": sum}, 409)
	// catalog shows it installed; a caddy service appeared
	e.do("GET", "/api/catalog", nil, &es)
	if !es[0].Installed {
		t.Fatal("installed flag")
	}
	var svcs []model.Service
	e.do("GET", "/api/services", nil, &svcs)
	if len(svcs) != 1 || svcs[0].Kind != "caddy" {
		t.Fatalf("%+v", svcs)
	}

	var lg struct{ Lines []string }
	e.do("GET", "/api/installer/log?tail=200", nil, &lg)
	joined := strings.Join(lg.Lines, "\n")
	if lg.Lines == nil || !strings.Contains(joined, "import caddy-t") || !strings.Contains(joined, "expected="+sum) || !strings.Contains(joined, "done: runtime") {
		t.Fatalf("installer log:\n%s", joined)
	}
	e.do("GET", "/api/installer/log?tail=1", nil, &lg)
	if len(lg.Lines) != 1 {
		t.Fatalf("tail=1: %v", lg.Lines)
	}
	e.errCode("GET", "/api/installer/log?tail=x", nil, 400)
	if resp, _ := e.raw("GET", "/api/installer/log", nil); resp.StatusCode != 401 {
		t.Fatal("installer log needs auth")
	}
}
