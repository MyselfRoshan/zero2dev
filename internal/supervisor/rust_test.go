package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestRustServiceEnvironment(t *testing.T) {
	s, home, _ := envSetup(t)
	install := filepath.Join(home, "bin", "rust-1.100.0")
	bin := filepath.Join(install, "cargo", "bin", "cargo.exe")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := model.Service{ID: "rs1", Name: "API", Kind: "rust", Cwd: home, Port: 3000, Settings: map[string]string{"run": "release", "bin": "server"}}
	s.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_rs", Kind: "rust", Version: "1.100.0", Name: "Rust 1.100.0", Binary: bin, Default: true})
		d.Services = append(d.Services, svc)
		return nil
	})
	t.Cleanup(func() { s.Remove("rs1") })

	r, err := s.resolve(svc, s.st.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if r.bin != bin || strings.Join(r.args, " ") != "run --release --bin server" {
		t.Fatalf("command %s %q", r.bin, r.args)
	}
	target := kinds.TargetDir(home, "rs1")
	for name, want := range map[string]string{
		"RUSTUP_HOME": filepath.Join(install, "rustup"), "CARGO_HOME": filepath.Join(install, "cargo"),
		"CARGO_TARGET_DIR": target, "PORT": "3000",
	} {
		if got, ok := last(r.env, name); !ok || got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	path, _ := last(r.env, "PATH")
	if !strings.HasPrefix(path, filepath.Dir(bin)+string(os.PathListSeparator)) {
		t.Errorf("<CARGO_HOME>\bin must be first on PATH: %q", path)
	}
	found := false
	for _, d := range r.dirs {
		found = found || d == target
	}
	if !found {
		t.Errorf("the target folder is made before the start: %q", r.dirs)
	}
	// The command preview says where the build goes.
	p := s.Preview(svc)
	if !strings.Contains(strings.Join(p.Env, "\n"), "CARGO_TARGET_DIR="+target) {
		t.Errorf("preview env %q", p.Env)
	}
}

func TestRustHintOncePerStart(t *testing.T) {
	s, _, _ := envSetup(t)
	emit := s.rustHintEmit("rs1")
	for _, l := range []string{"   Compiling foo v0.1.0", "error: linker `gcc` not found", "error: linker `gcc` not found", "error: could not compile `foo`"} {
		emit(l)
	}
	hints := 0
	for _, l := range s.Logs("rs1", 50) {
		if l.Stream == "system" && l.Text == kinds.MinGWHint {
			hints++
		}
	}
	if hints != 1 || len(s.Logs("rs1", 50)) != 5 {
		t.Fatalf("%d hints in %+v", hints, s.Logs("rs1", 50))
	}
	s.logFor("rs1").close()
}
