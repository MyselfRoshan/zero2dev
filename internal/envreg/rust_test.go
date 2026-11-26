package envreg

import (
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestFilesRustShims(t *testing.T) {
	install := filepath.Join("z", "bin", "rust-1.100.0")
	bin := filepath.Join(install, "cargo", "bin")
	cargo := filepath.Join(bin, "cargo.exe")
	d := store.Data{Runtimes: []model.Runtime{{Kind: "rust", Binary: cargo, Default: true}}}
	env := map[string]string{"RUSTUP_HOME": filepath.Join(install, "rustup"), "CARGO_HOME": filepath.Join(install, "cargo")}
	exists := func(p string) bool {
		return p == filepath.Join(bin, "rustc.exe") || p == filepath.Join(bin, "rustup.exe")
	}
	got := byName(Files(d, exists))
	for _, c := range []struct{ name, target string }{{"cargo.cmd", cargo}, {"rustc.cmd", filepath.Join(bin, "rustc.exe")}, {"rustup.cmd", filepath.Join(bin, "rustup.exe")}} {
		if got[c.name] != ShimCmd(c.target, env) {
			t.Errorf("%s = %q", c.name, got[c.name])
		}
		if !strings.Contains(got[c.name], "RUSTUP_HOME=") || !strings.Contains(got[c.name], "CARGO_HOME=") {
			t.Errorf("%s must set RUSTUP_HOME and CARGO_HOME: %q", c.name, got[c.name])
		}
	}
	for _, name := range []string{"activate.cmd", "activate.ps1", "activate.sh"} {
		s := strings.ReplaceAll(got[name], "\\", "/")
		for _, w := range []string{"RUSTUP_HOME", "CARGO_HOME", strings.ReplaceAll(bin, "\\", "/")} {
			if !strings.Contains(s, w) {
				t.Errorf("%s lacks %q", name, w)
			}
		}
	}
	// A cargo registered from ~\.cargo\bin keeps the user's own variables.
	d.Runtimes[0].Binary = filepath.Join("u", ".cargo", "bin", "cargo.exe")
	got = byName(Files(d, func(string) bool { return false }))
	if strings.Contains(got["cargo.cmd"], "RUSTUP_HOME") {
		t.Errorf("a foreign cargo gets no variables: %q", got["cargo.cmd"])
	}
}
