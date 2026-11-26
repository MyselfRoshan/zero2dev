package runtimes

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"zero2dev/internal/store"
)

func TestVersionRegexp(t *testing.T) {
	cases := map[string]string{
		"v22.11.0\n":                          "22.11.0",
		"PHP 8.3.1 (cli) (built: Dec 2023)":   "8.3.1",
		"go version go1.23.3 linux/amd64":     "1.23.3",
		`openjdk version "21.0.1" 2023-10-17`: "21.0.1",
		"v2.8.4 h1:abc":                       "2.8.4",
		"nginx version: nginx/1.24.0":         "1.24.0",
		"no digits here":                      "",
	}
	for in, want := range cases {
		if got := parseVersion(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	// Mailpit prints its own path first; digits in the path must not win.
	mp := `C:\tools\2.1\mailpit.exe v1.33.0 compiled with go1.25.3 on windows/amd64` + "\n"
	if got := parseVersion(mp); got != "1.33.0" {
		t.Errorf("mailpit: %q", got)
	}
}

func TestDisplayName(t *testing.T) {
	if DisplayName("node", "22.11.0", "x") != "Node.js 22.11.0" || DisplayName("other", "1.0", "/usr/sbin/nginx") != "nginx 1.0" {
		t.Fatal("display names")
	}
	if DisplayName("mailpit", "1.33.0", "x") != "Mailpit 1.33.0" {
		t.Fatal(DisplayName("mailpit", "1.33.0", "x"))
	}
	if a := argsFor("mailpit", `C:\m\mailpit.exe`); strings.Join(a, " ") != "version --no-release-check" {
		t.Fatalf("mailpit probe args %q: it must not ask GitHub", a)
	}
}

func TestNoProbeForDesktopPrograms(t *testing.T) {
	for _, c := range []struct {
		kind, bin string
		skip      bool
	}{
		{"notepadpp", `C:\npp\notepad++.exe`, true},
		{"heidisql", `C:\h\heidisql.exe`, true},
		{"dbeaver", `C:\d\dbeaver.exe`, true},
		{"vscode", `C:\vsc\Code.exe`, true},
		{"vscode", `C:\vsc\bin\code.cmd`, true}, // never run: the version comes from package.json
		{"mariadb", `C:\m\bin\mariadbd.exe`, false},
		{"node", `C:\n\node.exe`, false},
	} {
		if got := argsFor(c.kind, c.bin) == nil; got != c.skip {
			t.Errorf("%s %s: no probe = %v, want %v", c.kind, c.bin, got, c.skip)
		}
	}
	if DisplayName("postgres", "17.2", "x") != "PostgreSQL 17.2" {
		t.Fatal(DisplayName("postgres", "17.2", "x"))
	}
}

// A VS Code install whose own folder is on PATH (as Laragon does): Detect and
// Register must read the version from package.json and never run Code.exe,
// which opens the editor.
func fakeEditor(t *testing.T, exe, cmd string, hashDir bool) string {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "bin"), 0o755)
	// A program that would leave a marker if anything ran it.
	os.WriteFile(filepath.Join(root, exe), []byte("not a program"), 0o755)
	os.WriteFile(filepath.Join(root, "bin", cmd), []byte("@echo off\r\necho ran> \"%~dp0..\\RAN\"\r\n"), 0o755)
	app := filepath.Join(root, "resources", "app")
	if hashDir {
		app = filepath.Join(root, "a717861c07", "resources", "app")
	}
	os.MkdirAll(app, 0o755)
	os.WriteFile(filepath.Join(app, "package.json"), []byte(`{"name":"code-oss-dev","version":"1.145.0"}`), 0o644)
	return root
}

func TestEditorVersionWithoutRunning(t *testing.T) {
	root := fakeEditor(t, "Code.exe", "code.cmd", true)
	for _, bin := range []string{filepath.Join(root, "Code.exe"), filepath.Join(root, "bin", "code.cmd")} {
		if v := editorVersion(bin); v != "1.145.0" {
			t.Errorf("%s: version %q", bin, v)
		}
	}
	cod := fakeEditor(t, "VSCodium.exe", "codium.cmd", false)
	if v := editorVersion(filepath.Join(cod, "bin", "codium.cmd")); v != "1.145.0" {
		t.Errorf("codium layout: %q", v)
	}
	if runtime.GOOS == "windows" {
		if b := editorBinary("vscode", filepath.Join(root, "bin", "code.cmd")); b != filepath.Join(root, "Code.exe") {
			t.Errorf("the wrapper must be recorded as the editor itself, got %s", b)
		}
	}
	if v := editorVersion(filepath.Join(t.TempDir(), "Code.exe")); v != "" {
		t.Errorf("no package.json: %q", v)
	}
}

