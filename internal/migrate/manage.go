package migrate

import (
	"fmt"
	"os"
	"path/filepath"

	"zero2dev/internal/apperr"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
)

// Managing what a migration left behind: the free space it needs, the originals
// of a copy (Cleanup), the backups it made (DeleteBackups) and its record
// (DeleteRecord).

// free is the free bytes of the drive holding path (0: unknown).
func (m *Manager) free(path string) int64 {
	if path == "" {
		return 0
	}
	if m.Free != nil {
		return m.Free(path)
	}
	if !m.production() {
		return 0 // tests never ask the real drive
	}
	return diskFree(path)
}

// backupBytes is what the backup folders of a migration hold.
func (m *Manager) backupBytes(r *record) int64 {
	var n int64
	for _, d := range []string{"backup-target", "backup-original"} {
		sz, _ := treeSize(filepath.Join(m.migDir(r.ID), d), nil)
		n += sz
	}
	return n
}

// originalsLeft is the steps of a done copy whose original is still in place.
func (m *Manager) originalsLeft(r *record) (steps []int, bytes int64) {
	if r.State != StateDone || r.Mode != ModeCopy || r.Program == ProgRuntime || r.Program == ProgDatabase {
		return nil, 0
	}
	for i, s := range r.Steps {
		if !s.Done || s.Moved {
			continue
		}
		switch s.Kind {
		case KindDir, KindFile:
			if !exists(s.From) {
				continue
			}
		case KindRegistry:
			if _, ok, err := m.reg().ReadTree(s.RegPath); err != nil || !ok {
				continue
			}
		default:
			continue
		}
		steps = append(steps, i)
		bytes += s.SizeBytes
	}
	return steps, bytes
}

// view is the record as the API shows it, with the numbers that are worked out
// when it is asked for.
func (m *Manager) view(r *record) Migration {
	mg := r.Migration
	if mg.State == StateRunning {
		mg.State = StateFailed
	}
	mg.BackupBytes = m.backupBytes(r)
	if steps, n := m.originalsLeft(r); len(steps) > 0 {
		mg.CanCleanup, mg.OriginalsBytes = true, n
	}
	return mg
}

// verifyCopy checks that the portable copy of step s is complete, so removing
// the original loses nothing.
func (m *Manager) verifyCopy(s step) error {
	switch s.Kind {
	case KindRegistry:
		vals, ok, err := m.reg().ReadTree(s.RegPath)
		if err != nil || !ok {
			return fmt.Errorf("cannot read HKCU\\%s", s.RegPath)
		}
		return checkPortable(s.To, vals)
	default:
		if !exists(s.To) {
			return apperr.Conflict("%s is gone, so the original %s is the only copy", s.To, s.From)
		}
		if s.Rewrite {
			// extensions.json was rewritten to the new place: compare the files only.
			sf, _, err := treeStats(s.From, nil)
			if err != nil {
				return err
			}
			df, _, err := treeStats(s.To, nil)
			if err != nil {
				return err
			}
			if sf != df {
				return fmt.Errorf("the copy in %s is incomplete (%d files instead of %d)", s.To, df, sf)
			}
			return nil
		}
		return verify(s.From, s.To, s.Skip)
	}
}

// Cleanup starts a migrate-cleanup job that removes the originals of a done
// copy. The copy is checked first; a failed check leaves everything as it was.
// Afterwards the migration is a move (its originals are gone), so Revert offers
// "move back".
func (m *Manager) Cleanup(id string) (model.Job, error) {
	if m.Jobs == nil {
		return model.Job{}, ErrNotImplemented
	}
	rec, err := m.load(id)
	if err != nil {
		return model.Job{}, err
	}
	if m.isActive(id) {
		return model.Job{}, apperr.Conflict("migration %s is still running", id)
	}
	if rec.Program == ProgRuntime {
		return model.Job{}, apperr.Conflict("%s", runtimeCopyOnly)
	}
	if rec.Program == ProgDatabase {
		return model.Job{}, apperr.Conflict("%s", databaseCopyOnly)
	}
	if rec.State != StateDone || rec.Mode != ModeCopy {
		return model.Job{}, apperr.Conflict("only a finished copy has originals to remove")
	}
	steps, _ := m.originalsLeft(rec)
	if len(steps) == 0 {
		return model.Job{}, apperr.Conflict("the originals of %s are gone already", rec.Label)
	}
	if m.running(rec.Image) {
		return model.Job{}, apperr.Conflict("Close %s first", rec.Label)
	}
	h, err := m.Jobs.Start(model.JobMigrateCleanup, "migrate:"+rec.Source, "Checking the copy", func(j *model.Job) { j.MigrationID = rec.ID })
	if err != nil {
		return model.Job{}, err
	}
	m.setActive(rec.ID, true)
	job := h.Job()
	m.goJob(h, rec, func() error { return m.removeOriginals(h, rec, steps) })
	return job, nil
}

