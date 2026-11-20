package runtimes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/fsutil"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// editorEnv is a data folder with bin\vscode-1.0.0\Code.exe installed.
type editorEnv struct {
	t    *testing.T
	home string
	m    *Manager
	rt   model.Runtime
}

func newEditorEnv(t *testing.T) *editorEnv {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(home, st, nopEmitter{})
	m.Bin = filepath.Join(home, "bin")
	dir := filepath.Join(m.Bin, "vscode-1.0.0")
	writeFile(t, filepath.Join(dir, "Code.exe"), "exe")
	rt := model.Runtime{ID: "rt_code", Kind: "vscode", Version: "1.0.0", Name: "VS Code 1.0.0", Source: "installed", Binary: filepath.Join(dir, "Code.exe")}
	return &editorEnv{t: t, home: home, m: m, rt: rt}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *editorEnv) link() string { return filepath.Join(filepath.Dir(e.rt.Binary), "data") }

func TestEditorDataLinkOnAdd(t *testing.T) {
	e := newEditorEnv(t)
	target := filepath.Join(e.home, "code")
	writeFile(t, filepath.Join(target, "user-data", "User", "settings.json"), `{"a":1}`)
	if err := e.m.Add(e.rt); err != nil {
		t.Fatal(err)
	}
	cur, ok := fsutil.LinkTarget(e.link())
	if !ok || !fsutil.SamePath(cur, target) {
		t.Fatalf("link %q (%v), want %q", cur, ok, target)
	}
	// VS Code sees the settings through bin\...\data, as if they lived there.
	b, err := os.ReadFile(filepath.Join(e.link(), "user-data", "User", "settings.json"))
	if err != nil || string(b) != `{"a":1}` {
		t.Fatalf("settings through the link: %q %v", b, err)
	}
	// Running it again changes nothing.
	if err := e.m.EnsureEditorData(e.rt); err != nil {
		t.Fatal(err)
	}
}

func TestEditorDataLinkRepairedAfterMove(t *testing.T) {
	e := newEditorEnv(t)
	old := filepath.Join(t.TempDir(), "old-code")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.Junction(e.link(), old); err != nil {
		t.Fatal(err)
	}
	if err := e.m.st.Update(func(d *store.Data) error { d.Runtimes = append(d.Runtimes, e.rt); return nil }); err != nil {
		t.Fatal(err)
	}
	if errs := e.m.RepairEditorData(); len(errs) != 0 {
		t.Fatal(errs)
	}
	if cur, _ := fsutil.LinkTarget(e.link()); !fsutil.SamePath(cur, filepath.Join(e.home, "code")) {
		t.Fatalf("link still points at %q", cur)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatal("the old target is not touched")
	}
}

func TestEditorDataKeepsARealDataFolder(t *testing.T) {
	e := newEditorEnv(t)
	writeFile(t, filepath.Join(e.link(), "user-data", "mine.txt"), "mine")
	err := e.m.EnsureEditorData(e.rt)
	if err == nil || !strings.Contains(err.Error(), "data folder of its own") {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(e.link(), "user-data", "mine.txt")); string(b) != "mine" {
		t.Fatal("the user's own data folder must stay")
	}
}

func TestEditorDataOnlyForInstalledEditors(t *testing.T) {
	e := newEditorEnv(t)
	for _, rt := range []model.Runtime{
		{Kind: "vscode", Source: "registered", Binary: e.rt.Binary},
		{Kind: "node", Source: "installed", Binary: filepath.Join(e.m.Bin, "node-1", "node.exe")},
		{Kind: "vscode", Source: "installed", Binary: filepath.Join(t.TempDir(), "Code.exe")}, // outside bin
	} {
		if err := e.m.EnsureEditorData(rt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(e.link()); err == nil {
		t.Fatal("no link for a registered VS Code")
	}
}

func TestDeleteEditorKeepsItsData(t *testing.T) {
	e := newEditorEnv(t)
	settings := filepath.Join(e.home, "code", "user-data", "User", "settings.json")
	writeFile(t, settings, "keep")
	if err := e.m.Add(e.rt); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Delete(e.rt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(e.rt.Binary)); !os.IsNotExist(err) {
		t.Fatalf("program folder still there: %v", err)
	}
	if b, err := os.ReadFile(settings); err != nil || string(b) != "keep" {
		t.Fatalf("settings lost: %q %v", b, err)
	}
}
