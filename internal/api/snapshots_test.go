package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/snapshot"
)

// snapEnv is newEnv with the snapshot manager and the job registry wired in.
func snapEnv(t *testing.T) *tenv {
	e := newEnv(t)
	reg := jobs.New(e.hub)
	e.srv.Jobs = reg
	e.srv.Snapshots = snapshot.New(e.home, "test", e.srv.Store, e.sup, reg, e.srv.Sites, e.hub.Publish, e.srv.Catalog)
	return e
}

// waitJob polls GET /api/jobs/{id} until the job has ended.
func (e *tenv) waitJob(id string) model.Job {
	e.t.Helper()
	var j model.Job
	waitFor(e.t, func() bool {
		e.do("GET", "/api/jobs/"+id, nil, &j)
		return j.State != "running"
	})
	if j.State != "done" {
		e.t.Fatalf("job %s: %s %s", id, j.State, j.Error)
	}
	return j
}

func TestSnapshotsNotWired(t *testing.T) {
	e := newEnv(t)
	if c := e.errCode("GET", "/api/snapshots/snp_x", nil, 501); c != "not_supported" {
		t.Fatal(c)
	}
}

func TestSnapshotsOverAPI(t *testing.T) {
	e := snapEnv(t)
	var db, app model.Service
	e.do("POST", "/api/services", map[string]any{"name": "DB", "kind": "mariadb", "port": 3306}, &db)
	e.do("POST", "/api/services", map[string]any{"name": "App", "kind": "custom", "command": "x", "cwd": t.TempDir(), "port": 3000, "databaseId": db.ID}, &app)
	if db.ID == "" || app.ID == "" {
		t.Fatal("services not created")
	}
	var site model.SiteView
	e.do("POST", "/api/sites", map[string]any{"name": "App", "domain": "app.localhost", "type": "proxy", "serviceId": app.ID}, &site)
	var prj model.Project
	e.do("POST", "/api/projects", map[string]any{"name": "Shop", "serviceIds": []string{db.ID, app.ID}, "siteIds": []string{site.ID}}, &prj)
	if prj.ID == "" || site.ID == "" {
		t.Fatal("project or site not created")
	}
	dataDir := e.sup.DataDir("mariadb", db.ID)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "ibdata1"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Errors.
	e.errCode("GET", "/api/services/"+app.ID+"/snapshots", nil, 400)
	e.errCode("POST", "/api/services/"+app.ID+"/snapshots", map[string]any{"name": "x"}, 400)
	e.errCode("POST", "/api/projects/prj_none/snapshots", map[string]any{"name": "x"}, 404)
	e.errCode("POST", "/api/projects/"+prj.ID+"/snapshots", map[string]any{"name": ""}, 400)
	e.errCode("POST", "/api/projects/"+prj.ID+"/snapshots", map[string]any{"name": "x", "bogus": 1}, 400)
	e.errCode("GET", "/api/snapshots/snp_none", nil, 404)
	e.errCode("POST", "/api/snapshots/snp_none/restore", nil, 404)
	e.errCode("POST", "/api/snapshots/snp_none/clone", map[string]any{"name": "x"}, 404)
	e.errCode("DELETE", "/api/snapshots/snp_none", nil, 404)

	// Nothing yet: [] (never null).
	for _, p := range []string{"/api/projects/" + prj.ID + "/snapshots", "/api/services/" + db.ID + "/snapshots", "/api/projects/prj_deleted/snapshots"} {
		if body := strings.TrimSpace(e.rawBody("GET", p)); body != "[]" {
			t.Fatalf("%s: %s", p, body)
		}
	}

	// Take a project snapshot and a service snapshot.
	var j model.Job
	if resp := e.do("POST", "/api/projects/"+prj.ID+"/snapshots", map[string]any{"name": "Before"}, &j); resp.StatusCode != 202 || j.Type != "snapshot" || j.ProjectID != prj.ID {
		t.Fatalf("take: %d %+v", resp.StatusCode, j)
	}
	snapID := e.waitJob(j.ID).SnapshotID
	if resp := e.do("POST", "/api/services/"+db.ID+"/snapshots", map[string]any{"name": "DB only", "restart": false}, &j); resp.StatusCode != 202 || j.ServiceID != db.ID {
		t.Fatalf("take service: %d %+v", resp.StatusCode, j)
	}
	dbSnap := e.waitJob(j.ID).SnapshotID

	var list []snapshot.Manifest
	e.do("GET", "/api/projects/"+prj.ID+"/snapshots", nil, &list)
	if len(list) != 1 || list[0].ID != snapID || list[0].Name != "Before" || len(list[0].Services) != 2 {
		t.Fatalf("project list: %+v", list)
	}
	e.do("GET", "/api/services/"+db.ID+"/snapshots", nil, &list)
	if len(list) != 1 || list[0].ID != dbSnap || list[0].Scope != "service" {
		t.Fatalf("service list: %+v", list)
	}
	var raw map[string]json.RawMessage
	e.do("GET", "/api/snapshots/"+snapID, nil, &raw)
	if string(raw["missing"]) != "[]" || string(raw["format"]) != "1" || !strings.Contains(string(raw["warnings"]), "could not be resolved") {
		t.Fatalf("detail: %s %s %s", raw["missing"], raw["format"], raw["warnings"])
	}

	// Restore (no body) brings the data and the settings back.
	if err := os.WriteFile(filepath.Join(dataDir, "ibdata1"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.do("PUT", "/api/services/"+db.ID, map[string]any{"name": "Renamed", "kind": "mariadb", "port": 3306}, nil)
	if resp := e.do("POST", "/api/snapshots/"+snapID+"/restore", nil, &j); resp.StatusCode != 202 || j.Type != "snapshot-restore" {
		t.Fatalf("restore: %d %+v", resp.StatusCode, j)
	}
	e.waitJob(j.ID)
	if b, _ := os.ReadFile(filepath.Join(dataDir, "ibdata1")); string(b) != "v1" {
		t.Fatalf("data: %s", b)
	}
	var cur model.Service
	e.do("GET", "/api/services/"+db.ID, nil, &cur)
	if cur.Name != "DB" {
		t.Fatalf("settings not restored: %+v", cur)
	}
	if resp := e.do("POST", "/api/snapshots/"+snapID+"/restore", map[string]any{"restart": true}, &j); resp.StatusCode != 202 {
		t.Fatalf("restore with body: %d", resp.StatusCode)
	}
	e.waitJob(j.ID)

	// Clone.
	e.errCode("POST", "/api/snapshots/"+dbSnap+"/clone", map[string]any{"name": "x"}, 400)
	e.errCode("POST", "/api/snapshots/"+snapID+"/clone", map[string]any{"name": "x", "domainSuffix": "a.b"}, 400)
	if resp := e.do("POST", "/api/snapshots/"+snapID+"/clone", map[string]any{"name": "Shop 2"}, &j); resp.StatusCode != 202 || j.Type != "snapshot-clone" {
		t.Fatalf("clone: %d %+v", resp.StatusCode, j)
	}
	j = e.waitJob(j.ID)
	var clone model.Project
	for _, p := range e.srv.Store.Snapshot().Projects {
		if p.ID == j.ProjectID {
			clone = p
		}
	}
	if clone.Name != "Shop 2" || len(clone.ServiceIDs) != 2 || len(clone.SiteIDs) != 1 {
		t.Fatalf("clone project: %+v", clone)
	}
	var sites []model.SiteView
	e.do("GET", "/api/sites", nil, &sites)
	found := false
	for _, s := range sites {
		found = found || (s.ID == clone.SiteIDs[0] && s.Domain == "app-copy.localhost" && s.ServiceID == clone.ServiceIDs[1])
	}
	if !found {
		t.Fatalf("cloned site: %+v", sites)
	}

	// Delete.
	if resp := e.do("DELETE", "/api/snapshots/"+snapID, nil, nil); resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	e.errCode("GET", "/api/snapshots/"+snapID, nil, 404)
}

func TestSnapshotRunningConflictOverAPI(t *testing.T) {
	e := snapEnv(t)
	sv := helperService(t, e, 0)
	var prj model.Project
	e.do("POST", "/api/projects", map[string]any{"name": "P", "serviceIds": []string{sv.ID}}, &prj)
	e.do("POST", "/api/services/"+sv.ID+"/start", nil, nil)
	waitFor(t, func() bool { return e.sup.State(sv.ID) == model.StateRunning })
	var er struct {
		Error struct {
			Code, Message string
			Details       map[string]string
		}
	}
	if resp := e.do("POST", "/api/projects/"+prj.ID+"/snapshots", map[string]any{"name": "x"}, &er); resp.StatusCode != 409 ||
		er.Error.Code != "conflict" || !strings.Contains(er.Error.Message, "helper is running") || er.Error.Details["running"] != sv.ID {
		t.Fatalf("running: %d %+v", resp.StatusCode, er)
	}
}
