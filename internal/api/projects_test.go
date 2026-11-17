package api

import (
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

type startRep struct {
	Started []string `json:"started"`
	Failed  []struct{ ID, Name, Error string }
}

func (e *tenv) newSite(t *testing.T, domain string, enabled bool) model.SiteView {
	t.Helper()
	var v model.SiteView
	body := map[string]any{"name": domain, "domain": domain + ".localhost", "type": "static", "root": t.TempDir(), "enabled": enabled}
	if resp := e.do("POST", "/api/sites", body, &v); resp.StatusCode != 201 {
		t.Fatalf("create site %s: %d", domain, resp.StatusCode)
	}
	return v
}

func (e *tenv) newProject(t *testing.T, name string, svcs, sites []string) model.Project {
	t.Helper()
	var p model.Project
	if resp := e.do("POST", "/api/projects", map[string]any{"name": name, "serviceIds": svcs, "siteIds": sites}, &p); resp.StatusCode != 201 {
		t.Fatalf("create project %s: %d", name, resp.StatusCode)
	}
	return p
}

func (e *tenv) stateOf(id string) string {
	var cur model.Service
	e.do("GET", "/api/services/"+id, nil, &cur)
	return cur.State
}

func (e *tenv) siteEnabled(id string) bool {
	var list []model.SiteView
	e.do("GET", "/api/sites", nil, &list)
	for _, s := range list {
		if s.ID == id {
			return s.Enabled
		}
	}
	return false
}

func TestProjectValidationShapes(t *testing.T) {
	e := newEnv(t)
	svc := helperService(t, e, 0)

	if c := e.errCode("POST", "/api/projects", map[string]any{"name": "   "}, 400); c != "bad_request" {
		t.Fatalf("blank name: %s", c)
	}
	e.errCode("POST", "/api/projects", map[string]any{"name": strings.Repeat("x", 65)}, 400)
	e.errCode("POST", "/api/projects", `{"name":`, 400) // not JSON
	e.errCode("POST", "/api/projects", map[string]any{"name": "ok", "serviceIds": []string{svc.ID, "svc_gone"}}, 400)

	// 64 characters is allowed; lists are always arrays in the answer
	p := e.newProject(t, strings.Repeat("é", 64), nil, nil)
	raw := e.rawBody("GET", "/api/projects")
	if !strings.Contains(raw, `"serviceIds":[]`) || !strings.Contains(raw, `"siteIds":[]`) {
		t.Fatalf("lists must be [], not null: %s", raw)
	}

	// PUT replaces the members; unknown ids and unknown projects are rejected
	e.errCode("PUT", "/api/projects/"+p.ID, map[string]any{"name": "p", "serviceIds": []string{"svc_gone"}}, 400)
	e.errCode("PUT", "/api/projects/prj_none", map[string]any{"name": "p"}, 404)
	var got model.Project
	if resp := e.do("PUT", "/api/projects/"+p.ID, map[string]any{"name": "Renamed", "serviceIds": []string{svc.ID}}, &got); resp.StatusCode != 200 || got.Name != "Renamed" || got.ID != p.ID || len(got.ServiceIDs) != 1 {
		t.Fatalf("update: %d %+v", resp.StatusCode, got)
	}
}

func TestProjectStartEnablesSitesAndStartsMembers(t *testing.T) {
	e := newEnv(t)
	a, b, other := helperService(t, e, 0), helperService(t, e, 0), helperService(t, e, 0)
	on, off, outside := e.newSite(t, "on", true), e.newSite(t, "off", false), e.newSite(t, "outside", false)
	prj := e.newProject(t, "Shop", []string{a.ID, b.ID}, []string{on.ID, off.ID})

	var rep startRep
	if resp := e.do("POST", "/api/services/start-all?project="+prj.ID, nil, &rep); resp.StatusCode != 200 {
		t.Fatalf("project start: %d", resp.StatusCode)
	}
	if len(rep.Started) != 2 || !contains(rep.Started, a.ID) || !contains(rep.Started, b.ID) {
		t.Fatalf("started %v, want the two members", rep.Started)
	}
	if !e.siteEnabled(on.ID) || !e.siteEnabled(off.ID) {
		t.Fatal("the project's sites must be enabled")
	}
	if e.siteEnabled(outside.ID) {
		t.Fatal("a site outside the project must stay as it was")
	}
	for _, id := range []string{a.ID, b.ID} {
		waitFor(t, func() bool { return e.stateOf(id) == "running" })
	}
	if st := e.stateOf(other.ID); st == "running" || st == "starting" {
		t.Fatalf("a service outside the project was started: %s", st)
	}

	// a second start finds everything up: empty, non-null lists
	var again map[string]any
	e.do("POST", "/api/services/start-all?project="+prj.ID, nil, &again)
	if s, _ := again["started"].([]any); s == nil || len(s) != 0 {
		t.Fatalf("second start: %v", again)
	}
}

func TestProjectStopStopsOnlyMembers(t *testing.T) {
	e := newEnv(t)
	a, b, other := helperService(t, e, 0), helperService(t, e, 0), helperService(t, e, 0)
	s := e.newSite(t, "keep", true)
	prj := e.newProject(t, "Pair", []string{a.ID, b.ID}, []string{s.ID})
	for _, sv := range []model.Service{a, b, other} {
		if resp := e.do("POST", "/api/services/"+sv.ID+"/start", nil, nil); resp.StatusCode != 202 {
			t.Fatalf("start %s: %d", sv.ID, resp.StatusCode)
		}
		waitFor(t, func() bool { return e.stateOf(sv.ID) == "running" })
	}

	var rep struct {
		Stopped []string `json:"stopped"`
		Failed  []any    `json:"failed"`
	}
	if resp := e.do("POST", "/api/services/stop-all?project="+prj.ID, nil, &rep); resp.StatusCode != 200 || len(rep.Stopped) != 2 || len(rep.Failed) != 0 {
		t.Fatalf("project stop: %d %+v", resp.StatusCode, rep)
	}
	if st := e.stateOf(a.ID); st != "stopped" {
		t.Fatalf("member a: %s", st)
	}
	if st := e.stateOf(b.ID); st != "stopped" {
		t.Fatalf("member b: %s", st)
	}
	if st := e.stateOf(other.ID); st != "running" {
		t.Fatalf("a service outside the project must keep running, got %s", st)
	}
	if !e.siteEnabled(s.ID) {
		t.Fatal("stopping a project must not disable its sites")
	}

	// nothing running any more: an empty report, not an error
	var again struct{ Stopped []string }
	if resp := e.do("POST", "/api/services/stop-all?project="+prj.ID, nil, &again); resp.StatusCode != 200 || again.Stopped == nil || len(again.Stopped) != 0 {
		t.Fatalf("second stop: %d %+v", resp.StatusCode, again)
	}
}

// An empty project must start and stop nothing: a nil id list would mean
// "every service" to the supervisor.
func TestEmptyProjectTouchesNothing(t *testing.T) {
	e := newEnv(t)
	other := helperService(t, e, 0)
	if resp := e.do("POST", "/api/services/"+other.ID+"/start", nil, nil); resp.StatusCode != 202 {
		t.Fatalf("start: %d", resp.StatusCode)
	}
	waitFor(t, func() bool { return e.stateOf(other.ID) == "running" })
	idle := helperService(t, e, 0)
	prj := e.newProject(t, "Empty", nil, nil)

	var start startRep
	e.do("POST", "/api/services/start-all?project="+prj.ID, nil, &start)
	if len(start.Started) != 0 || len(start.Failed) != 0 {
		t.Fatalf("empty project start: %+v", start)
	}
	if st := e.stateOf(idle.ID); st != "stopped" {
		t.Fatalf("a service outside an empty project was started: %s", st)
	}
	var stop struct{ Stopped []string }
	e.do("POST", "/api/services/stop-all?project="+prj.ID, nil, &stop)
	if len(stop.Stopped) != 0 || e.stateOf(other.ID) != "running" {
		t.Fatalf("empty project stop touched services: %+v, %s", stop, e.stateOf(other.ID))
	}
}

func TestProjectWithDeletedMembers(t *testing.T) {
	e := newEnv(t)
	a, gone := helperService(t, e, 0), helperService(t, e, 0)
	s, goneSite := e.newSite(t, "kept", false), e.newSite(t, "gone", false)
	prj := e.newProject(t, "Mixed", []string{a.ID, gone.ID}, []string{s.ID, goneSite.ID})

	e.do("DELETE", "/api/services/"+gone.ID, nil, nil)
	e.do("DELETE", "/api/sites/"+goneSite.ID, nil, nil)

	var list []model.Project
	e.do("GET", "/api/projects", nil, &list)
	if len(list) != 1 || len(list[0].ServiceIDs) != 1 || len(list[0].SiteIDs) != 1 {
		t.Fatalf("dangling ids must leave the project: %+v", list)
	}

	// even a stale project value (read before the deletion) starts cleanly
	var rep startRep
	if resp := e.do("POST", "/api/services/start-all?project="+prj.ID, nil, &rep); resp.StatusCode != 200 || len(rep.Started) != 1 || rep.Started[0] != a.ID || len(rep.Failed) != 0 {
		t.Fatalf("start: %d %+v", resp.StatusCode, rep)
	}
	if !e.siteEnabled(s.ID) {
		t.Fatal("the remaining site must be enabled")
	}
	stale := model.Project{ID: prj.ID, Name: "Mixed", ServiceIDs: []string{a.ID, gone.ID}, SiteIDs: []string{s.ID, goneSite.ID}}
	waitFor(t, func() bool { return e.stateOf(a.ID) == "running" })
	if r := e.srv.stopProject(stale); len(r.Stopped) != 1 || len(r.Failed) != 0 {
		t.Fatalf("stale stop: %+v", r)
	}
	if r := e.srv.startProject(stale); len(r.Failed) != 0 {
		t.Fatalf("stale start: %+v", r)
	}
	waitFor(t, func() bool { return e.stateOf(a.ID) == "running" })
}

// A project with a site starts the site server service as well, and a project
// without sites does not.
func TestProjectStartIncludesSiteServer(t *testing.T) {
	e := newEnv(t)
	app := helperService(t, e, 0)
	// A caddy service with no runtime installed cannot start; the attempt shows
	// in the report, which is how we know it was part of the start.
	caddyID := "svc_caddy"
	if err := e.srv.Store.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{ID: caddyID, Name: "Caddy", Kind: "caddy", Cwd: e.home, Args: []string{}, Env: map[string]string{}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	site := e.newSite(t, "web", false)
	withSite := e.newProject(t, "With site", []string{app.ID}, []string{site.ID})
	noSite := e.newProject(t, "No site", []string{app.ID}, nil)

	var rep startRep
	e.do("POST", "/api/services/start-all?project="+noSite.ID, nil, &rep)
	for _, f := range rep.Failed {
		if f.ID == caddyID {
			t.Fatal("a project without sites must not start the site server")
		}
	}
	waitFor(t, func() bool { return e.stateOf(app.ID) == "running" })
	e.do("POST", "/api/services/stop-all?project="+noSite.ID, nil, nil)

	rep = startRep{}
	e.do("POST", "/api/services/start-all?project="+withSite.ID, nil, &rep)
	tried := contains(rep.Started, caddyID)
	for _, f := range rep.Failed {
		if f.ID == caddyID {
			tried = true
		}
	}
	if !tried {
		t.Fatalf("the site server was not part of the project start: %+v", rep)
	}
	if !e.siteEnabled(site.ID) {
		t.Fatal("the site must be enabled")
	}
}

func TestSiteServerServiceID(t *testing.T) {
	svcs := []model.Service{
		{ID: "svc_app", Kind: "node"},
		{ID: "svc_ngx_db", Kind: "nginx"}, // an nginx the user runs themselves
		{ID: "svc_caddy", Kind: "caddy"},
		{ID: "svc_ngx", Kind: "nginx", Args: []string{"-c", "{home}/config/nginx.conf"}},
	}
	if id := siteServerServiceID(store.Data{Services: svcs}); id != "svc_caddy" {
		t.Fatalf("caddy: %q", id)
	}
	if id := siteServerServiceID(store.Data{Services: svcs, Settings: model.Settings{SiteServer: "nginx"}}); id != "svc_ngx" {
		t.Fatalf("nginx: %q", id)
	}
	if id := siteServerServiceID(store.Data{Services: svcs[:2]}); id != "" {
		t.Fatalf("none: %q", id)
	}
}

func contains(list []string, id string) bool {
	for _, s := range list {
		if s == id {
			return true
		}
	}
	return false
}