func (m *Manager) removeOriginals(h *jobs.Handle, rec *record, steps []int) error {
	// Check every copy before anything is removed.
	for k, i := range steps {
		h.Progress(k*50/len(steps), "Checking "+rec.Steps[i].Label)
		if err := m.verifyCopy(rec.Steps[i]); err != nil {
			return fmt.Errorf("nothing was removed: %w", err)
		}
	}
	for k, i := range steps {
		s := &rec.Steps[i]
		h.Progress(50+k*50/len(steps), "Removing the old "+s.Label)
		switch s.Kind {
		case KindRegistry:
			vals, ok, err := m.reg().ReadTree(s.RegPath)
			if err != nil || !ok {
				return fmt.Errorf("cannot read HKCU\\%s", s.RegPath)
			}
			rel, err := m.saveKey(rec, i, "backup-original", vals)
			if err != nil {
				return err
			}
			s.OriginalBackup = rel
			if err := m.save(rec); err != nil {
				return err
			}
			if err := m.reg().DeleteTree(s.RegPath); err != nil {
				return fmt.Errorf("cannot remove HKCU\\%s: %w", s.RegPath, err)
			}
		default:
			if err := os.RemoveAll(s.From); err != nil {
				return fmt.Errorf("cannot remove %s (is a program using it?): %w", s.From, err)
			}
		}
		s.Moved = true
		if err := m.save(rec); err != nil {
			return err
		}
	}
	rec.Mode = ModeMove
	rec.RevertHow = revertOptions(rec)
	rec.Description = describe(rec)
	return m.save(rec)
}

// DeleteBackups removes the backups of a migration (what the portable place
// held before, and what a revert saved). A revert then no longer brings those
// back. It does not touch the portable data or the originals.
func (m *Manager) DeleteBackups(id string) (Migration, error) {
	rec, err := m.load(id)
	if err != nil {
		return Migration{}, err
	}
	if m.isActive(id) {
		return Migration{}, apperr.Conflict("migration %s is still running", id)
	}
	for _, d := range []string{"backup-target", "backup-original"} {
		if err := os.RemoveAll(filepath.Join(m.migDir(id), d)); err != nil {
			return Migration{}, apperr.Internal("cannot remove the backups: %v", err)
		}
	}
	for i := range rec.Steps {
		rec.Steps[i].TargetBackup, rec.Steps[i].OriginalBackup, rec.Steps[i].RevertBackups = "", "", nil
	}
	if err := m.save(rec); err != nil {
		return Migration{}, apperr.Internal("cannot write the migration record: %v", err)
	}
	m.emit("migrations.changed")
	return m.view(rec), nil
}

// DeleteRecord removes a reverted or failed migration, with its backups, from
// the history.
func (m *Manager) DeleteRecord(id string) error {
	rec, err := m.load(id)
	if err != nil {
		return err
	}
	if m.isActive(id) {
		return apperr.Conflict("migration %s is still running", id)
	}
	if rec.State == StateDone || (rec.State == StateRunning) || len(rec.RevertHow) > 0 {
		return apperr.Conflict("revert %s first: its data is still in use", rec.Label)
	}
	if err := os.RemoveAll(m.migDir(id)); err != nil {
		return apperr.Internal("cannot remove the record: %v", err)
	}
	m.emit("migrations.changed")
	return nil
}
