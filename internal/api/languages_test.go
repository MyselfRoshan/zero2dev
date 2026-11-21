package api

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/detect"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/newproject"
	"zero2dev/internal/store"
	"zero2dev/internal/tasks"
)

// The routes whose managers are not wired answer 501; detection and the frameworks list work.
func TestLanguageRoutesUnwired(t *testing.T) {
	e := newEnv(t)
	// not wired: 501
	e.errCode("GET", "/api/services/svc_x/tasks", nil, 501)
	e.errCode("POST", "/api/services/svc_x/tasks", map[string]any{"preset": "npm-install"}, 501)
	e.errCode("POST", "/api/new-project/plan", map[string]any{"framework": "html"}, 501)
	e.errCode("POST", "/api/new-project", map[string]any{"framework": "html"}, 501)
	e.errCode("GET", "/api/project/detect", nil, 400)

	// the shipped list is served as {frameworks: [...]}
	var got struct {
		Frameworks []struct {
			ID, Language string
		}
	}
	if resp := e.do("GET", "/api/frameworks", nil, &got); resp.StatusCode != 200 || len(got.Frameworks) < 2 {
		t.Fatalf("frameworks %d %+v", resp.StatusCode, got)
	}
}

func TestDetectRoute(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	// nothing in the folder: empty lists, never null
	var empty map[string]any
	if resp := e.do("GET", "/api/project/detect?dir="+url.QueryEscape(dir), nil, &empty); resp.StatusCode != 200 ||
		empty["kind"] != "" || len(empty["markers"].([]any)) != 0 || len(empty["settings"].(map[string]any)) != 0 {
		t.Fatalf("empty folder: %d %v", resp.StatusCode, empty)
	}
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"shop","scripts":{"dev":"vite"},"devDependencies":{"vite":"5","vue":"3"}}`), 0o644)
	var got detect.Result
	if resp := e.do("GET", "/api/project/detect?dir="+url.QueryEscape(dir), nil, &got); resp.StatusCode != 200 ||
		got.Kind != "node" || got.Framework != "vite-vue" || got.Name != "shop" || got.Confidence != "high" || got.Port != 5173 ||
		got.Settings["run"] != "npm" || got.Settings["npmScript"] != "dev" {
		t.Fatalf("detect: %d %+v", resp.StatusCode, got)
	}
	e.errCode("GET", "/api/project/detect?dir="+url.QueryEscape(filepath.Join(dir, "missing")), nil, 400)
}

// taskEnv wires a tasks manager with a fake runner and a node service in a temp folder.
func taskEnv(t *testing.T) (*tenv, string, *tasks.Manager) {
	e := newEnv(t)
	dir := t.TempDir()
	tools := t.TempDir()
	for _, n := range []string{"npm", "npm.cmd"} {
		os.WriteFile(filepath.Join(tools, n), []byte("x"), 0o755)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	e.srv.Store.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{ID: "svc_task", Name: "Shop", Kind: "node", Cwd: dir, Port: 3000, Settings: map[string]string{}})
		return nil
	})
	e.srv.Jobs = jobs.New(e.hub)
	m := tasks.New(e.home, e.srv.Store, e.sup, e.srv.Jobs)
	e.srv.Tasks = m
	return e, dir, m
}

func TestTaskRoutes(t *testing.T) {
	e, dir, m := taskEnv(t)
	release := make(chan struct{})
	m.Run = func(ctx context.Context, c tasks.Cmd) error {
		fmt.Fprintln(c.Out, "hello from "+filepath.Base(c.Bin))
		<-release
		return nil
	}

	var l struct {
		Presets []struct{ ID, Label, Program string }
		Scripts []struct{ Name, Command string }
		Running string
	}
	// no package.json yet: no presets, and the lists are [] not null
	var raw map[string]any
	if e.do("GET", "/api/services/svc_task/tasks", nil, &raw); raw["presets"] == nil || raw["scripts"] == nil || raw["running"] != "" {
		t.Fatalf("empty: %v", raw)
	}
	e.errCode("GET", "/api/services/svc_nope/tasks", nil, 404)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node a.js","build":"tsc"}}`), 0o644)
	if resp := e.do("GET", "/api/services/svc_task/tasks", nil, &l); resp.StatusCode != 200 || len(l.Presets) != 3 || l.Presets[0].ID != "npm-install" ||
		len(l.Scripts) != 2 || l.Scripts[0].Name != "build" || l.Running != "" {
		t.Fatalf("list: %d %+v", resp.StatusCode, l)
	}

	// bad requests
	e.errCode("POST", "/api/services/svc_task/tasks", map[string]any{}, 400)
	e.errCode("POST", "/api/services/svc_task/tasks", map[string]any{"preset": "npm-install", "program": "npm"}, 400)
	e.errCode("POST", "/api/services/svc_task/tasks", map[string]any{"program": "bin/npm"}, 400)
	e.errCode("POST", "/api/services/svc_task/tasks", map[string]any{"program": "npm", "args": []string{"a" + string(rune(10)) + "b"}}, 400)
	e.errCode("POST", "/api/services/svc_task/tasks", map[string]any{"preset": "nope"}, 404)
	e.errCode("POST", "/api/services/svc_nope/tasks", map[string]any{"preset": "npm-install"}, 404)

	// run a preset: 202 with the job, a second one is 409 naming the first
	var job model.Job
	if resp := e.do("POST", "/api/services/svc_task/tasks", map[string]any{"preset": "npm-build"}, &job); resp.StatusCode != 202 ||
		job.Type != "task" || job.ServiceID != "svc_task" || job.Step != "Build" || job.State != "running" {
		t.Fatalf("start: %d %+v", resp.StatusCode, job)
	}
	var er struct {
		Error struct{ Details map[string]string }
	}
	if resp := e.do("POST", "/api/services/svc_task/tasks", map[string]any{"program": "npm", "args": []string{"-v"}}, &er); resp.StatusCode != 409 || er.Error.Details["jobId"] != job.ID {
		t.Fatalf("second: %d %+v", resp.StatusCode, er)
	}
	if e.do("GET", "/api/services/svc_task/tasks", nil, &l); l.Running != job.ID {
		t.Fatalf("running %q want %q", l.Running, job.ID)
	}
	close(release)
	if done := waitJobAPI(e, job.ID); done.State != "done" {
		t.Fatalf("job %+v", done)
	}
	if e.do("GET", "/api/services/svc_task/tasks", nil, &l); l.Running != "" {
		t.Fatalf("after the end: %q", l.Running)
	}
	// the output is in the service's log under the stream "task"
	var logs []model.LogLine
	for range 100 {
		var body struct{ Lines []model.LogLine }
		e.do("GET", "/api/services/svc_task/logs", nil, &body)
		logs = body.Lines
		if len(logs) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var task []string
	for _, ln := range logs {
		if ln.Stream == "task" {
			task = append(task, ln.Text)
		}
	}
	if len(task) != 2 || task[0] != "$ npm run build" || !strings.HasPrefix(task[1], "hello from npm") {
		t.Fatalf("log %q (all %+v)", task, logs)
	}

	// a failing program fails the job with its code
	m.Run = func(ctx context.Context, c tasks.Cmd) error {
		fmt.Fprintln(c.Out, "boom")
		return &tasks.ExitError{Code: 2}
	}
	e.do("POST", "/api/services/svc_task/tasks", map[string]any{"program": "npm", "args": []string{"x"}}, &job)
	if f := waitJobAPI(e, job.ID); f.State != "failed" || f.Error != "npm exited with code 2: boom" {
		t.Fatalf("failed job %+v", f)
	}
	// a service held by another operation
	rel, err := e.sup.Reserve([]string{"svc_task"}, "taking a snapshot")
	if err != nil {
		t.Fatal(err)
	}
	e.errCode("POST", "/api/services/svc_task/tasks", map[string]any{"program": "npm"}, 409)
	rel()
}

