package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"zero2dev/internal/apperr"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestComposerCommandUsesTheServicesPHP(t *testing.T) {
	s, home, _ := envSetup(t)
	php := func(name string) string {
		p := filepath.Join(home, "bin", name, "php.exe")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o755)
		return p
	}
	php83, php84 := php("php-8.3"), php("php-8.4")
	svc := model.Service{ID: "w", Name: "Web", Kind: "php", RuntimeID: "rt_83"}
	s.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes,
			model.Runtime{ID: "rt_83", Kind: "php", Binary: php83},
			model.Runtime{ID: "rt_84", Kind: "php", Binary: php84, Default: true})
		return nil
	})

	// No Composer yet: a 409 that says where to get it.
	_, _, err := s.ComposerCommand(svc)
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Status != 409 || ae.Message != "Composer is not installed: install it on the Runtimes page" {
		t.Fatalf("err = %v", err)
	}

	// Registered, but the phar is gone: still not installed.
	bat := filepath.Join(home, "bin", "composer-2.11.1", "composer.bat")
	s.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_c", Kind: "composer", Binary: bat, Default: true})
		return nil
	})
	if _, _, err := s.ComposerCommand(svc); err == nil {
		t.Fatal("a missing phar means Composer is not installed")
	}

	os.MkdirAll(filepath.Dir(bat), 0o755)
	os.WriteFile(filepath.Join(filepath.Dir(bat), "composer.phar"), []byte("x"), 0o644)
	bin, args, err := s.ComposerCommand(svc)
	if err != nil || bin != php83 || !slices.Equal(args, []string{filepath.Join(filepath.Dir(bat), "composer.phar")}) {
		t.Fatalf("service's own PHP: %q %q %v", bin, args, err)
	}
	// Without a PHP of its own the default PHP runs it; a service of another kind too.
	svc.RuntimeID = ""
	if bin, _, err = s.ComposerCommand(svc); err != nil || bin != php84 {
		t.Fatalf("default PHP: %q %v", bin, err)
	}
	if bin, _, err = s.ComposerCommand(model.Service{ID: "c", Kind: "custom"}); err != nil || bin != php84 {
		t.Fatalf("custom service: %q %v", bin, err)
	}
}

func TestPortableVarsComposerAlways(t *testing.T) {
	home := t.TempDir()
	a, s := portableVars(home, "php", model.Portable{}, nil, []string{"Path=C:\bin"})
	if v, _ := last(a, "COMPOSER_HOME"); v != filepath.Join(home, "composer") {
		t.Fatalf("COMPOSER_HOME = %q in %q", v, a)
	}
	if v, _ := last(a, "COMPOSER_CACHE_DIR"); v != filepath.Join(home, "composer", "cache") {
		t.Fatalf("COMPOSER_CACHE_DIR = %q", v)
	}
	if _, ok := last(a, "Path"); ok || len(s) != len(a) {
		t.Fatalf("with the switch off the PATH is left alone: %q", a)
	}
	// Open terminal and a start get it through the service's environment.
	svc := model.Service{ID: "w", Name: "Web", Kind: "php"}
	srv, _, _ := envSetup(t)
	env, err := srv.Environ(svc)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := last(env, "COMPOSER_HOME"); !ok || !strings.HasSuffix(filepath.ToSlash(v), "/composer") {
		t.Fatalf("Environ: COMPOSER_HOME = %q", v)
	}
}

