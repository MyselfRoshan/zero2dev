package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyRuntimes(t *testing.T) {
	root := t.TempDir()
	home, bin := filepath.Join(root, "data"), filepath.Join(root, "bin")
	os.MkdirAll(filepath.Join(home, "runtimes", "caddy-2.9.1"), 0o755)
	os.WriteFile(filepath.Join(home, "runtimes", "caddy-2.9.1", "caddy.exe"), []byte("c"), 0o755)
	os.MkdirAll(filepath.Join(home, "runtimes", "php-8.4.1"), 0o755)
	os.MkdirAll(filepath.Join(bin, "php-8.4.1"), 0o755) // already there: kept, not overwritten
	if n := migrateLegacyRuntimes(home, bin); n != 1 {
		t.Fatalf("moved %d", n)
	}
	if b, err := os.ReadFile(filepath.Join(bin, "caddy-2.9.1", "caddy.exe")); err != nil || string(b) != "c" {
		t.Fatalf("caddy not moved: %v", err)
	}
	if migrateLegacyRuntimes(home, bin) != 0 {
		t.Fatal("second run must be a no-op")
	}
}
