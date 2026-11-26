package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/fsutil"
	"zero2dev/internal/jobs"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// owner is what a snapshot is taken of.
type owner struct {
	scope   string
	id      string
	project *model.Project
	members []string // service ids, in the project's order
	sites   []model.Site
}

// resolveOwner checks that the project or database service exists now.
func resolveOwner(scope, id string, d store.Data) (owner, error) {
	o := owner{scope: scope, id: id, sites: []model.Site{}}
	switch scope {
	case ScopeProject:
		i := slices.IndexFunc(d.Projects, func(p model.Project) bool { return p.ID == id })
		if i < 0 {
			return o, apperr.NotFound("project %s not found", id)
		}
		p := d.Projects[i]
		p.ServiceIDs, p.SiteIDs = append([]string{}, p.ServiceIDs...), append([]string{}, p.SiteIDs...)
		o.project = &p
		for _, sid := range p.ServiceIDs {
			if slices.ContainsFunc(d.Services, func(sv model.Service) bool { return sv.ID == sid }) && !slices.Contains(o.members, sid) {
				o.members = append(o.members, sid)
			}
		}
		for _, sid := range p.SiteIDs {
			if j := slices.IndexFunc(d.Sites, func(s model.Site) bool { return s.ID == sid }); j >= 0 {
				o.sites = append(o.sites, d.Sites[j])
			}
		}
	case ScopeService:
		i := slices.IndexFunc(d.Services, func(sv model.Service) bool { return sv.ID == id })
		if i < 0 {
			return o, apperr.NotFound("service %s not found", id)
		}
		if !model.IsDatabase(d.Services[i].Kind) {
			return o, apperr.BadRequest("%s is not a database", d.Services[i].Name)
		}
		o.members = []string{id}
	default:
		return o, apperr.BadRequest("scope must be project or service")
	}
	return o, nil
}

func jobInit(scope, ownerID, snapID string) func(*model.Job) {
	return func(j *model.Job) {
		j.SnapshotID = snapID
		if scope == ScopeProject {
			j.ProjectID = ownerID
		} else {
			j.ServiceID = ownerID
		}
	}
}

// Take starts a snapshot job for a project (scope project) or a database
// service (scope service).
func (m *Manager) Take(scope, ownerID string, opt TakeOptions) (model.Job, error) {
	d := m.St.Snapshot()
	o, err := resolveOwner(scope, ownerID, d)
	if err != nil {
		return model.Job{}, err
	}
	name, err := checkName(opt.Name)
	if err != nil {
		return model.Job{}, err
	}
	names, running, busy := m.activeMembers(o.members, d)
	if busy != nil {
		return model.Job{}, busy
	}
	if len(running) > 0 && !opt.Restart {
		return model.Job{}, runningError(names, running, "take the snapshot")
	}
	id := model.NewID("snp_")
	step := StepCopying
	if len(running) > 0 {
		step = StepStopping
	}
	return m.Jobs.Go(model.JobSnapshot, "snapshot:"+ownerID, step, jobInit(scope, ownerID, id), func(h *jobs.Handle) error {
		return m.take(h, o, id, name, running)
	})
}

func (m *Manager) take(h *jobs.Handle, o owner, id, name string, running []string) error {
	if len(running) > 0 {
		h.Progress(0, StepStopping)
		if err := m.stopAll(o.scope == ScopeProject, o.members); err != nil {
			m.startAgain(running)
			return err
		}
	}
	release, err := m.Sup.Reserve(o.members, "taking a snapshot")
	if err != nil {
		m.startAgain(running)
		return err
	}
	h.Progress(0, StepCopying)
	err = m.write(h, o, id, name)
	release()
	if len(running) > 0 {
		h.Progress(99, StepStarting)
		m.startAgain(running)
	}
	if err != nil {
		return err
	}
	m.emit("snapshots.changed", map[string]string{"ownerId": o.id})
	return nil
}

// copyJob is one folder or file copied by a job.
type copyJob struct{ src, dst string }

