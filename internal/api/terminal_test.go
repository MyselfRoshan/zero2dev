package api

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"zero2dev/internal/model"
)

// The terminal gets the service's resolved environment (database variables,
// .env file, own variables) and its folder; a database without a folder gets
// the home folder.
func TestTerminalEnvironmentAndFolder(t *testing.T) {
	e := newEnv(t)
	var pg model.Service
	if resp := e.do("POST", "/api/services", map[string]any{"name": "pg", "kind": "postgres", "port": 54329}, &pg); resp.StatusCode != 201 {
		t.Fatalf("create postgres: %d", resp.StatusCode)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FROM_FILE=hello\nexport QUOTED=\"a b\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var app model.Service
	body := svcBody("app", dir, map[string]any{"databaseId": pg.ID, "envFile": ".env", "env": map[string]string{"OWN": "1"}})
	if resp := e.do("POST", "/api/services", body, &app); resp.StatusCode != 201 {
		t.Fatalf("create app: %d", resp.StatusCode)
	}

	var gotDir string
	var gotEnv []string
	e.srv.Terminal = func(d string, en []string) error { gotDir, gotEnv = d, en; return nil }
	if resp := e.do("POST", "/api/services/"+app.ID+"/terminal", nil, nil); resp.StatusCode != 204 {
		t.Fatalf("terminal: %d", resp.StatusCode)
	}
	if gotDir != app.Cwd {
		t.Errorf("dir %q, want %q", gotDir, app.Cwd)
	}
	for _, want := range []string{"FROM_FILE=hello", "QUOTED=a b", "OWN=1", "DB_CONNECTION=pgsql", "DB_HOST=127.0.0.1", "DB_PORT=" + strconv.Itoa(pg.Port)} {
		if !containsEnv(gotEnv, want) {
			t.Errorf("terminal environment lacks %s", want)
		}
	}

	// a database has no folder: the home folder is used
	if resp := e.do("POST", "/api/services/"+pg.ID+"/terminal", nil, nil); resp.StatusCode != 204 {
		t.Fatalf("terminal for the database: %d", resp.StatusCode)
	}
	if gotDir != e.home {
		t.Errorf("database terminal dir %q, want home %q", gotDir, e.home)
	}
}
