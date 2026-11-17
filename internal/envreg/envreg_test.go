package envreg

import (
	"os"
	"path/filepath"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestRefreshWritesShimsAndRemovesStaleOnes(t *testing.T) {
	home := t.TempDir()
	dir := ShimsDir(home)
	d := store.Data{Runtimes: []model.Runtime{{Kind: "php", Binary: filepath.Join(home, "bin", "php", "php.exe"), Source: "installed", Default: true}}}
	if err := Refresh(home, d); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"php.cmd", "activate.cmd", "activate.ps1", "activate.sh"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s missing: %v", f, err)
		}
	}
	// A file the user keeps there is never deleted; a stale generated shim is.
	user := filepath.Join(dir, "mine.cmd")
	if err := os.WriteFile(user, []byte("@echo off\r\necho mine\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(home, "bin", "node", "node.exe")
	d.Runtimes = []model.Runtime{{Kind: "node", Binary: node, Source: "installed", Default: true}}
	if err := Refresh(home, d); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "php.cmd")); !os.IsNotExist(err) {
		t.Fatalf("stale php.cmd was not removed: %v", err)
	}
	if _, err := os.Stat(user); err != nil {
		t.Fatalf("a user file was removed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "node.cmd"))
	if err != nil || string(raw) != ShimCmd(node, nil) {
		t.Fatalf("node.cmd = %q, %v", raw, err)
	}
}

func TestRefreshIsStableWhenNothingChanged(t *testing.T) {
	home := t.TempDir()
	d := store.Data{Runtimes: []model.Runtime{{Kind: "go", Binary: filepath.Join(home, "bin", "go", "bin", "go.exe"), Default: true}}}
	if err := Refresh(home, d); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(ShimsDir(home), "go.cmd")
	before, _ := os.Stat(p)
	if err := Refresh(home, d); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an unchanged shim was rewritten")
	}
}
