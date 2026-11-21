package supervisor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func pinDir(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNodePinWarning(t *testing.T) {
	n20 := model.Runtime{ID: "n20", Kind: "node", Version: "20.11.1"}
	n22 := model.Runtime{ID: "n22", Kind: "node", Version: "22.18.0"}
	n24 := model.Runtime{ID: "n24", Kind: "node", Version: "24.11.1"}
	php := model.Runtime{ID: "p", Kind: "php", Version: "22.18.0"}

	if w := nodePinWarning(t.TempDir(), &n20, nil); w != "" {
		t.Fatalf("nothing pinned: %q", w)
	}
	dir := pinDir(t, ".node-version", "22.18.0\n")
	if w := nodePinWarning(dir, &n22, nil); w != "" {
		t.Fatalf("the pin is met: %q", w)
	}
	// not met, no matching Node installed: say where to get one
	w := nodePinWarning(dir, &n20, []model.Runtime{n20, php})
	if !strings.Contains(w, "pins Node 22.18.0 (.node-version)") || !strings.Contains(w, "is 20.11.1") || !strings.Contains(w, "Runtimes page") {
		t.Fatalf("no match installed: %q", w)
	}
	// not met, a matching one is installed: name it (the highest that matches)
	dir = pinDir(t, "package.json", `{"devEngines":{"runtime":{"name":"node","version":"^22.18.0 || ^24.11.0"}}}`)
	w = nodePinWarning(dir, &n20, []model.Runtime{n20, n22, n24})
	if !strings.Contains(w, "Node 24.11.1 is installed") || !strings.Contains(w, "(devEngines.runtime)") || strings.Contains(w, "\n") {
		t.Fatalf("a match is installed: %q", w)
	}
	// a pin this does not read says nothing
	dir = pinDir(t, ".nvmrc", "lts/*\n")
	if w := nodePinWarning(dir, &n20, nil); w != "" {
		t.Fatalf("lts/* is not read: %q", w)
	}
}

// A Node service in the Vite+ mode: the project's vp command is the program; without it the start
// says what to do, and a pinned Node that the runtime does not satisfy is one warning.
func TestNodeViteplusResolveAndPinWarning(t *testing.T) {
	s, home, _ := envSetup(t)
	cwd := filepath.Join(home, "app")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	nodeBin := filepath.Join(home, "node", "node.exe")
	if err := os.MkdirAll(filepath.Dir(nodeBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nodeBin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	rt := model.Runtime{ID: "rt_node", Kind: "node", Version: "20.11.1", Name: "Node 20", Binary: nodeBin, Default: true}
	s.st.Update(func(d *store.Data) error { d.Runtimes = append(d.Runtimes, rt); return nil })
	if err := os.WriteFile(filepath.Join(cwd, ".node-version"), []byte("22.18.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := model.Service{ID: "vpapp", Name: "VP", Kind: "node", Cwd: cwd, Port: 5173, Settings: map[string]string{"run": "vp"}}
	vp := kinds.VitePlusShim(cwd, runtime.GOOS == "windows")

	// not installed: the start is refused with the sentence, the preview carries it as a warning
	_, err := s.resolve(svc, s.st.Snapshot())
	if err == nil || !strings.Contains(err.Error(), "vp is not installed in this project: run npm install") {
		t.Fatalf("start without vp: %v", err)
	}
	p := s.Preview(svc)
	if p.Program != vp || !strings.Contains(strings.Join(p.Warnings, "\n"), "vp is not installed in this project") {
		t.Fatalf("preview: %+v", p)
	}
	pin := 0
	for _, w := range p.Warnings {
		if strings.Contains(w, "pins Node 22.18.0 (.node-version)") {
			pin++
		}
	}
	if pin != 1 {
		t.Fatalf("one pin warning, got %d in %v", pin, p.Warnings)
	}

	// installed: it resolves, and the same pin warning is in the resolved warnings (the start log)
	if err := os.MkdirAll(filepath.Dir(vp), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vp, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := s.resolve(svc, s.st.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if r.bin != vp || strings.Join(r.args, " ") != "dev --port 5173 --strictPort" {
		t.Fatalf("%s %v", r.bin, r.args)
	}
	if len(r.warnings) != 1 || !strings.Contains(r.warnings[0], "pins Node 22.18.0") {
		t.Fatalf("warnings: %v", r.warnings)
	}
}
