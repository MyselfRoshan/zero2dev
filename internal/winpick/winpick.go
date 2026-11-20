// Package winpick opens the operating system's own "Select Folder" dialog on
// the machine the daemon runs on. The browser cannot do this itself: its
// directory picker never reveals the absolute path, which services and sites
// need. Only Windows is supported; elsewhere Folder returns ErrUnsupported.
package winpick

import (
	"errors"
	"sync"
)

var (
	// ErrUnsupported: no native folder dialog on this OS.
	ErrUnsupported = errors.New("the folder dialog is only available on Windows")
	// ErrBusy: a dialog is already open (one at a time).
	ErrBusy = errors.New("a folder dialog is already open")
)

// Supported reports whether Folder can show a dialog on this OS.
func Supported() bool { return supported }

var mu sync.Mutex

// Folder shows the dialog and blocks until the user closes it. It returns the
// chosen absolute path, or ok=false when the user cancelled. start, when it
// names an existing folder (or a path below one), is where the dialog opens.
func Folder(title, start string) (path string, ok bool, err error) {
	if !supported {
		return "", false, ErrUnsupported
	}
	if !mu.TryLock() {
		return "", false, ErrBusy
	}
	defer mu.Unlock()
	return pickFolder(title, start)
}

// Filter is one entry of a file dialog's type list, e.g. {"SQL dumps", "*.sql;*.sql.gz"}.
type Filter struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

// FileOptions describe a file dialog.
type FileOptions struct {
	Title       string
	Start       string   // a folder or file where the dialog opens
	Save        bool     // a "Save as" dialog instead of "Open"
	DefaultName string   // Save: the file name it suggests
	Filters     []Filter // empty: all files
}

// File shows the native Open or Save as dialog and blocks until the user
// closes it. It returns the chosen absolute path, or ok=false when the user
// cancelled. One dialog (folder or file) at a time: ErrBusy otherwise.
func File(o FileOptions) (path string, ok bool, err error) {
	if !supported {
		return "", false, ErrUnsupported
	}
	if !mu.TryLock() {
		return "", false, ErrBusy
	}
	defer mu.Unlock()
	return pickFile(o)
}
