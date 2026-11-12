package runtimes

import (
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
		if got := versionRE.FindString(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if DisplayName("node", "22.11.0", "x") != "Node.js 22.11.0" || DisplayName("other", "1.0", "/usr/sbin/nginx") != "nginx 1.0" {
		t.Fatal("display names")
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
