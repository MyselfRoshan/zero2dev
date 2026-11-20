// Package jobs keeps the long operations that are not runtime installs: a
// database import or export, a snapshot, a restore, a clone, a migration. A job
// runs in its own goroutine and reports through the same events as the
// installer's jobs (job.progress, job.done), so the dashboard follows both the
// same way. Jobs are kept in memory only; a finished one is forgotten after an
// hour.
package jobs

import (
	"sync"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
)

// Emitter publishes events (events.Hub).
type Emitter interface {
	Publish(typ string, data any)
}

// Registry holds the jobs. The zero value is not usable; call New.
type Registry struct {
	em  Emitter
	now func() time.Time

	mu   sync.Mutex
	jobs map[string]*entry
}

type entry struct {
	job      model.Job
	key      string
	finished time.Time
}

// New returns an empty registry that publishes through em (may be nil).
func New(em Emitter) *Registry {
	return &Registry{em: em, now: time.Now, jobs: map[string]*entry{}}
}

// Handle is how a running job reports. All methods are safe for concurrent use.
type Handle struct {
	r *Registry
	e *entry
}

// Start registers a running job of type typ (model.JobDBImport ...). key names
// what the job works on (e.g. "db:svc_1" or "snapshot:prj_1"); while a job with
// the same key runs, Start answers 409 naming it. init (may be nil) fills the
// job's fields (ServiceID, ProjectID ...) before anyone can see it.
func (r *Registry) Start(typ, key, step string, init func(*model.Job)) (*Handle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, e := range r.jobs {
		if !e.finished.IsZero() && r.now().Sub(e.finished) > time.Hour {
			delete(r.jobs, id)
		}
	}
	if key != "" {
		for _, e := range r.jobs {
			if e.key == key && e.job.State == "running" {
				return nil, apperr.Conflict("another job is already working on this (%s, %s)", e.job.Type, e.job.ID).With("jobId", e.job.ID)
			}
		}
	}
	e := &entry{job: model.Job{ID: model.NewID("job_"), Type: typ, State: "running", Step: step}, key: key}
	if init != nil {
		init(&e.job)
	}
	r.jobs[e.job.ID] = e
	return &Handle{r: r, e: e}, nil
}

// Go is Start followed by running fn in a new goroutine; fn's error fails the
// job, nil finishes it. It returns the job as it is right after starting.
func (r *Registry) Go(typ, key, step string, init func(*model.Job), fn func(h *Handle) error) (model.Job, error) {
	h, err := r.Start(typ, key, step, init)
	if err != nil {
		return model.Job{}, err
	}
	j := h.Job()
	go func() {
		defer func() {
			if p := recover(); p != nil {
				h.Fail(apperr.Internal("job crashed: %v", p))
			}
		}()
		if err := fn(h); err != nil {
			h.Fail(err)
			return
		}
		h.Done()
	}()
	return j, nil
}

// Get returns a copy of the job, and whether it is known.
func (r *Registry) Get(id string) (model.Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.jobs[id]
	if !ok {
		return model.Job{}, false
	}
	return e.job, true
}

// Job returns a copy of the job as it is now.
func (h *Handle) Job() model.Job {
	h.r.mu.Lock()
	defer h.r.mu.Unlock()
	return h.e.job
}

// ID is the job's id.
func (h *Handle) ID() string { return h.Job().ID }

// Progress sets the percent (clamped to 0-99 while running) and the step, and
// publishes job.progress when either changed.
func (h *Handle) Progress(percent int, step string) {
	percent = max(0, min(percent, 99))
	h.r.mu.Lock()
	j := &h.e.job
	if j.State != "running" || (j.Percent == percent && j.Step == step) {
		h.r.mu.Unlock()
		return
	}
	j.Percent, j.Step = percent, step
	id := j.ID
	h.r.mu.Unlock()
	h.r.publish("job.progress", map[string]any{"id": id, "percent": percent, "step": step})
}

// Set changes the job's fields (for example Path or ProjectID once known).
func (h *Handle) Set(fn func(*model.Job)) {
	h.r.mu.Lock()
	fn(&h.e.job)
	h.r.mu.Unlock()
}

// Done finishes the job as done (100 %). Only the first Done or Fail counts.
func (h *Handle) Done() { h.finish(nil) }

// Fail finishes the job as failed with err's message.
func (h *Handle) Fail(err error) { h.finish(err) }

func (h *Handle) finish(err error) {
	h.r.mu.Lock()
	j := &h.e.job
	if j.State != "running" {
		h.r.mu.Unlock()
		return
	}
	h.e.finished = h.r.now()
	if err != nil {
		j.State, j.Error = "failed", err.Error()
	} else {
		j.State, j.Percent = "done", 100
	}
	snap := *j
	h.r.mu.Unlock()
	h.r.publish("job.done", map[string]any{
		"id": snap.ID, "state": snap.State, "error": snap.Error, "runtimeId": snap.RuntimeID,
		"type": snap.Type, "serviceId": snap.ServiceID, "projectId": snap.ProjectID,
		"snapshotId": snap.SnapshotID, "migrationId": snap.MigrationID, "path": snap.Path,
	})
}

func (r *Registry) publish(typ string, data any) {
	if r.em != nil {
		r.em.Publish(typ, data)
	}
}
