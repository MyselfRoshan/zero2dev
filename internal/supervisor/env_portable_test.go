package supervisor

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestPortableVarsOffAddsNothing(t *testing.T) {
	for _, kind := range []string{"node", "java"} { // (php always has Composer's home: TestPortableVarsComposerAlways)
		a, s := portableVars(`D:\z`, kind, model.Portable{}, nil, []string{"Path=C:\\bin"})
		if len(a) != 0 || len(s) != 0 {
			t.Errorf("%s with everything off: %q %q", kind, a, s)
		}
	}
	// The plain serviceVars (no data folder known) never adds them.
	d := store.Data{Settings: model.Settings{Portable: model.Portable{NpmGlobal: true}}}
	added, _, _, _, err := serviceVars(model.Service{Kind: "node"}, d, nil, nil, true)
	if err != nil || slices.ContainsFunc(added, func(kv string) bool { return strings.HasPrefix(kv, "npm_config_prefix=") }) {
		t.Fatalf("serviceVars without home: %q %v", added, err)
	}
}

func TestPortableVarsNpmGlobal(t *testing.T) {
	home := filepath.Join(t.TempDir(), "data")
	rt := &model.Runtime{Kind: "node", Binary: filepath.Join(home, "bin", "node-22", "node.exe")}
	environ := []string{"A=1", "Path=" + filepath.Join("C:", "Windows")}
	a, s := portableVars(home, "node", model.Portable{NpmGlobal: true}, rt, environ)
	global := filepath.Join(home, "node", "global")
	if v, ok := last(a, "npm_config_prefix"); !ok || v != global {
		t.Fatalf("npm_config_prefix = %q (%v) in %q", v, ok, a)
	}
	sep := string(os.PathListSeparator)
	want := global + sep + filepath.Dir(rt.Binary) + sep + filepath.Join("C:", "Windows")
	if v, ok := last(a, "Path"); !ok || v != want {
		t.Fatalf("Path = %q, want %q (keeps the spelling of the daemon's variable)", v, want)
	}
	if v, _ := last(s, "Path"); !strings.HasPrefix(v, global+sep) || strings.Contains(v, "Windows") {
		t.Fatalf("preview PATH = %q", v)
	}
	// Other kinds are not touched by the npm switch.
	if a, _ := portableVars(home, "php", model.Portable{NpmGlobal: true}, nil, environ); slices.ContainsFunc(a, func(kv string) bool { return strings.HasPrefix(kv, "npm_config_") }) {
		t.Fatalf("php got %q", a)
	}
}

func TestPortableVarsComposerAndGo(t *testing.T) {
	home := t.TempDir()
	a, _ := portableVars(home, "php", model.Portable{Composer: true}, nil, nil)
	if v, _ := last(a, "COMPOSER_HOME"); v != filepath.Join(home, "composer") {
		t.Fatalf("COMPOSER_HOME = %q in %q", v, a)
	}
	if v, _ := last(a, "PATH"); v != filepath.Join(home, "composer", "vendor", "bin") {
		t.Fatalf("PATH = %q", v)
	}
	a, _ = portableVars(home, "go", model.Portable{GoCache: true}, nil, nil)
	if v, _ := last(a, "GOMODCACHE"); v != filepath.Join(home, "go", "path", "pkg", "mod") {
		t.Fatalf("GOMODCACHE = %q", v)
	}
	if v, _ := last(a, "GOCACHE"); v != filepath.Join(home, "go", "cache") {
		t.Fatalf("GOCACHE = %q", v)
	}
	// Go's own tools are on PATH however the switch stands (see TestGoToolsOnPath).
	if v, _ := last(a, "PATH"); v != model.GoToolsDir(home) {
		t.Fatalf("PATH = %q, want the Go tools folder", v)
	}
}

