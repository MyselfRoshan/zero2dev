package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"zero2dev/internal/jobs"
	"zero2dev/internal/migrate"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// migrateEnv gives the server a Bring in manager on temp folders only.
func migrateEnv(t *testing.T) (*tenv, *migrate.Manager) {
	e := newEnv(t)
	root := t.TempDir()
	e.srv.Jobs = jobs.New(e.hub)
	m := &migrate.Manager{Home: e.home, Bin: filepath.Join(e.home, "bin"), St: e.srv.Store, Jobs: e.srv.Jobs, Emit: e.hub.Publish,
		Env:     migrate.Env{AppData: filepath.Join(root, "Roaming"), LocalAppData: filepath.Join(root, "Local"), UserProfile: filepath.Join(root, "me")},
		Running: func(string) bool { return false }}
	e.srv.Migrate = m
	return e, m
}

func waitJobAPI(e *tenv, id string) model.Job {
	e.t.Helper()
	var j model.Job
	for range 400 {
		e.do("GET", "/api/jobs/"+id, nil, &j)
		if j.State != "running" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("job %s still running", id)
	return j
}

func TestMigrationRoutes(t *testing.T) {
	e, m := migrateEnv(t)
	var sources []migrate.Source
	if resp := e.do("GET", "/api/migrations/sources", nil, &sources); resp.StatusCode != 200 || len(sources) < 10 {
		t.Fatalf("sources %d %d", resp.StatusCode, len(sources))
	}
	var list []migrate.Migration
	if e.do("GET", "/api/migrations", nil, &list); list == nil || len(list) != 0 {
		t.Fatalf("empty list must be []: %v", list)
	}
	e.errCode("POST", "/api/migrations", map[string]any{"source": "nope", "mode": "copy"}, 404)
	e.errCode("POST", "/api/migrations", map[string]any{"source": "composer", "mode": "x"}, 400)
	e.errCode("POST", "/api/migrations", map[string]any{"source": "database", "mode": "move"}, 400)
	e.errCode("POST", "/api/migrations", map[string]any{"source": "vscode", "mode": "copy"}, 409) // not installed in Zero2Dev
	e.errCode("POST", "/api/migrations/mig_00000000/revert", map[string]any{"how": "discard"}, 404)

	// A registered runtime is copy-only.
	bin := filepath.Join(t.TempDir(), "nodejs", "node.exe")
	os.MkdirAll(filepath.Dir(bin), 0o755)
	os.WriteFile(bin, []byte("x"), 0o755)
	e.srv.Store.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_n", Kind: "node", Version: "22", Source: "registered", Binary: bin})
		return nil
	})
	e.errCode("POST", "/api/migrations", map[string]any{"source": "runtime:rt_n", "mode": "move"}, 400)

	// Copy Composer, then revert it.
	os.MkdirAll(filepath.Join(m.Env.AppData, "Composer"), 0o755)
	os.WriteFile(filepath.Join(m.Env.AppData, "Composer", "auth.json"), []byte("{}"), 0o644)
	var j model.Job
	if resp := e.do("POST", "/api/migrations", map[string]any{"source": "composer", "mode": "copy"}, &j); resp.StatusCode != 202 || j.Type != model.JobMigrate || j.MigrationID == "" {
		t.Fatalf("start %d %+v", resp.StatusCode, j)
	}
	if got := waitJobAPI(e, j.ID); got.State != "done" {
		t.Fatalf("job %+v", got)
	}
	if !e.srv.Store.Snapshot().Settings.Portable.Composer {
		t.Fatal("composer switch on")
	}
	e.do("GET", "/api/migrations", nil, &list)
	if len(list) != 1 || list[0].State != "done" || list[0].ID != j.MigrationID {
		t.Fatalf("list %+v", list)
	}
	e.errCode("POST", "/api/migrations/"+j.MigrationID+"/revert", map[string]any{"how": "move-back"}, 400)
	var rj model.Job
	if resp := e.do("POST", "/api/migrations/"+j.MigrationID+"/revert", map[string]any{"how": "discard"}, &rj); resp.StatusCode != 202 || rj.Type != model.JobMigrateRevert {
		t.Fatalf("revert %d %+v", resp.StatusCode, rj)
	}
	if got := waitJobAPI(e, rj.ID); got.State != "done" {
		t.Fatalf("revert job %+v", got)
	}
	time.Sleep(20 * time.Millisecond)
	e.do("GET", "/api/migrations", nil, &list)
	if list[0].State != "reverted" || e.srv.Store.Snapshot().Settings.Portable.Composer {
		t.Fatalf("after revert %+v", list[0])
	}
	e.errCode("POST", "/api/migrations/"+j.MigrationID+"/revert", map[string]any{"how": "discard"}, 409)
	e.errCode("POST", "/api/migrations/databases", map[string]any{"kind": "redis", "host": "h", "port": 1, "user": "u"}, 501) // no database tools
}

func TestMigrationRoutesWithoutManager(t *testing.T) {
	e := newEnv(t)
	e.errCode("GET", "/api/migrations/sources", nil, 501)
}