func TestPortableVarsPackageManager(t *testing.T) {
	home := t.TempDir()
	rt := &model.Runtime{Kind: "node", Binary: filepath.Join(home, "bin", "node-26", "node.exe")}
	environ := []string{"Path=" + filepath.Join("C:", "Windows")}
	sep := string(os.PathListSeparator)
	for _, settings := range []map[string]string{nil, {"packageManager": "npm"}} {
		if a, _ := portableVarsSvc(home, "node", model.Portable{}, rt, environ, true, settings); len(a) != 0 {
			t.Fatalf("npm adds nothing: %q", a)
		}
	}
	for _, pm := range []string{"pnpm", "yarn"} {
		a, shown := portableVarsSvc(home, "node", model.Portable{}, rt, environ, true, map[string]string{"packageManager": pm})
		for name, want := range map[string]string{
			"COREPACK_HOME":                   filepath.Join(home, "node", "corepack-home"),
			"COREPACK_ENABLE_DOWNLOAD_PROMPT": "0",
			"npm_config_store_dir":            filepath.Join(home, "node", "pnpm-store"),
			"YARN_CACHE_FOLDER":               filepath.Join(home, "node", "yarn-cache"),
		} {
			if v, ok := last(a, name); !ok || v != want {
				t.Errorf("%s %s = %q, want %q", pm, name, v, want)
			}
		}
		bin := filepath.Join(home, "node", "corepack", "bin")
		if v, _ := last(a, "Path"); v != bin+sep+filepath.Dir(rt.Binary)+sep+filepath.Join("C:", "Windows") {
			t.Errorf("%s: Path = %q (Corepack's commands come before node's folder)", pm, v)
		}
		if v, _ := last(shown, "Path"); !strings.HasPrefix(v, bin+sep) {
			t.Errorf("preview PATH = %q", v)
		}
	}
	// A service whose arguments the user wrote does not use the setting.
	if got := ruleSettings(model.Service{CustomArgs: true, Settings: map[string]string{"packageManager": "pnpm"}}); got != nil {
		t.Errorf("custom args: %v", got)
	}
}

func TestPnpmServicePreparesCorepackWithAFakeRunner(t *testing.T) {
	s, calls := newPreSup(t)
	home := s.home
	node := filepath.Join(home, "bin", "node-26", "node.exe")
	npm := filepath.Join(filepath.Dir(node), "npm.cmd")
	if runtime.GOOS != "windows" {
		node, npm = filepath.Join(home, "bin", "node-26", "node"), filepath.Join(home, "bin", "node-26", "npm")
	}
	os.MkdirAll(filepath.Dir(node), 0o755)
	for _, p := range []string{node, npm} {
		os.WriteFile(p, []byte("x"), 0o755)
	}
	cwd := filepath.Join(home, "app")
	os.MkdirAll(cwd, 0o755)
	svc := model.Service{ID: "svc", Name: "App", Kind: "node", Cwd: cwd, Port: 3000,
		Settings: map[string]string{"run": "npm", "npmScript": "dev", "packageManager": "pnpm"}}
	s.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_node", Kind: "node", Binary: node, Default: true})
		return nil
	})

	// A start (not lenient) must not refuse the program Corepack has not made yet.
	r, err := s.resolveWith(svc, s.st.Snapshot(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(filepath.ToSlash(r.bin), "node/corepack/bin/pnpm"+map[bool]string{true: ".cmd", false: ""}[runtime.GOOS == "windows"]) {
		t.Fatalf("program %q", r.bin)
	}
	if v, _ := last(r.env, "COREPACK_HOME"); v != filepath.Join(home, "node", "corepack-home") {
		t.Fatalf("COREPACK_HOME = %q", v)
	}
	if err := s.PrepareService(svc); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || filepath.Dir((*calls)[0].bin) != filepath.Dir(node) || (*calls)[0].args[0] != "install" || (*calls)[1].args[0] != "enable" {
		t.Fatalf("calls %+v", *calls)
	}
	if st, err := os.Stat(filepath.Join(home, "node", "corepack", "bin")); err != nil || !st.IsDir() {
		t.Fatalf("the install folder must exist before corepack enable: %v", err)
	}

	// Once Corepack and its commands exist, nothing runs again.
	*calls = nil
	for _, p := range []string{
		filepath.Join(home, "node", "corepack", "node_modules", "corepack", "dist", "corepack.js"),
		kindsShim(home),
	} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	if err := s.PrepareService(svc); err != nil || len(*calls) != 0 {
		t.Fatalf("%v %+v", err, *calls)
	}
}

func kindsShim(home string) string {
	return kinds.PackageManagerShim(home, "pnpm", runtime.GOOS == "windows")
}