func TestGoToolsOnPath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "data")
	sep := string(os.PathListSeparator)
	rt := &model.Runtime{Kind: "go", Binary: filepath.Join(home, "bin", "go-1.24", "bin", "go.exe")}
	// The daemon's GOPATH points at a folder that does not exist: never the real user's.
	userGo := filepath.Join(t.TempDir(), "usergo")
	environ := []string{"Path=" + filepath.Join("C:", "Windows"), "GOPATH=" + userGo}
	a, s := portableVars(home, "go", model.Portable{}, rt, environ)
	if v, _ := last(a, "GOPATH"); v != filepath.Join(home, "go", "path") {
		t.Fatalf("GOPATH = %q: the data folder's while Go's downloads are kept there", v)
	}
	want := model.GoToolsDir(home) + sep + filepath.Dir(rt.Binary) + sep + filepath.Join("C:", "Windows")
	if v, _ := last(a, "Path"); v != want {
		t.Fatalf("Path = %q, want %q: tools folder, then GOROOT's bin", v, want)
	}
	// With the setting off Go keeps its own GOPATH: no GOPATH from Zero2Dev, and that
	// GOPATH's bin folder (else %USERPROFILE%\go\bin) is on PATH instead.
	off, _ := portableVarsGo(home, "go", model.Portable{}, rt, environ, false)
	if _, ok := last(off, "GOPATH"); ok {
		t.Fatalf("isolate off must not set GOPATH: %q", off)
	}
	wantOff := filepath.Join(userGo, "bin") + sep + filepath.Dir(rt.Binary) + sep + filepath.Join("C:", "Windows")
	if v, _ := last(off, "Path"); v != wantOff {
		t.Fatalf("isolate off: Path = %q, want %q", v, wantOff)
	}
	if v := defaultGoBin([]string{"A=1"}); !strings.HasSuffix(v, filepath.Join("go", "bin")) {
		t.Fatalf("no GOPATH: %q is not <profile>\\go\\bin", v)
	}
	// The setting of a service: on unless it says false.
	if !goIsolated(model.Service{}) || !goIsolated(model.Service{Settings: map[string]string{"isolate": "true"}}) || goIsolated(model.Service{Settings: map[string]string{"isolate": "false"}}) {
		t.Fatal("goIsolated")
	}
	if v, _ := last(s, "Path"); !strings.HasPrefix(v, model.GoToolsDir(home)+sep) {
		t.Fatalf("preview PATH = %q", v)
	}
	// A custom service (air run as a command) gets each tools folder once it exists.
	if a, _ := portableVars(home, "custom", model.Portable{}, nil, environ); len(a) != 0 {
		t.Fatalf("no tools folder yet: %q", a)
	}
	if err := os.MkdirAll(filepath.Join(userGo, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ = portableVars(home, "custom", model.Portable{}, nil, environ)
	if v, _ := last(a, "Path"); !strings.HasPrefix(v, filepath.Join(userGo, "bin")+sep) {
		t.Fatalf("Go's default folder exists: Path = %q", v)
	}
	if err := os.MkdirAll(model.GoToolsDir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ = portableVars(home, "custom", model.Portable{}, nil, environ)
	if v, _ := last(a, "Path"); !strings.HasPrefix(v, model.GoToolsDir(home)+sep) {
		t.Fatalf("custom Path = %q", v)
	}
	if _, ok := last(a, "GOPATH"); ok {
		t.Fatal("a custom service does not get GOPATH")
	}
}

func TestServiceVarsWithHomeOrderUserWins(t *testing.T) {
	s, home, _ := envSetup(t)
	d := s.st.Snapshot()
	d.Settings.Portable.Composer = true
	svc := model.Service{Kind: "php", Port: 8000, Env: map[string]string{"COMPOSER_HOME": "mine"}}
	added, shown, _, _, err := s.serviceVarsWithHome(svc, d, nil, []string{"X=1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := last(added, "COMPOSER_HOME"); v != "mine" {
		t.Fatalf("the user's own variable must win: %q", added)
	}
	i := slices.Index(added, "COMPOSER_HOME="+filepath.Join(home, "composer"))
	if i < 0 || slices.Index(added, "X=1") > i {
		t.Fatalf("portable folders come after the kind's variables: %q", added)
	}
	if !slices.Contains(shown, "COMPOSER_HOME="+filepath.Join(home, "composer")) {
		t.Fatalf("preview: %q", shown)
	}
}
