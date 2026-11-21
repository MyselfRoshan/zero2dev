package runtimes

import (
	"encoding/binary"
	"encoding/json"
	"io"
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

// Bruno (Electron) and Hoppscotch Desktop (Tauri) are desktop programs too:
// running them opens a window, so their version is read from files.
//
//	Bruno:      <root>/resources/app.asar, whose package.json holds the version
//	Hoppscotch: nothing readable without the exe's version resource, so the name
//	            of the folder it is in is used (bin/hoppscotch-26.10.1-windows-amd64/)

// isAPIClient reports whether kind is an API client handled here.
func isAPIClient(kind string) bool { return kind == "bruno" || kind == "hoppscotch" }

// apiClientVersion returns the version of an API client install, "" when unknown.
func apiClientVersion(kind, binary string) string {
	dir := filepath.Dir(binary)
	if kind == "bruno" {
		if v := asarPackageVersion(filepath.Join(dir, "resources", "app.asar")); v != "" {
			return v
		}
	}
	return versionRE.FindString(filepath.Base(dir))
}

// asarPackageVersion reads "version" from the package.json at the top of an
// Electron asar archive: a 16-byte pickle prefix, a JSON index of files
// (offset and size, relative to the end of the index), then the file data.
func asarPackageVersion(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var pre [16]byte
	if _, err := io.ReadFull(f, pre[:]); err != nil {
		return ""
	}
	jsonLen := int64(binary.LittleEndian.Uint32(pre[12:]))
	base := 8 + int64(binary.LittleEndian.Uint32(pre[4:])) // end of the index
	if jsonLen <= 0 || jsonLen > 64<<20 {
		return ""
	}
	hdr := make([]byte, jsonLen)
	if _, err := f.ReadAt(hdr, 16); err != nil {
		return ""
	}
	var idx struct {
		Files map[string]struct {
			Size   int64       `json:"size"`
			Offset json.Number `json:"offset"`
		} `json:"files"`
	}
	if json.Unmarshal(hdr, &idx) != nil {
		return ""
	}
	pk, ok := idx.Files["package.json"]
	off, err := pk.Offset.Int64()
	if !ok || err != nil || pk.Size <= 0 || pk.Size > 1<<20 {
		return ""
	}
	b := make([]byte, pk.Size)
	if _, err := f.ReadAt(b, base+off); err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(b, &pkg) != nil {
		return ""
	}
	return versionRE.FindString(pkg.Version)
}
