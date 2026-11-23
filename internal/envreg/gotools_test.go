package envreg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// No test looks at the real user's Go folder; one that wants it sets userGoBin.
func init() { userGoBin = func() string { return "" } }

// goData is a data folder with one Go runtime installed.
func goData(home string) store.Data {
	bin := filepath.Join(home, "bin", "go-1.24", "bin", "go.exe")
	return store.Data{Runtimes: []model.Runtime{{ID: "rt_go", Kind: "go", Version: "1.24", Binary: bin, Source: "installed", Default: true}}}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestGoToolsGetShimsAndGoPath(t *testing.T) {
	home := t.TempDir()
	tools := model.GoToolsDir(home)
	for _, f := range []string{"air.exe", "dlv.exe", "gopls.EXE", "notes.txt", "go.exe"} {
		touch(t, filepath.Join(tools, f))
	}
	if err := os.MkdirAll(filepath.Join(tools, "sub.exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := goData(home)
	files := byName(FilesAt(home, d, func(string) bool { return true }, os.ReadDir))

	// go and gofmt set GOPATH, so `go install` fills the same folder in any terminal.
	goShim := files["go.cmd"]
	if !strings.Contains(goShim, `set "GOPATH=`+model.GoPath(home)+`"`) || !strings.Contains(goShim, `set "GOROOT=`) {
		t.Fatalf("go.cmd: %q", goShim)
	}
	// Every installed tool has a shim that runs it. A runtime's own shim wins a name.
	for _, name := range []string{"air.cmd", "dlv.cmd", "gopls.cmd"} {
		if !strings.Contains(files[name], filepath.Join(tools, strings.TrimSuffix(name, ".cmd")+".exe")) &&
			!strings.Contains(strings.ToLower(files[name]), strings.ToLower(filepath.Join(tools, strings.TrimSuffix(name, ".cmd")+".exe"))) {
			t.Errorf("%s: %q", name, files[name])
		}
	}
	for _, name := range []string{"notes.cmd", "sub.cmd"} {
		if _, ok := files[name]; ok {
			t.Errorf("%s must not exist", name)
		}
	}
	if strings.Contains(files["go.cmd"], tools+`\go.exe`) {
		t.Error("the go shim stays the runtime's, not a tool named go")
	}
	// The activate scripts have the tools folder first (a tool installed later works at once) and GOPATH.
	for _, name := range []string{"activate.cmd", "activate.ps1", "activate.sh"} {
		c := files[name]
		if !strings.Contains(c, tools) || !strings.Contains(c, model.GoPath(home)) {
			t.Errorf("%s lacks the tools folder or GOPATH: %q", name, c)
		}
	}
}

func TestGoToolsOnlyWithAHome(t *testing.T) {
	home := t.TempDir()
	touch(t, filepath.Join(model.GoToolsDir(home), "air.exe"))
	d := goData(home)
	// Files has no data folder: nothing of Go's tools, as before.
	files := byName(Files(d, func(string) bool { return true }))
	if _, ok := files["air.cmd"]; ok {
		t.Fatal("Files must not know the tools folder")
	}
	if strings.Contains(files["go.cmd"], "GOPATH") {
		t.Fatalf("go.cmd: %q", files["go.cmd"])
	}
	// No Go runtime: the tools still get shims, but GOPATH and the PATH entry belong to Go.
	files = byName(FilesAt(home, store.Data{}, func(string) bool { return true }, os.ReadDir))
	if _, ok := files["air.cmd"]; !ok {
		t.Fatal("air.cmd")
	}
	if strings.Contains(files["activate.cmd"], model.GoToolsDir(home)) {
		t.Fatalf("activate.cmd without Go: %q", files["activate.cmd"])
	}
	// No folder yet: no error, no files.
	empty := t.TempDir()
	if got := byName(FilesAt(empty, goData(empty), func(string) bool { return true }, os.ReadDir)); got["air.cmd"] != "" {
		t.Fatal("no tools folder, no tool shims")
	}
}

func TestGoDefaultToolsFolderGetsShimsToo(t *testing.T) {
	home, user := t.TempDir(), t.TempDir()
	userBin := filepath.Join(user, "go", "bin")
	touch(t, filepath.Join(userBin, "old.exe"))
	touch(t, filepath.Join(model.GoToolsDir(home), "air.exe"))
	touch(t, filepath.Join(userBin, "air.exe")) // the same name: Zero2Dev's folder wins
	userGoBin = func() string { return userBin }
	t.Cleanup(func() { userGoBin = func() string { return "" } })
	files := byName(FilesAt(home, goData(home), func(string) bool { return true }, os.ReadDir))
	if !strings.Contains(files["old.cmd"], userBin) {
		t.Fatalf("old.cmd: %q", files["old.cmd"])
	}
	if !strings.Contains(files["air.cmd"], model.GoToolsDir(home)) || strings.Contains(files["air.cmd"], userBin) {
		t.Fatalf("air.cmd must run Zero2Dev's tool: %q", files["air.cmd"])
	}
	// Both folders are on the activated PATH, Zero2Dev's before the runtime's, Go's default after.
	cmd := files["activate.cmd"]
	i, j := strings.Index(cmd, model.GoToolsDir(home)), strings.Index(cmd, userBin)
	if i < 0 || j < 0 || i > j {
		t.Fatalf("activate.cmd: %q", cmd)
	}
	// A default folder that is not there is left out; Zero2Dev's own is always on the PATH.
	userGoBin = func() string { return filepath.Join(user, "missing", "bin") }
	cmd = byName(FilesAt(home, goData(home), func(string) bool { return true }, os.ReadDir))["activate.cmd"]
	if strings.Contains(cmd, "missing") || !strings.Contains(cmd, model.GoToolsDir(home)) {
		t.Fatalf("activate.cmd: %q", cmd)
	}
}