func TestTaskComposerMissingIs409(t *testing.T) {
	e, dir, _ := taskEnv(t)
	e.srv.Store.Update(func(d *store.Data) error { d.Services[len(d.Services)-1].Kind = "php"; return nil })
	os.WriteFile(filepath.Join(dir, "composer.json"), []byte("{}"), 0o644)
	var er struct{ Error struct{ Message string } }
	if resp := e.do("POST", "/api/services/svc_task/tasks", map[string]any{"preset": "composer-install"}, &er); resp.StatusCode != 409 || !strings.Contains(er.Error.Message, "Runtimes") {
		t.Fatalf("%d %+v", resp.StatusCode, er)
	}
}

// newProjectEnv wires a New project manager (the real stores and supervisor, no installer and no
// database tools) to the server.
func newProjectEnv(t *testing.T) (*tenv, string) {
	e := newEnv(t)
	e.srv.Jobs = jobs.New(e.hub)
	parent := t.TempDir()
	m := newproject.New(e.home, e.srv.Store, e.sup, nil, nil, e.srv.Sites, nil, e.srv.Jobs, e.hub.Publish)
	m.PortFree = func(int) bool { return true }
	e.srv.NewProject = m
	return e, parent
}

func TestNewProjectRoutes(t *testing.T) {
	e, parent := newProjectEnv(t)
	req := map[string]any{"framework": "html", "name": "My Shop", "parent": parent}

	// plan: nothing is changed, the lists are [] and never null
	var plan newproject.Plan
	if resp := e.do("POST", "/api/new-project/plan", req, &plan); resp.StatusCode != 200 || plan.Slug != "my-shop" || plan.Domain != "my-shop.localhost" ||
		plan.Dir != filepath.Join(parent, "my-shop") || len(plan.Problems) != 0 || len(plan.Installs) != 0 || plan.Database != nil || len(plan.Steps) != 2 {
		t.Fatalf("plan: %d %+v", resp.StatusCode, plan)
	}
	if _, err := os.Stat(plan.Dir); err == nil {
		t.Fatal("a plan must not create the folder")
	}
	var raw map[string]any
	e.do("POST", "/api/new-project/plan", req, &raw)
	for _, k := range []string{"installs", "steps", "creates", "problems", "warnings"} {
		if _, ok := raw[k].([]any); !ok {
			t.Errorf("%s must be a list, got %v", k, raw[k])
		}
	}
	// problems are part of a 200 plan; the request errors are 400
	var bad newproject.Plan
	if resp := e.do("POST", "/api/new-project/plan", map[string]any{"framework": "html", "name": "x", "parent": parent, "databaseKind": "mariadb"}, &bad); resp.StatusCode != 200 || len(bad.Problems) != 1 {
		t.Fatalf("problem plan: %d %+v", resp.StatusCode, bad)
	}
	e.errCode("POST", "/api/new-project/plan", map[string]any{"framework": "nope", "name": "x", "parent": parent}, 400)
	e.errCode("POST", "/api/new-project/plan", map[string]any{"framework": 5}, 400)

	// start: a plan with problems is a 409 with the first problem as the message
	var er struct{ Error struct{ Message string } }
	if resp := e.do("POST", "/api/new-project", map[string]any{"framework": "html", "name": "x", "parent": filepath.Join(parent, "missing")}, &er); resp.StatusCode != 409 || !strings.Contains(er.Error.Message, "does not exist") {
		t.Fatalf("problem start: %d %+v", resp.StatusCode, er)
	}
	e.errCode("POST", "/api/new-project", map[string]any{"framework": "nope"}, 400)

	var job model.Job
	if resp := e.do("POST", "/api/new-project", req, &job); resp.StatusCode != 202 || job.Type != "new-project" || job.State != "running" {
		t.Fatalf("start: %d %+v", resp.StatusCode, job)
	}
	done := waitJobAPI(e, job.ID)
	if done.State != "done" || done.Path != plan.Dir || done.ProjectID == "" {
		t.Fatalf("job %+v", done)
	}
	if b, _ := os.ReadFile(filepath.Join(plan.Dir, "index.html")); !strings.Contains(string(b), "<h1>My Shop</h1>") {
		t.Fatalf("index.html %q", b)
	}
	var projects []model.Project
	e.do("GET", "/api/projects", nil, &projects)
	var sites []model.SiteView
	e.do("GET", "/api/sites", nil, &sites)
	if len(projects) != 1 || projects[0].ID != done.ProjectID || projects[0].Name != "My Shop" || len(projects[0].SiteIDs) != 1 ||
		len(sites) != 1 || sites[0].Domain != "my-shop.localhost" || sites[0].Type != "static" || sites[0].Root != plan.Dir {
		t.Fatalf("projects %+v sites %+v", projects, sites)
	}
	// the folder now has files: the same request is a 409
	e.errCode("POST", "/api/new-project", req, 409)
}

// 0.0.6 part 2, Wave 0: the project file routes exist and answer 501 until they are built.
func TestProjectFileRoutesWave0(t *testing.T) {
	e := newEnv(t)
	e.errCode("POST", "/api/projects/prj_x/export", map[string]any{"path": "x"}, 501)
	e.errCode("POST", "/api/projects/import/plan", map[string]any{"path": "x"}, 501)
	e.errCode("POST", "/api/projects/import", map[string]any{"path": "x"}, 501)
}
