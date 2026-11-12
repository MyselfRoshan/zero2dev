package runtimes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// VS Code and VSCodium are desktop (Electron) programs: running Code.exe or
// VSCodium.exe with --version opens the editor. Their version is read from the
// install's resources/app/package.json instead, and nothing is ever run.
//
// An install looks like
//
//	<root>/Code.exe                                  (VSCodium.exe)
//	<root>/bin/code.cmd                              (bin/codium.cmd, the command-line wrapper)
//	<root>/resources/app/package.json                (older layout, VSCodium)
//	<root>/<commit>/resources/app/package.json       (current VS Code layout)
//
// Some tools (Laragon, for one) put <root> itself on PATH, so looking up "code"
// finds Code.exe, not the wrapper.

var editorExe = map[string]string{"vscode": "Code", "vscodium": "VSCodium"}

// isEditor reports whether kind is an Electron editor handled here.
func isEditor(kind string) bool { return editorExe[kind] != "" }

// editorRoot returns the install folder of a VS Code / VSCodium binary: the
// folder of the .exe, or the parent of bin/ for the command-line wrapper.
func editorRoot(binary string) string {
	dir := filepath.Dir(binary)
	if strings.EqualFold(filepath.Base(dir), "bin") {
		return filepath.Dir(dir)
	}
	return dir
}

// editorBinary returns the editor program itself (Code.exe, VSCodium.exe; on
// other systems the wrapper is kept), so launching it opens the editor and a
// found wrapper is never recorded instead.
func editorBinary(kind, binary string) string {
	if runtime.GOOS != "windows" {
		return binary
	}
	exe := filepath.Join(editorRoot(binary), editorExe[kind]+".exe")
	if st, err := os.Stat(exe); err == nil && !st.IsDir() {
		return exe
	}
	return binary
}

// editorVersion reads the version from package.json without running anything.
// "" when it cannot be found.
func editorVersion(binary string) string {
	root := editorRoot(binary)
	cands := []string{filepath.Join(root, "resources", "app", "package.json")}
	if ents, err := os.ReadDir(root); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				cands = append(cands, filepath.Join(root, e.Name(), "resources", "app", "package.json"))
			}
		}
	}
	for _, c := range cands {
		b, err := os.ReadFile(c)
		if err != nil || len(b) > 1<<20 {
			continue
		}
		var pkg struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(b, &pkg) == nil {
			if v := versionRE.FindString(pkg.Version); v != "" {
				return v
			}
		}
	}
	return ""
}
