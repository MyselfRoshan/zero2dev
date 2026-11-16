package api

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/envreg"
	"zero2dev/internal/launch"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func svcBody(name, dir string, extra map[string]any) map[string]any {
	m := map[string]any{"name": name, "kind": "custom", "command": "x", "cwd": dir}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func (e *tenv) newService(t *testing.T, name string, extra map[string]any) model.Service {
	t.Helper()
	var sv model.Service
	if resp := e.do("POST", "/api/services", svcBody(name, t.TempDir(), extra), &sv); resp.StatusCode != 201 {
		t.Fatalf("create %s: %d", name, resp.StatusCode)
	}
	return sv
}

func TestServiceLinkFieldsValidation(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	web := e.newService(t, "web", nil)
	if web.DependsOn == nil || len(web.DependsOn) != 0 || web.RestartLimit != model.DefaultRestartLimit || web.RestartOnCrash || web.Restarts != 0 || web.EnvFile != "" {
		t.Fatalf("defaults: %+v", web)
	}
	raw := e.rawBody("GET", "/api/services/"+web.ID)
	for _, want := range []string{`"dependsOn":[]`, `"databaseId":""`, `"envFile":""`, `"restartOnCrash":false`, `"restartLimit":5`, `"restarts":0`} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
	var pg model.Service
	if resp := e.do("POST", "/api/services", map[string]any{"name": "pg", "kind": "postgres"}, &pg); resp.StatusCode != 201 {
		t.Fatalf("create postgres: %d", resp.StatusCode)
	}

	invalid := map[string]map[string]any{
		"unknown dependency":      {"dependsOn": []string{"svc_nope"}},
		"unknown database":        {"databaseId": "svc_nope"},
		"database not a database": {"databaseId": web.ID},
		"restartLimit too big":    {"restartLimit": 21},
		"restartLimit negative":   {"restartLimit": -1},
		"envFile absolute":        {"envFile": filepath.Join(dir, ".env")},
		"envFile rooted":          {"envFile": "/etc/passwd"},
		"envFile parent":          {"envFile": "../.env"},
		"envFile nested parent":   {"envFile": "a/../../.env"},
		"envFile backslash up":    {"envFile": `a\..\..\.env`},
		"envFile drive":           {"envFile": `C:.env`},
	}
	for name, extra := range invalid {
		if c := e.errCode("POST", "/api/services", svcBody("bad", dir, extra), 400); c != "bad_request" {
			t.Errorf("%s: code %s", name, c)
		}
	}

	var ok model.Service
	resp := e.do("POST", "/api/services", svcBody("api", dir, map[string]any{
		"dependsOn": []string{web.ID, web.ID}, "databaseId": pg.ID, "envFile": "config/./.env",
		"restartOnCrash": true, "restartLimit": 20,
	}), &ok)
	if resp.StatusCode != 201 || len(ok.DependsOn) != 1 || ok.DependsOn[0] != web.ID || ok.DatabaseID != pg.ID ||
		ok.EnvFile != filepath.Clean("config/.env") || !ok.RestartOnCrash || ok.RestartLimit != 20 {
		t.Fatalf("created: %d %+v", resp.StatusCode, ok)
	}
	// every field survives a round trip through GET and PUT
	var got model.Service
	e.do("GET", "/api/services/"+ok.ID, nil, &got)
	if got.DatabaseID != pg.ID || got.RestartLimit != 20 {
		t.Fatalf("GET: %+v", got)
	}
	resp = e.do("PUT", "/api/services/"+ok.ID, svcBody("api", dir, map[string]any{"dependsOn": []string{}, "restartLimit": 3}), &got)
	if resp.StatusCode != 200 || len(got.DependsOn) != 0 || got.DatabaseID != "" || got.RestartLimit != 3 || got.RestartOnCrash {
		t.Fatalf("PUT: %d %+v", resp.StatusCode, got)
	}
}

func TestServiceDependencyCycles(t *testing.T) {
	e := newEnv(t)
	a := e.newService(t, "Alpha", nil)
	b := e.newService(t, "Beta", map[string]any{"dependsOn": []string{a.ID}})
	dir := t.TempDir()

	// self dependency, direct loop, and a loop through a third service
	e.errCode("PUT", "/api/services/"+a.ID, svcBody("Alpha", dir, map[string]any{"dependsOn": []string{a.ID}}), 400)
	var er struct{ Error struct{ Message string } }
	if resp := e.do("PUT", "/api/services/"+a.ID, svcBody("Alpha", dir, map[string]any{"dependsOn": []string{b.ID}}), &er); resp.StatusCode != 400 {
		t.Fatalf("loop accepted: %d", resp.StatusCode)
	}
	if m := er.Error.Message; !strings.Contains(m, "Alpha") || !strings.Contains(m, "Beta") || !strings.Contains(m, "->") {
		t.Fatalf("message must name the loop: %q", m)
	}
	c := e.newService(t, "Gamma", map[string]any{"dependsOn": []string{b.ID}})
	e.errCode("PUT", "/api/services/"+a.ID, svcBody("Alpha", dir, map[string]any{"dependsOn": []string{c.ID}}), 400)

	// the database link counts as a dependency too
	var pg model.Service
	e.do("POST", "/api/services", map[string]any{"name": "pg", "kind": "postgres", "dependsOn": []string{}}, &pg)
	app := e.newService(t, "app", map[string]any{"databaseId": pg.ID})
	e.errCode("PUT", "/api/services/"+pg.ID, map[string]any{"name": "pg", "kind": "postgres", "dependsOn": []string{app.ID}}, 400)
	e.errCode("PUT", "/api/services/"+app.ID, svcBody("app", dir, map[string]any{"databaseId": app.ID}), 400)

	// a diamond is not a loop
	e.newService(t, "Delta", map[string]any{"dependsOn": []string{b.ID, c.ID}})
}

func TestStartAllAndProjectsOverAPI(t *testing.T) {
	e := newEnv(t)
	a, b := helperService(t, e, 0), helperService(t, e, 0)

	// unknown project: 404 on every bulk route
	for _, p := range []string{"/api/services/start-all", "/api/services/stop-all", "/api/sites/enable-all", "/api/sites/disable-all"} {
		e.errCode("POST", p+"?project=prj_none", nil, 404)
	}

	// projects CRUD
	var list []model.Project
	if resp := e.do("GET", "/api/projects", nil, &list); resp.StatusCode != 200 || list == nil || len(list) != 0 {
		t.Fatalf("empty list: %d %#v", resp.StatusCode, list)
	}
	e.errCode("POST", "/api/projects", map[string]any{"name": ""}, 400)
	e.errCode("POST", "/api/projects", map[string]any{"name": "p", "serviceIds": []string{"svc_nope"}}, 400)
	e.errCode("POST", "/api/projects", map[string]any{"name": "p", "siteIds": []string{"site_nope"}}, 400)
	e.errCode("POST", "/api/projects", map[string]any{"name": "p", "bogus": 1}, 400)
	var prj model.Project
	resp := e.do("POST", "/api/projects", map[string]any{"name": " Shop ", "serviceIds": []string{a.ID, a.ID}}, &prj)
	if resp.StatusCode != 201 || !strings.HasPrefix(prj.ID, "prj_") || prj.Name != "Shop" || len(prj.ServiceIDs) != 1 || prj.SiteIDs == nil {
		t.Fatalf("create project: %d %+v", resp.StatusCode, prj)
	}
	if raw := e.rawBody("GET", "/api/projects"); !strings.Contains(raw, `"siteIds":[]`) {
		t.Fatalf("siteIds must be a list: %s", raw)
	}
	resp = e.do("PUT", "/api/projects/"+prj.ID, map[string]any{"name": "Shop", "serviceIds": []string{a.ID, b.ID}}, &prj)
	if resp.StatusCode != 200 || len(prj.ServiceIDs) != 2 {
		t.Fatalf("update project: %d %+v", resp.StatusCode, prj)
	}
	e.errCode("PUT", "/api/projects/prj_none", map[string]any{"name": "x"}, 404)

	// start the project's services only
	var rep struct {
		Started []string `json:"started"`
		Failed  []struct{ ID, Name, Error string }
	}
	if resp := e.do("POST", "/api/services/start-all?project="+prj.ID, nil, &rep); resp.StatusCode != 200 || len(rep.Started) != 2 || len(rep.Failed) != 0 {
		t.Fatalf("project start: %d %+v", resp.StatusCode, rep)
	}
	for _, sv := range []model.Service{a, b} {
		waitFor(t, func() bool {
			var cur model.Service
			e.do("GET", "/api/services/"+sv.ID, nil, &cur)
			return cur.State == "running"
		})
	}
	// nothing left to start: both lists are empty, never null
	var raw map[string]json.RawMessage
	e.do("POST", "/api/services/start-all", nil, &raw)
	if string(raw["started"]) != "[]" || string(raw["failed"]) != "[]" {
		t.Fatalf("second start-all: %s", raw)
	}
	var stop struct{ Stopped []string }
	if resp := e.do("POST", "/api/services/stop-all?project="+prj.ID, nil, &stop); resp.StatusCode != 200 || len(stop.Stopped) != 2 {
		t.Fatalf("project stop: %d %+v", resp.StatusCode, stop)
	}

	// a deleted service leaves the project; deleting the project keeps the services
	e.do("DELETE", "/api/services/"+b.ID, nil, nil)
	e.do("GET", "/api/projects", nil, &list)
	if len(list) != 1 || len(list[0].ServiceIDs) != 1 || list[0].ServiceIDs[0] != a.ID {
		t.Fatalf("project after service delete: %+v", list)
	}
	if resp := e.do("DELETE", "/api/projects/"+prj.ID, nil, nil); resp.StatusCode != 204 {
		t.Fatalf("delete project: %d", resp.StatusCode)
	}
	e.errCode("DELETE", "/api/projects/"+prj.ID, nil, 404)
	e.errCode("GET", "/api/services/"+a.ID+"x", nil, 404)
	var one model.Service
	if resp := e.do("GET", "/api/services/"+a.ID, nil, &one); resp.StatusCode != 200 {
		t.Fatal("service must survive its project")
	}
}

func TestStartServiceStartsOne(t *testing.T) {
	e := newEnv(t)
	a := helperService(t, e, 0)
	if resp := e.do("POST", "/api/services/"+a.ID+"/start", nil, nil); resp.StatusCode != 202 {
		t.Fatalf("start: %d", resp.StatusCode)
	}
	e.errCode("POST", "/api/services/"+a.ID+"/start", nil, 409) // already running
	e.errCode("POST", "/api/services/svc_none/start", nil, 404)
}

func TestSitesBulkOverAPI(t *testing.T) {
	e := newEnv(t)
	site := func(domain string, enabled bool) model.SiteView {
		var v model.SiteView
		if resp := e.do("POST", "/api/sites", map[string]any{"name": domain, "domain": domain + ".localhost", "type": "static", "root": t.TempDir(), "enabled": enabled}, &v); resp.StatusCode != 201 {
			t.Fatalf("create site %s: %d", domain, resp.StatusCode)
		}
		return v
	}
	s1, s2, s3 := site("one", true), site("two", true), site("three", false)

	changed := func(path string) int {
		var out map[string]int
		if resp := e.do("POST", path, nil, &out); resp.StatusCode != 200 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
		return out["changed"]
	}
	if n := changed("/api/sites/disable-all"); n != 2 {
		t.Fatalf("disable-all changed %d, want 2", n)
	}
	if n := changed("/api/sites/disable-all"); n != 0 {
		t.Fatalf("second disable-all changed %d, want 0", n)
	}
	if n := changed("/api/sites/enable-all"); n != 3 {
		t.Fatalf("enable-all changed %d, want 3", n)
	}

	var prj model.Project
	e.do("POST", "/api/projects", map[string]any{"name": "P", "siteIds": []string{s1.ID, s2.ID}}, &prj)
	if n := changed("/api/sites/disable-all?project=" + prj.ID); n != 2 {
		t.Fatalf("project disable-all changed %d, want 2", n)
	}
	var sites []model.SiteView
	e.do("GET", "/api/sites", nil, &sites)
	on := 0
	for _, s := range sites {
		if s.Enabled {
			on++
			if s.ID != s3.ID {
				t.Errorf("site %s must stay enabled only for the project's complement", s.Name)
			}
		}
	}
	if on != 1 {
		t.Fatalf("%d sites enabled, want only the one outside the project", on)
	}
	// an empty project touches nothing
	var empty model.Project
	e.do("POST", "/api/projects", map[string]any{"name": "Empty"}, &empty)
	if n := changed("/api/sites/enable-all?project=" + empty.ID); n != 0 {
		t.Fatalf("empty project enabled %d sites", n)
	}
}

func TestTerminalEndpoint(t *testing.T) {
	e := newEnv(t)
	sv := e.newService(t, "term", map[string]any{"env": map[string]string{"Z2D_TERM": "yes"}})

	e.errCode("POST", "/api/services/"+sv.ID+"/terminal", nil, 501) // Terminal is nil

	var dir string
	var env []string
	e.srv.Terminal = func(d string, en []string) error { dir, env = d, en; return nil }
	if resp := e.do("POST", "/api/services/"+sv.ID+"/terminal", nil, nil); resp.StatusCode != 204 {
		t.Fatalf("terminal: %d", resp.StatusCode)
	}
	if dir != sv.Cwd || !containsEnv(env, "Z2D_TERM=yes") {
		t.Fatalf("terminal got dir %q env has Z2D_TERM=%v", dir, containsEnv(env, "Z2D_TERM=yes"))
	}
	e.errCode("POST", "/api/services/svc_none/terminal", nil, 404)

	e.srv.Terminal = func(string, []string) error { return launch.ErrNoTerminal }
	if c := e.errCode("POST", "/api/services/"+sv.ID+"/terminal", nil, 501); c != "not_supported" {
		t.Fatalf("code %s", c)
	}
	e.srv.Terminal = func(string, []string) error { return errors.New("boom") }
	e.errCode("POST", "/api/services/"+sv.ID+"/terminal", nil, 500)
}

func containsEnv(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

func TestEnvironmentConfig(t *testing.T) {
	e := newEnv(t)
	var v struct {
		Config model.EnvConfig
		Report envreg.Report
	}
	e.do("GET", "/api/environment", nil, &v)
	if !v.Config.Seeded || len(v.Config.Path) != 1 || len(v.Config.Vars) != 2 || v.Report.Path == nil || v.Report.Vars == nil {
		t.Fatalf("defaults: %+v", v)
	}
	if raw := e.rawBody("GET", "/api/settings"); strings.Contains(raw, "registerPath") {
		t.Fatalf("the single switch is gone: %s", raw)
	}

	// without a hook the config is only stored; the user's choices are per item
	put := func(path []model.EnvPath, vars []model.EnvVar, want int) {
		t.Helper()
		if resp := e.do("PUT", "/api/environment", map[string]any{"path": path, "vars": vars}, &v); resp.StatusCode != want {
			t.Fatalf("PUT status %d, want %d", resp.StatusCode, want)
		}
	}
	shims := model.EnvPath{ID: "envp_shims", Source: model.EnvShims, Enabled: true}
	java := model.EnvVar{ID: "envv_java_home", Name: "JAVA_HOME", Source: model.EnvDefault, Kind: "java"}
	custom := model.EnvVar{Name: "MY_TOOL", Source: model.EnvCustom, Value: "yes", Enabled: true}
	put([]model.EnvPath{shims}, []model.EnvVar{java, custom}, 200)
	if !v.Config.Path[0].Enabled || v.Config.Vars[0].Enabled || !v.Config.Vars[1].Enabled || !strings.HasPrefix(v.Config.Vars[1].ID, "envv_") {
		t.Fatalf("one on, one off, one new: %+v", v.Config)
	}

	// invalid input is a 400 and saves nothing
	put([]model.EnvPath{shims}, []model.EnvVar{{Name: "PATH", Source: model.EnvCustom, Value: "x", Enabled: true}}, 400)
	put([]model.EnvPath{shims}, []model.EnvVar{{Name: "1BAD", Source: model.EnvCustom, Value: "x"}}, 400)
	put([]model.EnvPath{{Source: model.EnvCustom, Dir: "tools", Enabled: true}}, nil, 400)
	e.do("GET", "/api/environment", nil, &v)
	if len(v.Config.Vars) != 2 || v.Config.Vars[1].Name != "MY_TOOL" {
		t.Fatalf("a rejected update changed the config: %+v", v.Config)
	}
	if resp := e.do("PUT", "/api/environment", map[string]any{"path": []any{}, "vars": []any{}, "bogus": 1}, nil); resp.StatusCode != 400 {
		t.Fatalf("unknown fields are refused: %d", resp.StatusCode)
	}

	// the hook sees the config being saved; a failure is a 409 that saves nothing
	var applied []model.EnvConfig
	var fail error
	e.srv.EnvApply = func(d store.Data) error {
		applied = append(applied, d.Env)
		return fail
	}
	e.srv.EnvReport = func(d store.Data) envreg.Report {
		return envreg.Report{Supported: true, Path: []envreg.PathStatus{}, Vars: []envreg.VarStatus{}}
	}
	put([]model.EnvPath{shims}, []model.EnvVar{java, custom}, 200)
	if len(applied) != 1 || !v.Report.Supported {
		t.Fatalf("applied %d, report %+v", len(applied), v.Report)
	}
	fail = errors.New("access denied")
	var er struct{ Error struct{ Message string } }
	if resp := e.do("PUT", "/api/environment", map[string]any{"path": []any{}, "vars": []any{}}, &er); resp.StatusCode != 409 || !strings.Contains(er.Error.Message, "access denied") {
		t.Fatalf("failure: %d %+v", resp.StatusCode, er)
	}
	e.do("GET", "/api/environment", nil, &v)
	if len(v.Config.Path) != 1 || len(v.Config.Vars) != 2 {
		t.Fatalf("a failed apply must save nothing: %+v", v.Config)
	}
}

func TestOldServiceInputStillAccepted(t *testing.T) {
	// a client from before 0.0.4 sends none of the new fields
	e := newEnv(t)
	sv := e.newService(t, "old", nil)
	if sv.RestartLimit != 5 || len(sv.DependsOn) != 0 {
		t.Fatalf("%+v", sv)
	}
	e.errCode("POST", "/api/services", svcBody("x", t.TempDir(), map[string]any{"restarts": 3}), 400) // supervisor-filled, not input
}
