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
	for _, kind := range []string{"node", "php", "go"} {
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
	if a, _ := portableVars(home, "php", model.Portable{NpmGlobal: true}, nil, environ); len(a) != 0 {
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
	if _, ok := last(a, "PATH"); ok {
		t.Fatal("go adds no PATH entry")
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
