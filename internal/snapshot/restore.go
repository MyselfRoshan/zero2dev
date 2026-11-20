package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"zero2dev/internal/apperr"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// domainClash is the 409 for a snapshot site whose domain another site uses now.
func domainClash(sites []model.Site, d store.Data) error {
	for _, s := range sites {
		for _, o := range d.Sites {
			if o.Domain == s.Domain && o.ID != s.ID {
				return apperr.Conflict("domain %s is now used by site %q; change or delete that site first", s.Domain, o.Name)
			}
		}
	}
	return nil
}

func entryIDs(mf Manifest) []string {
	ids := make([]string, 0, len(mf.Services))
	for _, e := range mf.Services {
		ids = append(ids, e.Service.ID)
	}
	return ids
}

// Restore starts a snapshot-restore job. It is a 409 while a needed runtime is
// missing, or while a service runs and restart is false.
func (m *Manager) Restore(id string, restart bool) (model.Job, error) {
	dir, mf, err := m.find(id)
	if err != nil {
		return model.Job{}, err
	}
	d := m.St.Snapshot()
	if miss := m.missing(mf, d); len(miss) > 0 {
		return model.Job{}, missingError(miss)
	}
	if err := domainClash(mf.Sites, d); err != nil {
		return model.Job{}, err
	}
	members := entryIDs(mf)
	names, running, busy := m.activeMembers(members, d)
	if busy != nil {
		return model.Job{}, busy
	}
	if len(running) > 0 && !restart {
		return model.Job{}, runningError(names, running, "restore")
	}
	step := StepRestoring
	if len(running) > 0 {
		step = StepStopping
	}
	unclaim := m.claim(id, "restoring it")
	j, err := m.Jobs.Go(model.JobSnapshotRestore, "snapshot:"+mf.OwnerID, step, jobInit(mf.Scope, mf.OwnerID, id), func(h *jobs.Handle) error {
		defer unclaim()
		return m.restore(h, dir, mf, members, running)
	})
	if err != nil {
		unclaim()
	}
	return j, err
}

func (m *Manager) restore(h *jobs.Handle, dir string, mf Manifest, members, running []string) error {
	if len(running) > 0 {
		h.Progress(0, StepStopping)
		if err := m.stopAll(mf.Scope == ScopeProject, running); err != nil {
			m.startAgain(running)
			return err
		}
	}
	release, err := m.Sup.Reserve(members, "restoring a snapshot")
	if err != nil {
		m.startAgain(running)
		return err
	}
	h.Progress(0, StepRestoring)
	err = m.restoreFiles(h, dir, mf)
	release()
	if err == nil {
		if m.Sites != nil {
			_ = m.Sites.Regenerate()
		}
		m.emit("services.changed", struct{}{})
		m.emit("sites.changed", struct{}{})
		m.emit("projects.changed", struct{}{})
		m.emit("snapshots.changed", map[string]string{"ownerId": mf.OwnerID})
		for _, e := range mf.Services {
			m.Sup.Log(e.Service.ID, "system", fmt.Sprintf("snapshot %q (%s) restored", mf.Name, mf.ID))
		}
	}
	if len(running) > 0 {
		h.Progress(99, StepStarting)
		m.startAgain(running)
	}
	return err
}

// swap is one data folder being replaced: the current one is renamed to
// old, the snapshot's copy goes to target, and old is deleted at the end (or
// renamed back when anything fails).
type swap struct {
	target, old string
	moved       bool
}

// iniBackup is the php.ini a restore overwrote.
type iniBackup struct {
	path string
	old  []byte
	had  bool
}

// restoreFiles puts the data folders and php.ini files back, then the
// settings, sites and project. When any step fails, every data folder and
// php.ini is put back as it was and the state is not changed.
func (m *Manager) restoreFiles(h *jobs.Handle, dir string, mf Manifest) (err error) {
	var swaps []*swap
	var inis []iniBackup
	defer func() {
		if err == nil {
			for _, s := range swaps {
				if s.moved {
					_ = os.RemoveAll(s.old)
				}
			}
			return
		}
		for i := len(swaps) - 1; i >= 0; i-- {
			s := swaps[i]
			_ = os.RemoveAll(s.target)
			if s.moved {
				if e := os.Rename(s.old, s.target); e != nil {
					err = errors.Join(err, fmt.Errorf("could not put %s back (it is in %s): %w", s.target, s.old, e))
				}
			}
		}
		for _, b := range inis {
			if b.had {
				_ = os.WriteFile(b.path, b.old, 0o644)
			} else {
				_ = os.Remove(b.path)
			}
		}
	}()

	var copies []copyJob
	for _, e := range mf.Services {
		if !e.HasData {
			continue
		}
		target := m.Sup.DataDir(e.Service.Kind, e.Service.ID)
		old := target + ".restore-old"
		if _, err := os.Stat(old); err == nil {
			return apperr.Conflict("%s is left over from an earlier restore; move it away first", old)
		}
		copies = append(copies, copyJob{filepath.Join(dir, "data", e.Service.ID), target})
		swaps = append(swaps, &swap{target: target, old: old})
	}
	for _, s := range swaps {
		if _, err := os.Stat(s.target); err == nil {
			if err := os.Rename(s.target, s.old); err != nil {
				return apperr.Conflict("could not move %s aside (is a program using it?): %v", s.target, err)
			}
			s.moved = true
		}
	}
	if err := m.copyAll(h, StepRestoring, copies); err != nil {
		return apperr.Internal("the snapshot could not be restored: %v", err)
	}
	for _, e := range mf.Services {
		if !e.PHPIni {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "php", e.Service.ID, "php.ini"))
		if err != nil {
			return apperr.Internal("the snapshot's php.ini of %s cannot be read: %v", e.Service.Name, err)
		}
		path := m.phpIni(e.Service.ID)
		bk := iniBackup{path: path}
		if old, err := os.ReadFile(path); err == nil {
			bk.old, bk.had = old, true
		}
		inis = append(inis, bk)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return apperr.Internal("could not write %s: %v", path, err)
		}
	}
	return m.St.Update(func(d *store.Data) error { return applyManifest(d, mf) })
}

// applyManifest writes the snapshot's services, sites and project into d:
// every record by id (replaced, or added again when it was deleted), each
// service pinned to the runtime it used, and the project's lists set to the
// snapshot's, which takes later additions out of the project.
func applyManifest(d *store.Data, mf Manifest) error {
	if err := domainClash(mf.Sites, *d); err != nil {
		return err
	}
	for _, e := range mf.Services {
		sv := cleared(e.Service)
		sv.State = model.StateStopped
		if rt := matchRuntime(e.Runtime, d.Runtimes); rt != nil {
			sv.RuntimeID = rt.ID
		}
		if i := slices.IndexFunc(d.Services, func(o model.Service) bool { return o.ID == sv.ID }); i >= 0 {
			d.Services[i] = sv
		} else {
			d.Services = append(d.Services, sv)
		}
	}
	for _, s := range mf.Sites {
		if i := slices.IndexFunc(d.Sites, func(o model.Site) bool { return o.ID == s.ID }); i >= 0 {
			d.Sites[i] = s
		} else {
			d.Sites = append(d.Sites, s)
		}
	}
	if mf.Project != nil {
		p := *mf.Project
		p.ServiceIDs, p.SiteIDs = append([]string{}, p.ServiceIDs...), append([]string{}, p.SiteIDs...)
		if i := slices.IndexFunc(d.Projects, func(o model.Project) bool { return o.ID == p.ID }); i >= 0 {
			d.Projects[i] = p
		} else {
			d.Projects = append(d.Projects, p)
		}
	}
	return nil
}
