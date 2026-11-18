package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreeProgressAndDirSize(t *testing.T) {
	src := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "a", "b"), 0o755))
	must(os.WriteFile(filepath.Join(src, "one.txt"), []byte("12345"), 0o644))
	must(os.WriteFile(filepath.Join(src, "a", "b", "two.txt"), []byte("123"), 0o644))
	must(os.MkdirAll(filepath.Join(src, "empty"), 0o755))

	if n, err := DirSize(src); err != nil || n != 8 {
		t.Fatalf("DirSize = %d, %v; want 8", n, err)
	}
	if n, err := DirSize(filepath.Join(src, "missing")); err != nil || n != 0 {
		t.Fatalf("a missing folder is 0: %d, %v", n, err)
	}

	dst := filepath.Join(t.TempDir(), "copy")
	var calls int
	var last [2]int64
	must(CopyTreeProgress(src, dst, func(done, total int64) {
		calls++
		last = [2]int64{done, total}
		if done > total {
			t.Errorf("done %d > total %d", done, total)
		}
	}))
	if calls < 3 || last != [2]int64{8, 8} {
		t.Fatalf("progress calls %d, last %v", calls, last)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "a", "b", "two.txt")); err != nil || string(b) != "123" {
		t.Fatalf("nested file: %q %v", b, err)
	}
	if fi, err := os.Stat(filepath.Join(dst, "empty")); err != nil || !fi.IsDir() {
		t.Fatalf("empty folder not copied: %v", err)
	}
	// copying again overwrites
	must(os.WriteFile(filepath.Join(src, "one.txt"), []byte("x"), 0o644))
	must(CopyTree(src, dst))
	if b, _ := os.ReadFile(filepath.Join(dst, "one.txt")); string(b) != "x" {
		t.Fatalf("not overwritten: %q", b)
	}
}
