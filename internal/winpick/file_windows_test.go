//go:build windows

package winpick

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSplitStartAndDefaultExt(t *testing.T) {
	dir := t.TempDir()
	if d, n := splitStart(dir); d != dir || n != "" {
		t.Errorf("folder: %q %q", d, n)
	}
	if d, n := splitStart(filepath.Join(dir, "missing", "shop.sql")); d != dir || n != "shop.sql" {
		t.Errorf("file below a missing folder: %q %q", d, n)
	}
	if d, n := splitStart("relative.sql"); d != "" || n != "" {
		t.Errorf("relative: %q %q", d, n)
	}
	for pattern, want := range map[string]string{"*.sql;*.sql.gz": "sql", "*.gz": "gz", "*.*": "", "*": "", "dump*": ""} {
		if got := defaultExt([]Filter{{Name: "x", Pattern: pattern}}); got != want {
			t.Errorf("defaultExt(%q) = %q, want %q", pattern, got, want)
		}
	}
	if defaultExt(nil) != "" {
		t.Error("no filters")
	}
}

func TestFileBusy(t *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	if _, _, err := File(FileOptions{Title: "x"}); err != ErrBusy {
		t.Fatalf("second dialog must be refused: %v", err)
	}
}

// Shows real dialogs, so it needs an interactive desktop session.
func TestFileDialog(t *testing.T) {
	if os.Getenv("Z2D_DIALOG_TEST") != "1" {
		t.Skip("set Z2D_DIALOG_TEST=1 to open real file dialogs")
	}
	dir := t.TempDir()
	filters := []Filter{{Name: "SQL dumps", Pattern: "*.sql;*.sql.gz"}}

	title := fmt.Sprintf("z2d test cancel %d", time.Now().UnixNano())
	press(t, title, idCancel)
	if p, ok, err := File(FileOptions{Title: title, Start: dir, Filters: filters}); err != nil || ok || p != "" {
		t.Fatalf("cancel: %q %v %v", p, ok, err)
	}

	// Save as with a suggested name: OK returns that name in the start folder.
	title = fmt.Sprintf("z2d test save %d", time.Now().UnixNano())
	press(t, title, idOK)
	p, ok, err := File(FileOptions{Title: title, Start: dir, Save: true, DefaultName: "shop.sql", Filters: filters})
	if err != nil || !ok {
		t.Fatalf("save: %q %v %v", p, ok, err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(filepath.Dir(p))
	if !strings.EqualFold(got, want) || !strings.EqualFold(filepath.Base(p), "shop.sql") {
		t.Fatalf("saved as %q, want shop.sql in %q", p, dir)
	}

	// Open with an existing file named in start.
	f := filepath.Join(dir, "in.sql")
	if err := os.WriteFile(f, []byte("SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	title = fmt.Sprintf("z2d test open %d", time.Now().UnixNano())
	press(t, title, idOK)
	p, ok, err = File(FileOptions{Title: title, Start: f, Filters: filters})
	if err != nil || !ok || !strings.EqualFold(filepath.Base(p), "in.sql") {
		t.Fatalf("open: %q %v %v", p, ok, err)
	}
}
