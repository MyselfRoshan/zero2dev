package kinds

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestNodeNpmIsTheDefaultAndNeedsNoCorepack(t *testing.T) {
	c := ctx()
	pl := Build("node", map[string]string{"run": "npm", "npmScript": "dev"}, []string{"--x"}, c)
	if pl.Program != "npm" || len(pl.Pre) != 0 || !slices.Equal(pl.Args, []string{"run", "dev", "--", "--x"}) {
		t.Fatalf("%+v", pl)
	}
	if err := Validate("node", map[string]string{"packageManager": "bun"}); err == nil {
		t.Error("an unknown package manager must be refused")
	}
}

func TestNodePnpmAndYarnMakeCorepackOnce(t *testing.T) {
	c := ctx()
	dir := filepath.Join(c.Home, "node", "corepack")
	for _, pm := range []string{"pnpm", "yarn"} {
		if err := Validate("node", map[string]string{"run": "npm", "packageManager": pm}); err != nil {
			t.Fatal(err)
		}
		pl := Build("node", map[string]string{"run": "npm", "npmScript": "dev", "packageManager": pm}, []string{"--x"}, c)
		shim := filepath.Join(dir, "bin", pm+".cmd")
		// The program is the Corepack command (it does not exist yet at the first start), and npm's "--" is not added.
		if pl.Program != shim || !slices.Equal(pl.Args, []string{"run", "dev", "--x"}) {
			t.Fatalf("%s: program %q args %q", pm, pl.Program, pl.Args)
		}
		if !slices.Contains(pl.Dirs, filepath.Join(dir, "bin")) || len(pl.Pre) != 2 {
			t.Fatalf("%s: dirs %q pre %+v", pm, pl.Dirs, pl.Pre)
		}
		in, en := pl.Pre[0], pl.Pre[1]
		if in.Program != "npm" || !slices.Equal(in.Args, []string{"install", "--prefix", dir, "corepack"}) ||
			in.Skip != filepath.Join(dir, "node_modules", "corepack", "dist", "corepack.js") {
			t.Errorf("install step %+v", in)
		}
		if en.Program != filepath.Join(dir, "node_modules", ".bin", "corepack.cmd") ||
			!slices.Equal(en.Args, []string{"enable", "--install-directory", filepath.Join(dir, "bin"), "pnpm", "yarn"}) ||
			en.Skip != filepath.Join(dir, "bin", "pnpm.cmd") {
			t.Errorf("enable step %+v", en)
		}
	}
	// A script file run with node directly keeps node as the program: only the tools are made.
	pl := Build("node", map[string]string{"run": "script", "script": "server.js", "packageManager": "pnpm"}, nil, c)
	if pl.Program != "" || len(pl.Pre) != 2 {
		t.Fatalf("%+v", pl)
	}
	// Not on Windows: the commands have no .cmd.
	c.Windows = false
	pl = Build("node", map[string]string{"packageManager": "yarn"}, nil, c)
	if pl.Pre[1].Program != filepath.Join(dir, "node_modules", ".bin", "corepack") || pl.Pre[1].Skip != filepath.Join(dir, "bin", "pnpm") {
		t.Fatalf("%+v", pl.Pre)
	}
}
