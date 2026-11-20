package migrate

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zero2dev/internal/model"
)

func (e *tenv) cleanup(id string) Migration {
	e.t.Helper()
	j, err := e.m.Cleanup(id)
	if err != nil {
		e.t.Fatalf("cleanup %s: %v", id, err)
	}
	if got := e.wait(j); got.State != "done" || got.Type != model.JobMigrateCleanup {
		e.t.Fatalf("cleanup job: %+v", got)
	}
	return e.migration(id)
}

func TestCleanupRemovesOriginalsOfACopyAndMoveBackRestores(t *testing.T) {
	e := setup(t)
	e.install("vscode")
	user, ext := e.vscodeTree()
	mg := e.run(Request{Source: ProgVSCode, Mode: ModeCopy})
	if !mg.CanCleanup || mg.OriginalsBytes == 0 || mg.FinishedAt == nil {
		t.Fatalf("copy: %+v", mg)
	}
	mg = e.cleanup(mg.ID)
	if exists(user) || exists(ext) {
		t.Fatal("the originals must be gone")
	}
	portable := filepath.Join(e.m.Home, "code", "user-data", "User", "settings.json")
	if read(t, portable) == "" {
		t.Fatal("the portable copy stays")
	}
	if mg.Mode != ModeMove || mg.CanCleanup || !slices.Equal(mg.RevertHow, []string{RevertMoveBack}) || !strings.HasPrefix(mg.Description, "Moved VS Code") {
		t.Fatalf("after cleanup: %+v", mg)
	}
	if _, err := e.m.Cleanup(mg.ID); status(err) != http.StatusConflict {
		t.Fatalf("second cleanup: %v", err)
	}
	e.revert(mg.ID, RevertMoveBack)
	if read(t, filepath.Join(user, "settings.json")) != `{"editor.fontSize": 14}` {
		t.Fatal("move back puts the originals back")
	}
	if exists(filepath.Join(e.m.Home, "code", "user-data", "User", "settings.json")) {
		t.Fatal("the portable place is empty again")
	}
}