// copyAll copies every job with one percent over all of them (the job's
// Progress with step), and reports the first error naming what failed.
func (m *Manager) copyAll(h *jobs.Handle, step string, list []copyJob) error {
	var total int64
	for _, c := range list {
		n, err := fsutil.DirSize(c.src)
		if err != nil {
			return err
		}
		total += n
	}
	var base int64
	for _, c := range list {
		if err := os.MkdirAll(filepath.Dir(c.dst), 0o755); err != nil {
			return err
		}
		var last int64
		err := m.copy(c.src, c.dst, func(done, _ int64) {
			last = done
			if total > 0 {
				h.Progress(int((base+done)*100/total), step)
			}
		})
		if err != nil {
			return fmt.Errorf("copying %s: %w", c.src, err)
		}
		base += last
	}
	return nil
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// phpIni is where a PHP service's generated php.ini is.
func (m *Manager) phpIni(id string) string { return kinds.PHPIniPath(m.Sup.DataDir("php", id)) }

// write copies the members into <id>.partial, writes the manifest and renames
// the folder to <id>. On failure nothing is left behind.
func (m *Manager) write(h *jobs.Handle, o owner, id, name string) error {
	d := m.St.Snapshot()
	dir := filepath.Join(m.root(), o.id, id)
	partial := dir + ".partial"
	_ = os.RemoveAll(partial)
	if err := os.MkdirAll(partial, 0o755); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(partial)
			_ = os.Remove(filepath.Dir(dir)) // the owner's folder, when nothing else is in it
		}
	}()
	mf := Manifest{
		Format: Format, ID: id, Name: name, Scope: o.scope, OwnerID: o.id, CreatedAt: time.Now().UTC(),
		Version: m.Version, Project: o.project, Sites: o.sites, Services: []ServiceEntry{}, Warnings: []string{},
	}
	var copies []copyJob
	for _, sid := range o.members {
		i := slices.IndexFunc(d.Services, func(sv model.Service) bool { return sv.ID == sid })
		if i < 0 {
			continue // deleted meanwhile (cannot happen while reserved, but be safe)
		}
		sv := d.Services[i]
		e := ServiceEntry{Service: cleared(sv)}
		var warn string
		if e.Runtime, warn = m.runtimeRef(sv); warn != "" {
			mf.Warnings = append(mf.Warnings, warn)
		}
		if model.IsDatabase(sv.Kind) {
			if sv.CustomArgs {
				mf.Warnings = append(mf.Warnings, sv.Name+" uses its own command line, so its data folder is not known and was not copied")
			} else if src := m.Sup.DataDir(sv.Kind, sv.ID); isDir(src) {
				copies = append(copies, copyJob{src, filepath.Join(partial, "data", sv.ID)})
				e.HasData = true
			}
		}
		if sv.Kind == "php" {
			if ini := m.phpIni(sv.ID); isFile(ini) {
				copies = append(copies, copyJob{ini, filepath.Join(partial, "php", sv.ID, "php.ini")})
				e.PHPIni = true
			}
		}
		// A Python service virtual environment (python/venvs in the data folder) is never copied: it holds absolute paths.
		// The first start after a restore or a clone makes an empty one (the Install requirements task fills it).
		mf.Services = append(mf.Services, e)
	}
	if err := m.copyAll(h, StepCopying, copies); err != nil {
		return apperr.Internal("the snapshot could not be taken: %v", err)
	}
	size, err := fsutil.DirSize(partial)
	if err != nil {
		return err
	}
	mf.SizeBytes = size
	if err := writeManifest(partial, mf); err != nil {
		return err
	}
	if err := os.Rename(partial, dir); err != nil {
		return apperr.Internal("the snapshot could not be saved: %v", err)
	}
	ok = true
	for _, e := range mf.Services {
		m.Sup.Log(e.Service.ID, "system", fmt.Sprintf("snapshot %q (%s) taken", name, id))
	}
	return nil
}
