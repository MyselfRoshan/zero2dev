//go:build windows

package winpick

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

var (
	procFindWindowW  = user32.NewProc("FindWindowW")
	procPostMessageW = user32.NewProc("PostMessageW")
)

const (
	wmCommand = 0x0111
	idOK      = 1
	idCancel  = 2
)

// press waits for the dialog titled title and sends it a button command, as
// if the user clicked it.
func press(t *testing.T, title string, button uintptr) {
	t.Helper()
	go func() {
		for i := 0; i < 200; i++ {
			h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(utf16(title))))
			if h != 0 {
				time.Sleep(300 * time.Millisecond) // let the dialog finish loading the folder
				procPostMessageW.Call(h, wmCommand, button, 0)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
}

// Shows real dialogs, so it needs an interactive desktop session.
func TestFolderDialog(t *testing.T) {
	if os.Getenv("Z2D_DIALOG_TEST") != "1" {
		t.Skip("set Z2D_DIALOG_TEST=1 to open real folder dialogs")
	}
	dir := filepath.Join(t.TempDir(), "pick me")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	title := fmt.Sprintf("z2d test cancel %d", time.Now().UnixNano())
	press(t, title, idCancel)
	if p, ok, err := Folder(title, dir); err != nil || ok || p != "" {
		t.Fatalf("cancel: %q %v %v", p, ok, err)
	}

	// "Select folder" with nothing typed picks the folder the dialog shows: the
	// start folder, also when start names a missing path below it.
	title = fmt.Sprintf("z2d test ok %d", time.Now().UnixNano())
	press(t, title, idOK)
	p, ok, err := Folder(title, filepath.Join(dir, "not", "there"))
	if err != nil || !ok {
		t.Fatalf("ok: %q %v %v", p, ok, err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(p)
	if !strings.EqualFold(got, want) {
		t.Fatalf("picked %q, want %q", p, dir)
	}
}

func TestBusyAndExistingDir(t *testing.T) {
	mu.Lock()
	if _, _, err := Folder("x", ""); err != ErrBusy {
		t.Fatalf("second dialog must be refused: %v", err)
	}
	mu.Unlock()

	dir := t.TempDir()
	if got := existingDir(filepath.Join(dir, "a", "b")); got != dir {
		t.Fatalf("nearest parent: %q", got)
	}
	if existingDir("relative\\path") != "" || existingDir("") != "" {
		t.Fatal("relative or empty start must be ignored")
	}
}
