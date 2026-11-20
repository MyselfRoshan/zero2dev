package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJunction(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "prog", "data")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := LinkTarget(link); ok {
		t.Fatal("nothing there yet")
	}
	if err := Junction(link, target); err != nil {
		t.Fatal(err)
	}
	if cur, ok := LinkTarget(link); !ok || !SamePath(cur, target) {
		t.Fatalf("LinkTarget = %q %v", cur, ok)
	}
	if b, err := os.ReadFile(filepath.Join(link, "f.txt")); err != nil || string(b) != "x" {
		t.Fatalf("read through the link: %q %v", b, err)
	}
	// Writing through the link lands in the target.
	if err := os.WriteFile(filepath.Join(link, "g.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "g.txt")); err != nil {
		t.Fatal(err)
	}
	if err := Junction(link, target); err == nil {
		t.Fatal("an existing link is not overwritten")
	}
	if _, ok := LinkTarget(target); ok {
		t.Fatal("a real folder is no link")
	}
	// os.RemoveAll of the folder holding the link removes the link, not
	// what it points at.
	if err := os.RemoveAll(filepath.Dir(link)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"f.txt", "g.txt"} {
		if _, err := os.Stat(filepath.Join(target, f)); err != nil {
			t.Fatalf("%s lost: %v", f, err)
		}
	}
}
