package newproject

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zero2dev/internal/frameworks"
)

// shippedAxum is the real Axum entry of the shipped list.
func shippedAxum(t *testing.T) frameworks.Framework {
	t.Helper()
	list, err := frameworks.Load("")
	if err != nil {
		t.Fatal(err)
	}
	fw, ok := frameworks.Get(list, "axum")
	if !ok {
		t.Fatal("no axum framework")
	}
	return fw
}

func TestAxumPlanInstallsRust(t *testing.T) {
	e := newEnv(t, shippedAxum(t))
	req := e.req("axum")
	p, err := e.m.Plan(req)
	if err != nil || len(p.Problems) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	if len(p.Installs) != 1 || p.Installs[0].Kind != "rust" || p.Installs[0].CatalogID != "rust-1.100.0-windows-amd64" || p.Installs[0].Reason != "Axum needs Rust 1.75 or newer" {
		t.Fatalf("installs %+v", p.Installs)
	}
	if p.Steps[0] != "Installing Rust 1.100.0 (stable, GNU)" || !slices.Contains(p.Steps, "Adding Axum") {
		t.Errorf("steps %q", p.Steps)
	}
	if !slices.Contains(p.Creates, "Service My Shop (Rust, port 3000)") {
		t.Errorf("creates %q", p.Creates)
	}
	// With Rust installed nothing is installed.
	e.install("rust", "1.100.0", true)
	if p, _ := e.m.Plan(req); len(p.Installs) != 0 {
		t.Errorf("installed: %+v", p.Installs)
	}
	// Too old: the plan installs a fitting one.
	e2 := newEnv(t, shippedAxum(t))
	e2.install("rust", "1.70.0", true)
	if p, _ := e2.m.Plan(e2.req("axum")); len(p.Installs) != 1 {
		t.Errorf("1.70 is too old for axum 0.8: %+v", p.Installs)
	}
}

func TestAxumStartRunsCargo(t *testing.T) {
	e := newEnv(t, shippedAxum(t))
	e.prog("cargo")
	e.install("rust", "1.100.0", true)
	if done := e.start(e.req("axum")); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	dir := filepath.Join(e.parent, "my-shop")
	b, err := os.ReadFile(filepath.Join(dir, "src", "main.rs"))
	if err != nil || !strings.Contains(string(b), `std::env::var("PORT")`) || !strings.Contains(string(b), "My Shop works.") {
		t.Fatalf("main.rs %q %v", b, err)
	}
	var lines []string
	for _, c := range e.run.cmds {
		if c.Dir != dir {
			t.Errorf("cargo runs in the project folder: %+v", c)
		}
		lines = append(lines, strings.Join(c.Args, " "))
	}
	want := []string{"init --vcs none --name my-shop", "add axum@0.8", "add tokio@1 --features macros,rt-multi-thread,net"}
	if !slices.Equal(lines, want) {
		t.Errorf("cargo ran %q, want %q", lines, want)
	}
	s := e.st.Snapshot().Services[0]
	if s.Kind != "rust" || s.Port != 3000 {
		t.Errorf("service %+v", s)
	}
}
