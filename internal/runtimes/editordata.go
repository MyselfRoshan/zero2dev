package runtimes

import (
	"fmt"
	"os"
	"path/filepath"

	"zero2dev/internal/fsutil"
	"zero2dev/internal/model"
)

// VS Code and VSCodium run portable when a folder named "data" sits next to
// their exe, however they are started (a double-click in bin, a shortcut, the
// taskbar). Zero2Dev keeps that data in <home>\code and <home>\codium, outside
// bin, so it survives a version update. The link <install>\data -> <home>\code
// makes both true: every start of an installed VS Code uses the same settings
// and extensions as one opened from Zero2Dev (which also sets
// VSCODE_PORTABLE to that folder).

// editorLink returns the link and the folder it points at for an installed VS
// Code or VSCodium below bin, or "" for every other runtime.
func (m *Manager) editorLink(rt model.Runtime) (link, target string) {
	if rt.Source != "installed" || (rt.Kind != "vscode" && rt.Kind != "vscodium") || m.installDir(rt.Binary) == "" {
		return "", ""
	}
	name := model.DataDirName(rt.Kind)
	if name == "" {
		return "", ""
	}
	return filepath.Join(filepath.Dir(rt.Binary), "data"), filepath.Join(m.home, name)
}

// EnsureEditorData makes or repairs the data link of an installed VS Code or
// VSCodium (nothing for other runtimes). A link that points elsewhere (the data
// folder moved) is replaced. A real "data" folder is the user's own portable
// data: it is left alone and reported.
func (m *Manager) EnsureEditorData(rt model.Runtime) error {
	link, target := m.editorLink(rt)
	if link == "" {
		return nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	if cur, ok := fsutil.LinkTarget(link); ok {
		if fsutil.SamePath(cur, target) {
			return nil
		}
		if err := os.Remove(link); err != nil { // removes the link only
			return err
		}
	} else if _, err := os.Lstat(link); err == nil {
		return fmt.Errorf("%s has a data folder of its own at %s; Zero2Dev's settings are in %s", rt.Name, link, target)
	}
	return fsutil.Junction(link, target)
}

// RepairEditorData runs EnsureEditorData for every runtime, for example at
// start after the data folder moved. It returns one error per runtime it could
// not fix.
func (m *Manager) RepairEditorData() []error {
	var errs []error
	for _, rt := range m.st.Snapshot().Runtimes {
		if err := m.EnsureEditorData(rt); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// removeEditorLink deletes the data link (never what it points at) before a
// runtime's folder is removed.
func (m *Manager) removeEditorLink(rt model.Runtime) error {
	link, _ := m.editorLink(rt)
	if link == "" {
		return nil
	}
	if _, ok := fsutil.LinkTarget(link); !ok {
		return nil
	}
	return os.Remove(link)
}
