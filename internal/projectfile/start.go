package projectfile

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/newproject"
	"zero2dev/internal/store"
)

const stepChecking = "Checking the plan"

// Start runs the import as a job (type "project-import"; job.done carries projectId and path, the
// project's folder). It answers the same problems as Plan with a 409, so a stale plan cannot start.
func (m *Manager) Start(req ImportRequest) (model.Job, error) {
	if m.St == nil || m.Jobs == nil {
		return model.Job{}, errors.New("project import is not available in this build")
	}
	p, err := m.plan(req)
	if err != nil {
		return model.Job{}, err
	}
	if len(p.Problems) > 0 {
		return model.Job{}, apperr.Conflict("%s", p.Problems[0])
	}
	if len(p.Installs) > 0 && m.Installer == nil {
		return model.Job{}, apperr.Conflict("%s must be installed first, but installing is not available", p.Installs[0].Name)
	}
	key := "projectimport:" + strings.ToLower(p.Path)
	return m.Jobs.Go(model.JobProjectImport, key, stepChecking, nil, func(h *jobs.Handle) error {
		return m.run(h, req)
	})
}

// run is the job: check again, install what is missing, then create everything in one change.
func (m *Manager) run(h *jobs.Handle, req ImportRequest) (err error) {
	created := false
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the job crashed: %v", r)
		}
		if err != nil && !created {
			err = errors.New(strings.TrimRight(err.Error(), ". ") + ". Nothing was created.")
		}
	}()
	h.Progress(2, stepChecking)
	p, err := m.plan(req)
	if err != nil {
		return err
	}
	if len(p.Problems) > 0 {
		return errors.New(p.Problems[0])
	}

	for i, in := range p.Installs {
		step := "Installing " + in.Name
		report := func(pct int) { h.Progress(5+80*(i*100+pct)/(len(p.Installs)*100), step) }
		h.Progress(5+80*i*100/(len(p.Installs)*100), step)
		if err := newproject.InstallAndWait(m.Installer, m.St, in.CatalogID, m.poll(), m.installWait(), report); err != nil {
			return fmt.Errorf("Installing %s failed: %v", in.Name, err)
		}
	}

	h.Progress(90, "Creating the project")
	var b *built
	err = m.St.Update(func(d *store.Data) error {
		var probs []string
		b, probs = m.build(d, p.file, p.Name, p.Dir, false)
		if len(probs) > 0 {
			return errors.New(probs[0])
		}
		return nil
	})
	if err != nil {
		return err
	}
	created = true
	m.emit("services.changed")
	if len(b.sites) > 0 {
		m.emit("sites.changed")
		if m.Sites != nil {
			_, _ = m.Sites.EnsureServers()
			_ = m.Sites.Regenerate()
		}
	}
	m.emit("projects.changed")
	if m.Sup != nil {
		for _, id := range b.project.ServiceIDs {
			m.Sup.Log(id, "system", "created from "+p.Path)
		}
	}
	h.Set(func(j *model.Job) { j.ProjectID, j.Path = b.project.ID, p.Dir })
	return nil
}

func (m *Manager) poll() time.Duration {
	if m.PollEvery > 0 {
		return m.PollEvery
	}
	return 300 * time.Millisecond
}

func (m *Manager) installWait() time.Duration {
	if m.InstallWait > 0 {
		return m.InstallWait
	}
	return 30 * time.Minute
}