func TestDetectAndRegisterNeverRunEditors(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PATH lookup of Code.exe is Windows behaviour")
	}
	root := fakeEditor(t, "Code.exe", "code.cmd", true)
	t.Setenv("PATH", root) // the install folder itself, like Laragon
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(t.TempDir(), st, nopEmitter{})
	var found *Detected
	for _, d := range m.Detect() {
		if d.Kind == "vscode" {
			found = &d
		}
	}
	if found == nil || found.Version != "1.145.0" || !strings.EqualFold(filepath.Base(found.Binary), "Code.exe") {
		t.Fatalf("detected %+v", found)
	}
	if _, err := m.Register("vscode", filepath.Join(root, "bin", "code.cmd"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "RAN")); err == nil {
		t.Fatal("the editor was run")
	}
}

type nopEmitter struct{}

func (nopEmitter) Publish(string, any) {}

func TestInstallDirKeepsOtherAdoptedRuntimes(t *testing.T) {
	bin := t.TempDir()
	m := &Manager{Bin: bin}
	cases := map[string]string{
		filepath.Join(bin, "node-22.1.0", "node.exe"):               filepath.Join(bin, "node-22.1.0"),
		filepath.Join(bin, "adopted", "node-20.11.0", "node.exe"):   filepath.Join(bin, "adopted", "node-20.11.0"),
		filepath.Join(bin, "adopted", "java-21", "bin", "java.exe"): filepath.Join(bin, "adopted", "java-21"),
		filepath.Join(bin, "adopted", "stray.exe"):                  "",
		filepath.Join(filepath.Dir(bin), "elsewhere", "node.exe"):   "",
	}
	for binary, want := range cases {
		if got := m.installDir(binary); got != want {
			t.Errorf("installDir(%s) = %q, want %q", binary, got, want)
		}
	}
}

// fakeAsar writes an Electron asar with a package.json like Bruno's: an index
// (pickle prefix plus JSON) followed by the file data.
func fakeAsar(t *testing.T, path, pkg string) {
	t.Helper()
	idx := fmt.Sprintf(`{"files":{"readme.md":{"size":3,"offset":"0"},"package.json":{"size":%d,"offset":"3"}}}`, len(pkg))
	var b bytes.Buffer
	pre := make([]byte, 16)
	binary.LittleEndian.PutUint32(pre[0:], 4)
	binary.LittleEndian.PutUint32(pre[4:], uint32(len(idx)+8))
	binary.LittleEndian.PutUint32(pre[8:], uint32(len(idx)+4))
	binary.LittleEndian.PutUint32(pre[12:], uint32(len(idx)))
	b.Write(pre)
	b.WriteString(idx)
	b.WriteString("abc" + pkg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAPIClientVersionWithoutRunning(t *testing.T) {
	root := t.TempDir()
	bruno := filepath.Join(root, "Bruno.exe")
	os.WriteFile(bruno, []byte("not a program"), 0o755)
	fakeAsar(t, filepath.Join(root, "resources", "app.asar"), `{"name":"bruno","version":"4.3.0"}`)
	if v := apiClientVersion("bruno", bruno); v != "4.3.0" {
		t.Errorf("bruno: %q", v)
	}
	// No archive: nothing to read, and a folder name without a version gives none.
	if v := apiClientVersion("bruno", filepath.Join(t.TempDir(), "Bruno.exe")); v != "" {
		t.Errorf("bruno without asar: %q", v)
	}
	hopp := filepath.Join(root, "hoppscotch-26.10.1-windows-amd64", "hoppscotch-desktop.exe")
	if v := apiClientVersion("hoppscotch", hopp); v != "26.10.1" {
		t.Errorf("hoppscotch: %q", v)
	}
	// Register reads the version and runs nothing (a "program" that is not one).
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(t.TempDir(), st, nopEmitter{})
	rt, err := m.Register("bruno", bruno, "")
	if err != nil || rt.Version != "4.3.0" {
		t.Fatalf("register: %+v %v", rt, err)
	}
}
