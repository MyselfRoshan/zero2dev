package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKindCategories(t *testing.T) {
	for _, k := range RuntimeKinds {
		if k == "other" {
			continue
		}
		if KindCategory[k] == "" {
			t.Errorf("runtime kind %s has no category", k)
		}
	}
	for k := range KindCategory {
		if !In(k, RuntimeKinds) {
			t.Errorf("category for unknown kind %s", k)
		}
	}
	for _, k := range ServiceKinds {
		if Launchable(k) {
			t.Errorf("service kind %s must not be a launch-only program", k)
		}
	}
	if !Launchable("vscode") || !Launchable("dbeaver") || Launchable("mysql") || !IsDatabase("postgres") || IsDatabase("node") {
		t.Fatal("Launchable / IsDatabase")
	}
	// Mailpit is a service of its own category: not a database (no dumps, no
	// clean stop), not a launch-only program, and its versions are fetched.
	if KindCategory["mailpit"] != CatMail || IsDatabase("mailpit") || Launchable("mailpit") ||
		!In("mailpit", ServiceKinds) || !In("mailpit", RemoteKinds) || DataDirName("mailpit") != "mailpit" {
		t.Fatal("mailpit kind lists")
	}
}

func TestDataDirNames(t *testing.T) {
	want := map[string]string{"vscode": "code", "vscodium": "codium", "notepadpp": "notepadpp", "postgres": "postgres", "node": "node", "php": "php", "java": ""}
	for k, v := range want {
		if DataDirName(k) != v {
			t.Errorf("%s: %q, want %q", k, DataDirName(k), v)
		}
	}
	for k := range KindCategory {
		if (Launchable(k) || IsDatabase(k)) && DataDirName(k) == "" {
			t.Errorf("%s keeps data but has no data folder name", k)
		}
	}
}

func TestDeps(t *testing.T) {
	sv := Service{DependsOn: []string{"a", "b", "a", ""}, DatabaseID: "db"}
	if got := strings.Join(Deps(sv), ","); got != "a,b,db" {
		t.Fatalf("Deps = %s", got)
	}
	sv = Service{DependsOn: []string{"db"}, DatabaseID: "db"}
	if got := Deps(sv); len(got) != 1 || got[0] != "db" {
		t.Fatalf("db listed twice: %v", got)
	}
	if got := Deps(Service{}); got == nil || len(got) != 0 {
		t.Fatalf("no deps must be an empty list: %#v", got)
	}
	// the input is not modified
	in := []string{"x"}
	Deps(Service{DependsOn: in, DatabaseID: "y"})
	if len(in) != 1 || cap(in) > 1 && in[:2][1] == "y" {
		t.Fatal("Deps wrote into DependsOn")
	}
}

func TestRuntimeHome(t *testing.T) {
	jdk := filepath.Join("data", "bin", "java", "jdk-21")
	cases := []struct {
		rt         Runtime
		name, home string
	}{
		{Runtime{Kind: "java", Binary: filepath.Join(jdk, "bin", "java.exe")}, "JAVA_HOME", jdk},
		{Runtime{Kind: "go", Binary: filepath.Join("g", "go", "bin", "go.exe")}, "GOROOT", filepath.Join("g", "go")},
		{Runtime{Kind: "node", Binary: filepath.Join("n", "node.exe")}, "", ""},
		{Runtime{Kind: "java"}, "", ""},
	}
	for _, c := range cases {
		if n, h := RuntimeHome(c.rt); n != c.name || h != c.home {
			t.Errorf("%s %s: got %q %q, want %q %q", c.rt.Kind, c.rt.Binary, n, h, c.name, c.home)
		}
	}
}

func TestProjectAndSettingsJSON(t *testing.T) {
	b, _ := json.Marshal(Project{ID: "prj_1", Name: "P", ServiceIDs: []string{"a"}, SiteIDs: []string{}})
	if string(b) != `{"id":"prj_1","name":"P","serviceIds":["a"],"siteIds":[]}` {
		t.Fatal(string(b))
	}
	b, _ = json.Marshal(Settings{})
	if strings.Contains(string(b), "registerPath") {
		t.Fatal("the single PATH switch is gone, items are chosen one by one: " + string(b))
	}
	b, _ = json.Marshal(EnvConfig{Path: []EnvPath{{ID: "envp_1", Source: EnvCustom, Dir: `C:\t`, Enabled: true}}, Vars: []EnvVar{}})
	if string(b) != `{"seeded":false,"path":[{"id":"envp_1","source":"custom","dir":"C:\\t","enabled":true}],"vars":[]}` {
		t.Fatal(string(b))
	}
}

func TestDefaultGoBin(t *testing.T) {
	sep := string(os.PathListSeparator)
	for _, c := range []struct{ gopath, home, want string }{
		{"", filepath.Join("C:", "Users", "me"), filepath.Join("C:", "Users", "me", "go", "bin")},
		{filepath.Join("D:", "gp") + sep + filepath.Join("E:", "other"), "C:", filepath.Join("D:", "gp", "bin")},
		{"  ", "", ""},
		{"", "", ""},
	} {
		if got := DefaultGoBin(c.gopath, c.home); got != c.want {
			t.Errorf("DefaultGoBin(%q, %q) = %q, want %q", c.gopath, c.home, got, c.want)
		}
	}
	home := filepath.Join("D:", "z")
	if GoPath(home) != filepath.Join(home, "go", "path") || GoToolsDir(home) != filepath.Join(home, "go", "path", "bin") {
		t.Fatal("GoPath / GoToolsDir")
	}
}
