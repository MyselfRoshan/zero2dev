package kinds

import "path/filepath"

// CorepackDir is where Zero2Dev installs Corepack for pnpm and Yarn: Node 26 no longer bundles
// it, so it comes from npm into the data folder, once. Its commands (pnpm, yarn) are made in
// the bin folder below it, which is first on the PATH of a service that uses one of them.
func CorepackDir(home string) string { return filepath.Join(home, "node", "corepack") }

// PackageManagerShim is the command Corepack makes for a package manager (pnpm or yarn) in
// CorepackDir\bin.
func PackageManagerShim(home, pm string, windows bool) string {
	name := pm
	if windows {
		name += ".cmd"
	}
	return filepath.Join(CorepackDir(home), "bin", name)
}

// nodePackageManager adds what a Node service needs when its package manager is pnpm or Yarn:
// the pre-steps that make Corepack and the two commands (both skipped once done), the folder they
// go in, and the program of an npm script run (`pnpm run dev` instead of `npm run dev`). The
// variables and the PATH entry are the "portable" rule of the node profile.
func nodePackageManager(pl *Plan, eff map[string]string, c Ctx) {
	pm := eff["packageManager"]
	if (pm != "pnpm" && pm != "yarn") || c.Home == "" {
		return
	}
	dir := CorepackDir(c.Home)
	bin := filepath.Join(dir, "bin")
	corepack := filepath.Join(dir, "node_modules", ".bin", "corepack")
	if c.Windows {
		corepack += ".cmd"
	}
	pl.Dirs = append(pl.Dirs, bin) // corepack enable needs the folder to exist
	pl.Pre = append(pl.Pre,
		PreStep{
			Label:   "Installing Corepack",
			Program: "npm", // next to the service's node.exe
			Args:    []string{"install", "--prefix", dir, "corepack"},
			Skip:    filepath.Join(dir, "node_modules", "corepack", "dist", "corepack.js"),
			Dir:     dir,
			Timeout: 600,
		},
		PreStep{
			Label:   "Making the pnpm and yarn commands",
			Program: corepack,
			Args:    []string{"enable", "--install-directory", bin, "pnpm", "yarn"},
			Skip:    PackageManagerShim(c.Home, "pnpm", c.Windows),
			Dir:     dir,
			Timeout: 120,
		},
	)
	if pl.Program == "npm" {
		pl.Program = PackageManagerShim(c.Home, pm, c.Windows)
	}
}
