package kinds

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func rustCtx() Ctx {
	c := ctx()
	c.Bin = filepath.FromSlash("/z/bin/rust-1.100.0/cargo/bin/cargo.exe")
	c.Port = "3000"
	c.ServiceID = "svc_rs"
	return c
}

func TestBuildRustRunModes(t *testing.T) {
	c := rustCtx()
	target := filepath.Join(c.Home, "rust", "target", "svc_rs")
	for _, tc := range []struct {
		name     string
		settings map[string]string
		extra    []string
		args     string
	}{
		{"default", nil, nil, "run"},
		{"release", map[string]string{"run": "release"}, nil, "run --release"},
		{"package and bin", map[string]string{"package": "api", "bin": "server"}, nil, "run -p api --bin server"},
		{"features", map[string]string{"run": "release", "features": "serde,tokio/full"}, nil, "run --release --features serde,tokio/full"},
		{"extra after --", map[string]string{"bin": "server"}, []string{"--port", "{port}"}, "run --bin server -- --port 3000"},
	} {
		if err := Validate("rust", tc.settings); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		pl := Build("rust", tc.settings, tc.extra, c)
		if got := strings.Join(pl.Args, " "); got != tc.args {
			t.Errorf("%s: args %q, want %q", tc.name, got, tc.args)
		}
		if !slices.Contains(pl.Env, "CARGO_TARGET_DIR="+target) {
			t.Errorf("%s: env %q lacks CARGO_TARGET_DIR", tc.name, pl.Env)
		}
		if !slices.Contains(pl.Dirs, target) {
			t.Errorf("%s: the target folder is made before the start: %q", tc.name, pl.Dirs)
		}
	}
	for _, bad := range []map[string]string{{"run": "debugger"}, {"package": "a b"}, {"bin": "x;y"}, {"features": "a;b"}} {
		if Validate("rust", bad) == nil {
			t.Errorf("%v must be refused", bad)
		}
	}
	// A service with its own arguments still gets its own target folder.
	if pl := BuildCustom("rust", []string{"build", "--release"}, c); !slices.Contains(pl.Env, "CARGO_TARGET_DIR="+target) {
		t.Errorf("custom: %q", pl.Env)
	}
}

func TestRustTasks(t *testing.T) {
	p, ok := Lookup("rust")
	if !ok {
		t.Fatal("no rust profile")
	}
	got := map[string]Task{}
	for _, tk := range p.Tasks {
		got[tk.ID] = tk
	}
	for id, args := range map[string]string{"cargo-build": "build", "cargo-test": "test", "cargo-fetch": "fetch", "cargo-clippy": "clippy"} {
		tk, ok := got[id]
		if !ok || tk.Program != "cargo" || strings.Join(tk.Args, " ") != args || tk.When != "Cargo.toml" {
			t.Errorf("%s: %+v", id, tk)
		}
	}
	if tk := got["rustup-clippy"]; tk.Program != "rustup" || strings.Join(tk.Args, " ") != "component add clippy" {
		t.Errorf("rustup-clippy: %+v", tk)
	}
}

func TestRustHint(t *testing.T) {
	for line, want := range map[string]bool{
		"error: linker `gcc` not found":                                  true,
		"error: linker `link.exe` not found":                             true,
		"  = note: program not found":                                    false,
		"error occurred: Failed to find tool. Is `gcc` installed?":       true,
		"error: error calling dlltool 'dlltool.exe': program not found":  true,
		"   Compiling serde v1.0.200":                                    false,
		"error[E0425]: cannot find value `x` in this scope":              false,
		"thread 'main' panicked at src/main.rs:3:5: linker is mentioned": false,
	} {
		if got := RustHint(line) != ""; got != want {
			t.Errorf("%q: hint %v, want %v", line, got, want)
		}
	}
	if !strings.Contains(MinGWHint, "MinGW") {
		t.Error(MinGWHint)
	}
}
