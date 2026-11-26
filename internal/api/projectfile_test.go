package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/projectfile"
	"zero2dev/internal/store"
)

// The project file routes answer 501 only while no manager is wired.
func TestProjectFileRoutesUnwired(t *testing.T) {
	e := newEnv(t)
	e.errCode("POST", "/api/projects/prj_x/export", map[string]any{"path": "x"}, 501)
	e.errCode("POST", "/api/projects/import/plan", map[string]any{"path": "x"}, 501)
	e.errCode("POST", "/api/projects/import", map[string]any{"path": "x"}, 501)
}

func TestProjectFileRoutes(t *testing.T) {
	e := newEnv(t)
	e.srv.Jobs = jobs.New(e.hub)
	m := projectfile.New(e.home, e.srv.Store, e.sup, nil, nil, e.srv.Sites, e.srv.Jobs, e.hub.Publish)
	m.PortFree = func(int) bool { return true }
	e.srv.ProjectFile = m

	proj := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(filepath.Join(proj, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "node.exe")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := e.srv.Store.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_node", Kind: "node", Version: "22.1.0", Name: "Node 22", Binary: bin, Source: "registered", Default: true})
		d.Services = append(d.Services, model.Service{ID: "svc_web", Name: "Web", Kind: "node", Cwd: proj, Port: 3000, Settings: map[string]string{"script": "dev"},
			Env: map[string]string{"APP_ENV": "local", "API_TOKEN": "shh"}, DependsOn: []string{}, RestartLimit: model.DefaultRestartLimit})
		d.Sites = append(d.Sites, model.Site{ID: "site_web", Name: "Web", Domain: "web.localhost", Type: "proxy", ServiceID: "svc_web", Enabled: true})
		d.Projects = append(d.Projects, model.Project{ID: "prj_web", Name: "Web", ServiceIDs: []string{"svc_web"}, SiteIDs: []string{"site_web"}})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// export: 404, 400, 200 with warnings as a list, 409 without overwrite, 200 with it
	e.errCode("POST", "/api/projects/prj_nope/export", map[string]any{"path": proj}, 404)
	e.errCode("POST", "/api/projects/prj_web/export", map[string]any{"path": "relative"}, 400)
	e.errCode("POST", "/api/projects/prj_web/export", map[string]any{"path": 5}, 400)
	var ex projectfile.ExportResult
	if resp := e.do("POST", "/api/projects/prj_web/export", map[string]any{"path": proj}, &ex); resp.StatusCode != 200 ||
		ex.Path != filepath.Join(proj, projectfile.FileName) || len(ex.Warnings) != 1 || !strings.Contains(ex.Warnings[0], "API_TOKEN of Web") {
		t.Fatalf("export: %d %+v", resp.StatusCode, ex)
	}
	b, _ := os.ReadFile(ex.Path)
	if strings.Contains(string(b), "shh") || strings.Contains(string(b), "svc_") {
		t.Fatalf("the file holds a secret or an id:\n%s", b)
	}
	e.errCode("POST", "/api/projects/prj_web/export", map[string]any{"path": ex.Path}, 409)
	if resp := e.do("POST", "/api/projects/prj_web/export", map[string]any{"path": ex.Path, "overwrite": true}, &ex); resp.StatusCode != 200 {
		t.Fatalf("overwrite: %d", resp.StatusCode)
	}
	var raw map[string]any
	e.do("POST", "/api/projects/prj_web/export", map[string]any{"path": ex.Path, "overwrite": true}, &raw)
	if _, ok := raw["warnings"].([]any); !ok {
		t.Fatalf("warnings must be a list: %v", raw)
	}

	// plan: lists are [], nothing changes; the web port and domain are taken by the project itself
	var plan projectfile.Plan
	if resp := e.do("POST", "/api/projects/import/plan", map[string]any{"path": proj, "name": "Web 2"}, &plan); resp.StatusCode != 200 ||
		plan.Name != "Web 2" || plan.Dir != proj || len(plan.Problems) != 0 || len(plan.Installs) != 0 ||
		len(plan.Services) != 1 || plan.Services[0] != "Web (Node.js, port 3001)" || len(plan.Sites) != 1 || plan.Sites[0] != "http://web-2.localhost -> Web" || len(plan.Changes) != 2 {
		t.Fatalf("plan: %d %+v", resp.StatusCode, plan)
	}
	e.do("POST", "/api/projects/import/plan", map[string]any{"path": proj}, &raw)
	for _, k := range []string{"services", "sites", "installs", "changes", "problems", "warnings"} {
		if _, ok := raw[k].([]any); !ok {
			t.Errorf("%s must be a list, got %v", k, raw[k])
		}
	}
	// problems are part of a 200 plan; a bad request is a 400
	var bad projectfile.Plan
	if resp := e.do("POST", "/api/projects/import/plan", map[string]any{"path": filepath.Join(proj, "missing.json")}, &bad); resp.StatusCode != 200 || len(bad.Problems) != 1 {
		t.Fatalf("problem plan: %d %+v", resp.StatusCode, bad)
	}
	e.errCode("POST", "/api/projects/import/plan", map[string]any{"path": ""}, 400)

	// import: a plan with problems is a 409 with the first problem as the message
	var er struct{ Error struct{ Message string } }
	if resp := e.do("POST", "/api/projects/import", map[string]any{"path": filepath.Join(proj, "missing.json")}, &er); resp.StatusCode != 409 || !strings.Contains(er.Error.Message, "does not exist") {
		t.Fatalf("problem import: %d %+v", resp.StatusCode, er)
	}
	var job model.Job
	if resp := e.do("POST", "/api/projects/import", map[string]any{"path": proj, "name": "Web 2"}, &job); resp.StatusCode != 202 || job.Type != "project-import" || job.State != "running" {
		t.Fatalf("import: %d %+v", resp.StatusCode, job)
	}
	done := waitJobAPI(e, job.ID)
	if done.State != "done" || done.ProjectID == "" || done.Path != proj {
		t.Fatalf("job %+v", done)
	}
	var projects []model.Project
	var svcs []model.Service
	var sites []model.SiteView
	e.do("GET", "/api/projects", nil, &projects)
	e.do("GET", "/api/services", nil, &svcs)
	e.do("GET", "/api/sites", nil, &sites)
	if len(projects) != 2 || len(svcs) != 2 || len(sites) != 2 {
		t.Fatalf("%d projects %d services %d sites", len(projects), len(svcs), len(sites))
	}
	var created model.Project
	for _, p := range projects {
		if p.ID == done.ProjectID {
			created = p
		}
	}
	if created.Name != "Web 2" || len(created.ServiceIDs) != 1 || len(created.SiteIDs) != 1 {
		t.Fatalf("%+v", created)
	}
	for _, s := range svcs {
		if s.ID == created.ServiceIDs[0] && (s.Port != 3001 || s.Cwd != proj || s.Env["API_TOKEN"] != "") {
			t.Fatalf("%+v", s)
		}
	}
	// no file was added to the project folder besides the export
	if entries, _ := os.ReadDir(proj); len(entries) != 2 {
		t.Fatalf("the project folder holds %d entries", len(entries))
	}
}
