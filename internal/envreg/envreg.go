// Package envreg makes the runtimes Zero2Dev installed usable outside it: it
// keeps a folder of shims (node.cmd, npm.cmd, java.cmd, go.cmd, php.cmd, ...
// that forward to the default runtime of each kind, plus activate.cmd /
// activate.ps1 / activate.sh) in <home>\shims, and, for the items the user
// switched on in the Environment page, puts folders on the Windows user PATH and
// sets user environment variables (JAVA_HOME, GOROOT, ...).
//
// That is the one opt-in exception to "no registry": HKCU only, nothing on by
// default, every item on its own, fully reversible (see applyUserEnv).
//
// shims.go generates the files (pure), config.go validates and resolves the
// user's choices (pure), userpath.go holds the user environment logic (pure,
// through a registry interface) and userpath_windows.go the Windows registry.
package envreg

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"zero2dev/internal/fsutil"
	"zero2dev/internal/store"
)

// ErrUnsupported is returned where there is no user PATH to change (not Windows).
var ErrUnsupported = errors.New("changing the user PATH is only supported on Windows")

// The user environment entry points, replaced by tests so that no test touches the
// real registry of the machine.
var (
	openReg   = openRegistry
	broadcast = broadcastEnvChange
)

// ShimsDir is the folder of shims and activate scripts: <home>\shims.
func ShimsDir(home string) string { return filepath.Join(home, "shims") }

// BackupFile is where ApplyUserPath keeps the previous user PATH value so that
// RemoveUserPath can restore it: <home>\shims\.registry-backup.json.
func BackupFile(home string) string { return filepath.Join(ShimsDir(home), ".registry-backup.json") }

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// Refresh regenerates the shims and activate scripts from the installed
// runtimes (the default of each kind wins), removing shims of kinds that no
// longer have a runtime. It runs after every runtime change (install, delete,
// Make default, register) and once at start-up. When the user switched anything
// on in the Environment page, or Zero2Dev still owns something in the user
// environment, it also brings the user environment up to date (a no-op when
// nothing changed): a variable that follows the default runtime moves with Make
// default, and items of a removed runtime leave. A failure there is returned but
// the shims are written first.
func Refresh(home string, d store.Data) error {
	if err := writeShims(ShimsDir(home), FilesAt(home, d, fileExists, os.ReadDir)); err != nil {
		return err
	}
	if anyEnabled(d.Env) || fileExists(BackupFile(home)) {
		if err := Apply(home, d); err != nil && !errors.Is(err, ErrUnsupported) {
			return err
		}
	}
	return nil
}

// Apply writes the shims and then brings the user environment (HKCU\Environment, the
// one opt-in exception to "no registry") in line with the Environment config: the
// folders and variables that are switched on are added, the ones Zero2Dev added
// earlier and that are no longer wanted are taken back, and anything the user set
// themselves is never touched. New terminals see the change. It returns
// ErrUnsupported off Windows when something is switched on.
func Apply(home string, d store.Data) error {
	if err := writeShims(ShimsDir(home), FilesAt(home, d, fileExists, os.ReadDir)); err != nil {
		return err
	}
	reg := openReg()
	if reg == nil {
		if anyEnabled(d.Env) {
			return ErrUnsupported
		}
		return nil
	}
	changed, err := applyUserEnv(reg, Resolve(home, d, fileExists).Desired, BackupFile(home), ShimsDir(home))
	if changed {
		broadcast()
	}
	return err
}

// writeShims writes the files that changed (atomically) and deletes the files
// Zero2Dev generated earlier that are no longer wanted. Nothing outside dir is
// touched, and inside it only files that carry Zero2Dev's marker are deleted.
func writeShims(dir string, files []File) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	want := map[string]bool{}
	for _, f := range files {
		want[strings.ToLower(f.Name)] = true
		path := filepath.Join(dir, f.Name)
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, []byte(f.Content)) {
			continue
		}
		if err := fsutil.WriteFileAtomic(path, []byte(f.Content), 0o755); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		switch strings.ToLower(filepath.Ext(name)) {
		case ".cmd", ".ps1", ".sh":
		default:
			continue
		}
		if e.IsDir() || want[strings.ToLower(name)] {
			continue
		}
		path := filepath.Join(dir, name)
		if raw, err := os.ReadFile(path); err == nil && bytes.Contains(raw, []byte(marker)) {
			_ = os.Remove(path)
		}
	}
	return nil
}
