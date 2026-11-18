package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
)

func TestStoreRoundTripAndAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot().Settings.OpenBrowserOnStart || s.Snapshot().Settings.ProxyHTTPPort != 8080 {
		t.Fatal("defaults missing")
	}
	err = s.Update(func(d *Data) error {
		d.Services = append(d.Services, model.Service{ID: "svc_1", Name: "a", Port: 3000})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// no temp files left behind
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("expected only state.json, got %v", ents)
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	sv := s2.Snapshot().Services
	if len(sv) != 1 || sv[0].ID != "svc_1" || sv[0].Args == nil || sv[0].Env == nil || sv[0].State != "stopped" {
		t.Fatalf("bad reload: %+v", sv)
	}
}

func TestUpdateRollsBackOnError(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	boom := errors.New("boom")
	err := s.Update(func(d *Data) error {
		d.Sites = append(d.Sites, model.Site{ID: "x"})
		return boom
	})
	if !errors.Is(err, boom) || len(s.Snapshot().Sites) != 0 {
		t.Fatal("failed update must not apply")
	}
}

func TestCorruptFileIsError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(p, []byte("{not json"), 0o600)
	if _, err := Open(p); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("want corrupt error, got %v", err)
	}
}

func TestRebasePath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "new")
	want := filepath.Join(home, "node-22", "bin", "node")
	for _, in := range []string{
		`C:\Users\me\.zero2dev\runtimes\node-22\bin\node`,
		"/old/place/data/runtimes/node-22/bin/node",
		`D:\z\bin\data\runtimes\node-22/bin/node`, // mixed separators
	} {
		if got := RebasePath(home, in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	if got := RebasePath(home, "/usr/bin/node"); got != "/usr/bin/node" {
		t.Errorf("no runtimes segment must be left alone: %q", got)
	}
	// "runtimes" appearing earlier in the path: first segment wins when it resolves,
	// otherwise the last one is tried.
	os.MkdirAll(filepath.Join(home, "x"), 0o755)
	os.WriteFile(filepath.Join(home, "x", "bin"), []byte("b"), 0o755)
	if got := RebasePath(home, "/srv/runtimes/app/data/runtimes/x/bin"); got != filepath.Join(home, "x", "bin") {
		t.Errorf("nested runtimes segment: %q", got)
	}
}

func TestRebaseRuntimesOnlyTouchesMissingInstalled(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	os.MkdirAll(filepath.Join(home, "bin", "a"), 0o755)
	existing := filepath.Join(home, "bin", "a", "bin")
	os.WriteFile(existing, []byte("x"), 0o755)
	outside := filepath.Join(dir, "runtimes", "registered") // path contains "runtimes" but is registered
	s, _ := Open(filepath.Join(home, "state.json"))
	s.Update(func(d *Data) error {
		d.Runtimes = []model.Runtime{
			{ID: "1", Source: "installed", Binary: existing},
			{ID: "2", Source: "installed", Binary: `C:\old\runtimes\b\bin.exe`},
			{ID: "3", Source: "registered", Binary: outside},
		}
		return nil
	})
	n, err := s.RebaseRuntimes(filepath.Join(home, "bin"))
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	rs := s.Snapshot().Runtimes
	if rs[0].Binary != existing || rs[1].Binary != filepath.Join(home, "bin", "b", "bin.exe") || rs[2].Binary != outside {
		t.Fatalf("%+v", rs)
	}
}

func TestMigrateServicesFromBeforeSettings(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	old := `{"services":[
		{"id":"svc_a","name":"API","kind":"node","args":["server.js"],"cwd":"/w","env":{}},
		{"id":"svc_b","name":"DB","kind":"mariadb","args":[],"cwd":"/w","env":{}},
		{"id":"svc_c","name":"Tool","kind":"custom","command":"x","args":["-v"],"cwd":"/w","env":{}},
		{"id":"svc_d","name":"New","kind":"node","args":["--x"],"settings":{"script":"a.js"},"cwd":"/w","env":{}}
	],"runtimes":[],"sites":[],"settings":{}}`
	if err := os.WriteFile(p, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]model.Service{}
	for _, sv := range s.Snapshot().Services {
		got[sv.ID] = sv
	}
	if !got["svc_a"].CustomArgs {
		t.Error("a service with args and no settings keeps its args as the whole command line")
	}
	if got["svc_b"].CustomArgs || got["svc_b"].Settings == nil {
		t.Errorf("no args: guided settings with defaults, got %+v", got["svc_b"])
	}
	if got["svc_c"].CustomArgs {
		t.Error("custom commands never use customArgs")
	}
	if got["svc_d"].CustomArgs {
		t.Error("a service that has settings is not migrated")
	}
}

// A state.json from 0.0.3 has none of the 0.0.4 fields; it must load with
// sensible defaults and no nil slices.
func TestOldStateFixtureLoads(t *testing.T) {
	const old = `{
  "services": [
    {"id": "svc_node", "name": "Api", "kind": "node", "command": "", "args": [], "settings": {"script": "server.js", "envFile": ".env.local"},
     "customArgs": false, "cwd": "C:/work/api", "env": {}, "port": 3000, "healthPath": "", "runtimeId": "", "autostart": true,
     "state": "running", "pid": 42, "startedAt": null, "lastError": "old"},
    {"id": "svc_pg", "name": "Postgres", "kind": "postgres", "command": "", "args": [], "settings": {}, "customArgs": false,
     "cwd": "", "env": {}, "port": 5432, "healthPath": "", "runtimeId": "", "autostart": false, "state": "stopped", "pid": 0, "startedAt": null, "lastError": ""}
  ],
  "runtimes": [],
  "sites": [{"id": "site_1", "name": "api", "domain": "api.localhost", "type": "proxy", "root": "", "serviceId": "svc_node", "upstreamPort": 0, "https": false, "enabled": true}],
  "settings": {"apiPort": 0, "homeDir": "", "domainSuffix": "localhost", "proxyHttpPort": 8080, "proxyHttpsPort": 8443,
               "openBrowserOnStart": true, "trustLocalCA": false, "downloadProxy": "", "siteServer": "caddy"}
}`
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	d := s.Snapshot()
	if d.Projects == nil || len(d.Projects) != 0 {
		t.Fatalf("projects must be an empty list, got %#v", d.Projects)
	}
	api := d.Services[0]
	if api.DependsOn == nil || len(api.DependsOn) != 0 || api.DatabaseID != "" || api.RestartOnCrash || api.RestartLimit != model.DefaultRestartLimit || api.Restarts != 0 {
		t.Fatalf("new service fields: %+v", api)
	}
	if api.State != model.StateStopped || api.Pid != 0 || api.LastError != "" {
		t.Fatalf("runtime fields must be reset: %+v", api)
	}
	if api.EnvFile != ".env.local" {
		t.Fatalf("node envFile setting not moved: %q", api.EnvFile)
	}
	if _, left := api.Settings["envFile"]; left || api.Settings["script"] != "server.js" {
		t.Fatalf("settings after migration: %v", api.Settings)
	}
	// the suggested environment items exist and are all off
	if !d.Env.Seeded || len(d.Env.Path) != 1 || d.Env.Path[0].Source != model.EnvShims || len(d.Env.Vars) != 2 {
		t.Fatalf("env after migration: %+v", d.Env)
	}
	for _, p := range d.Env.Path {
		if p.Enabled {
			t.Fatalf("nothing may be on by default: %+v", p)
		}
	}
	for _, v := range d.Env.Vars {
		if v.Enabled || v.Source != model.EnvDefault {
			t.Fatalf("nothing may be on by default: %+v", v)
		}
	}
	// round trip through Update keeps the migrated shape
	if err := s.Update(func(*Data) error { return nil }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	for _, want := range []string{`"projects": []`, `"dependsOn": []`, `"envFile": ".env.local"`, `"restartLimit": 5`, `"seeded": true`, `"JAVA_HOME"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("saved state lacks %s", want)
		}
	}
}

func TestNormalizePrunesDanglingLinks(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	err := s.Update(func(d *Data) error {
		d.Services = []model.Service{
			{ID: "pg", Name: "pg", Kind: "postgres"},
			{ID: "web", Name: "web", Kind: "node", DependsOn: []string{"pg", "gone", "web", "pg"}, DatabaseID: "pg", RestartLimit: 99},
			{ID: "bad1", Name: "bad1", Kind: "node", DatabaseID: "web"},     // not a database
			{ID: "bad2", Name: "bad2", Kind: "node", DatabaseID: "missing"}, // does not exist
			{ID: "bad3", Name: "bad3", Kind: "node", RestartLimit: -4},
		}
		d.Sites = []model.Site{{ID: "site_a"}}
		d.Projects = []model.Project{{ID: "prj_1", Name: "P", ServiceIDs: []string{"web", "gone", "web"}, SiteIDs: []string{"site_a", "site_gone"}}, {ID: "prj_2"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	d := s.Snapshot()
	web := d.Services[1]
	if got := strings.Join(web.DependsOn, ","); got != "pg" || web.DatabaseID != "pg" || web.RestartLimit != model.MaxRestartLimit {
		t.Fatalf("web: %+v", web)
	}
	if d.Services[2].DatabaseID != "" || d.Services[3].DatabaseID != "" || d.Services[4].RestartLimit != model.DefaultRestartLimit {
		t.Fatalf("bad links kept: %+v", d.Services[2:])
	}
	p := d.Projects[0]
	if strings.Join(p.ServiceIDs, ",") != "web" || strings.Join(p.SiteIDs, ",") != "site_a" {
		t.Fatalf("project not pruned: %+v", p)
	}
	if d.Projects[1].ServiceIDs == nil || d.Projects[1].SiteIDs == nil {
		t.Fatal("project id lists must never be nil")
	}
	// deleting a service removes it from the links on the next update
	if err := s.Update(func(d *Data) error { d.Services = d.Services[1:]; return nil }); err != nil {
		t.Fatal(err)
	}
	d = s.Snapshot()
	if len(d.Services[0].DependsOn) != 0 || d.Services[0].DatabaseID != "" {
		t.Fatalf("links to a deleted service survive: %+v", d.Services[0])
	}
}

func TestNodeEnvFileSettingKeepsExistingEnvFile(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	s.Update(func(d *Data) error {
		d.Services = []model.Service{{ID: "a", Kind: "node", EnvFile: ".env", Settings: map[string]string{"envFile": ".other"}},
			{ID: "b", Kind: "custom", Settings: map[string]string{"envFile": "stays"}}}
		return nil
	})
	d := s.Snapshot()
	if d.Services[0].EnvFile != ".env" || len(d.Services[0].Settings) != 0 || d.Services[1].Settings["envFile"] != "stays" {
		t.Fatalf("%+v", d.Services)
	}
}