func TestCleanupChecksTheCopyFirst(t *testing.T) {
	e := setup(t)
	e.install("vscode")
	user, _ := e.vscodeTree()
	mg := e.run(Request{Source: ProgVSCode, Mode: ModeCopy})
	// The portable copy lost a file: nothing may be removed.
	if err := os.Remove(filepath.Join(e.m.Home, "code", "user-data", "User", "settings.json")); err != nil {
		t.Fatal(err)
	}
	j, err := e.m.Cleanup(mg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.wait(j); got.State != "failed" || !strings.Contains(got.Error, "nothing was removed") {
		t.Fatalf("job %+v", got)
	}
	if !exists(filepath.Join(user, "settings.json")) {
		t.Fatal("the original must stay when the copy is incomplete")
	}
	if got := e.migration(mg.ID); got.Mode != ModeCopy || !got.CanCleanup {
		t.Fatalf("still a copy: %+v", got)
	}
}

func TestCleanupGuards(t *testing.T) {
	e := setup(t)
	e.install("vscode")
	e.vscodeTree()
	if _, err := e.m.Cleanup("mig_deadbeef"); status(err) != http.StatusNotFound {
		t.Fatalf("unknown: %v", err)
	}
	mg := e.run(Request{Source: ProgVSCode, Mode: ModeCopy})
	e.running["code.exe"] = true
	if _, err := e.m.Cleanup(mg.ID); status(err) != http.StatusConflict {
		t.Fatalf("running program: %v", err)
	}
	e.running["code.exe"] = false
	e.revert(mg.ID, RevertDiscard)
	if _, err := e.m.Cleanup(mg.ID); status(err) != http.StatusConflict {
		t.Fatalf("reverted: %v", err)
	}
	// A move has no originals to remove.
	mv := e.run(Request{Source: ProgVSCode, Mode: ModeMove})
	if _, err := e.m.Cleanup(mv.ID); status(err) != http.StatusConflict || mv.CanCleanup {
		t.Fatalf("move: %v %+v", err, mv)
	}
}

func TestCleanupIsNotOfferedForRuntimesAndDatabases(t *testing.T) {
	e := setup(t)
	rt := e.registered("node", "22.1.0", map[string]string{"node.exe": "exe"}, "node.exe")
	mg := e.run(Request{Source: "runtime:" + rt.ID, Mode: ModeCopy})
	if mg.CanCleanup {
		t.Fatalf("a runtime: %+v", mg)
	}
	if _, err := e.m.Cleanup(mg.ID); status(err) != http.StatusConflict {
		t.Fatalf("runtime cleanup: %v", err)
	}
}

func TestCleanupOfARegistryKey(t *testing.T) {
	e := setup(t)
	e.install("heidisql")
	e.reg.keys[heidiKey] = heidiValues()
	write(t, filepath.Join(e.m.Home, "heidisql", "portable_settings.txt"), "")
	mg := e.run(Request{Source: ProgHeidiSQL, Mode: ModeCopy})
	if !mg.CanCleanup {
		t.Fatalf("copy: %+v", mg)
	}
	mg = e.cleanup(mg.ID)
	if _, ok := e.reg.keys[heidiKey]; ok {
		t.Fatal("the key is deleted")
	}
	if m, _ := filepath.Glob(filepath.Join(e.m.Home, "migrations", mg.ID, "backup-original", "*", "HeidiSQL.reg")); len(m) != 1 {
		t.Fatal("a .reg backup of the key")
	}
	e.revert(mg.ID, RevertMoveBack)
	if len(e.reg.keys[heidiKey]) != 6 {
		t.Fatalf("key restored with %d values", len(e.reg.keys[heidiKey]))
	}
}

func TestBackupsAreCountedAndDeleted(t *testing.T) {
	e := setup(t)
	e.install("vscode")
	e.vscodeTree()
	target := filepath.Join(e.m.Home, "code", "user-data", "User")
	write(t, filepath.Join(target, "old.json"), "0123456789")
	mg := e.run(Request{Source: ProgVSCode, Mode: ModeCopy})
	if mg.BackupBytes != 10 {
		t.Fatalf("backupBytes %d, want the 10 bytes of old.json", mg.BackupBytes)
	}
	got, err := e.m.DeleteBackups(mg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BackupBytes != 0 || got.State != StateDone {
		t.Fatalf("after: %+v", got)
	}
	if m, _ := filepath.Glob(filepath.Join(e.m.Home, "migrations", mg.ID, "backup-target", "*")); len(m) != 0 {
		t.Fatalf("backup folder still there: %v", m)
	}
	// The portable data stays and Revert still works (it just has nothing to put back).
	e.revert(mg.ID, RevertDiscard)
	if exists(filepath.Join(target, "old.json")) {
		t.Fatal("old.json was deleted with the backups")
	}
}

func TestDeleteRecordOnlyWhenRevertedOrFailed(t *testing.T) {
	e := setup(t)
	e.install("vscode")
	e.vscodeTree()
	mg := e.run(Request{Source: ProgVSCode, Mode: ModeCopy})
	if err := e.m.DeleteRecord(mg.ID); status(err) != http.StatusConflict {
		t.Fatalf("done: %v", err)
	}
	e.revert(mg.ID, RevertDiscard)
	if err := e.m.DeleteRecord(mg.ID); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(e.m.Home, "migrations", mg.ID)) {
		t.Fatal("the folder is removed")
	}
	if err := e.m.DeleteRecord(mg.ID); status(err) != http.StatusNotFound {
		t.Fatalf("again: %v", err)
	}
	if list, _ := e.m.List(); len(list) != 0 {
		t.Fatalf("history %+v", list)
	}
}

func TestSourcesReportFreeSpace(t *testing.T) {
	e := setup(t)
	e.m.Free = func(p string) int64 {
		if strings.HasPrefix(p, e.m.Bin) {
			return 7
		}
		return 42
	}
	e.registered("node", "22.1.0", map[string]string{"node.exe": "exe"}, "node.exe")
	if got := e.source(ProgVSCode).TargetFreeBytes; got != 42 {
		t.Fatalf("data drive free %d", got)
	}
	if got := e.source("runtime:rt_node1").TargetFreeBytes; got != 7 {
		t.Fatalf("bin drive free %d", got)
	}
	e.m.Free = nil
	if got := e.source(ProgVSCode).TargetFreeBytes; got != 0 {
		t.Fatalf("unknown is 0, got %d", got)
	}
}
