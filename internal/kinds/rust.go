package kinds

import (
	"path/filepath"
	"regexp"
)

// A Rust service builds into a Cargo target folder of its own, <home>\rust\target\<serviceId>, kept
// out of the project folder and apart from every other service. The runtime's folders
// (RUSTUP_HOME, CARGO_HOME) come from model.RuntimeVars and its cargo\bin folder is put on PATH
// with the runtime's program, so rustc and rustup are found next to cargo.

// TargetDir is the CARGO_TARGET_DIR of a service.
func TargetDir(home, serviceID string) string {
	return filepath.Join(home, "rust", "target", serviceID)
}

// rustPlan sets CARGO_TARGET_DIR and has the folder made before the start. Arguments the user
// wrote after the options come after "--" (see Build).
func rustPlan(pl *Plan, c Ctx) {
	dir := TargetDir(c.Home, c.ServiceID)
	pl.Env = append(pl.Env, "CARGO_TARGET_DIR="+dir)
	pl.Dirs = append(pl.Dirs, dir)
}

// MinGWHint is the extra log line for a build that failed for want of a C compiler or linker.
const MinGWHint = "A crate that needs a C compiler needs MinGW (the GNU toolchain has no C compiler of its own): install MinGW-w64 or MSYS2 and put its bin folder on PATH, then start the service again."

var missingCompiler = regexp.MustCompile("(?i)linker `[^`]+` not found|failed to find tool|is `(gcc|cc|clang|g\\+\\+)` installed|error calling dlltool")

// RustHint returns MinGWHint when a line of cargo's output says a C compiler, linker or
// dlltool is missing, else "".
func RustHint(line string) string {
	if missingCompiler.MatchString(line) {
		return MinGWHint
	}
	return ""
}
