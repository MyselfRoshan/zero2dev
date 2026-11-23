package envreg

import (
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestFilesPythonShimsFindPipInScripts(t *testing.T) {
	py := filepath.Join("z", "bin", "python-3.13", "python.exe")
	pip := filepath.Join("z", "bin", "python-3.13", "Scripts", "pip.exe")
	d := store.Data{Runtimes: []model.Runtime{{Kind: "python", Binary: py, Default: true}}}
	got := byName(Files(d, func(p string) bool { return p == pip }))
	if got["python.cmd"] != ShimCmd(py, nil) {
		t.Errorf("python.cmd = %q", got["python.cmd"])
	}
	if got["pip.cmd"] != ShimCmd(pip, nil) {
		t.Errorf("pip.cmd must run the pip in Scripts: %q", got["pip.cmd"])
	}
	// The activated PATH gets the runtime folder, then Scripts (the tools pip installs).
	want := strings.Join([]string{filepath.Dir(py), filepath.Dir(pip)}, ";")
	for _, name := range []string{"activate.cmd", "activate.ps1"} {
		if !strings.Contains(got[name], want) && !strings.Contains(strings.ReplaceAll(got[name], "\\", "/"), strings.ReplaceAll(want, "\\", "/")) {
			t.Errorf("%s lacks %q", name, want)
		}
	}

	// Without a pip (Scripts missing), only python gets a shim, and PATH has just its folder.
	got = byName(Files(d, func(string) bool { return false }))
	if _, ok := got["pip.cmd"]; ok {
		t.Error("pip.cmd must not be made without a pip")
	}
	if _, ok := got["python.cmd"]; !ok {
		t.Error("python.cmd missing")
	}
}
