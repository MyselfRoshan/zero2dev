package jobs

import (
	"errors"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/model"
)

type rec struct {
	mu     sync.Mutex
	events []string
	last   map[string]any
}

func (r *rec) Publish(typ string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, typ)
	r.last, _ = data.(map[string]any)
}

func TestJobLifecycle(t *testing.T) {
	em := &rec{}
	r := New(em)
	h, err := r.Start(model.JobDBImport, "db:svc_1", "Reading", func(j *model.Job) { j.ServiceID = "svc_1" })
	if err != nil {
		t.Fatal(err)
	}
	if j, ok := r.Get(h.ID()); !ok || j.State != "running" || j.ServiceID != "svc_1" || j.Step != "Reading" {
		t.Fatalf("started job: %+v %v", j, ok)
	}
	// the same key is refused while running, another key is fine
	if _, err := r.Start(model.JobDBExport, "db:svc_1", "", nil); err == nil {
		t.Fatal("a second job on the same key must be refused")
	}
	if _, err := r.Start(model.JobDBExport, "db:svc_2", "", nil); err != nil {
		t.Fatal(err)
	}
	h.Progress(150, "Importing")
	if j, _ := r.Get(h.ID()); j.Percent != 99 {
		t.Fatalf("percent is capped at 99 while running: %d", j.Percent)
	}
	h.Progress(99, "Importing") // unchanged: no event
	h.Set(func(j *model.Job) { j.Path = "C:\\x.sql" })
	h.Done()
	h.Fail(errors.New("late")) // ignored
	j, _ := r.Get(h.ID())
	if j.State != "done" || j.Percent != 100 || j.Error != "" || j.Path != "C:\\x.sql" {
		t.Fatalf("finished job: %+v", j)
	}
	em.mu.Lock()
	got := append([]string(nil), em.events...)
	last := em.last
	em.mu.Unlock()
	if len(got) != 2 || got[0] != "job.progress" || got[1] != "job.done" {
		t.Fatalf("events %v", got)
	}
	if last["state"] != "done" || last["serviceId"] != "svc_1" || last["type"] != model.JobDBImport {
		t.Fatalf("job.done data %v", last)
	}
	// finished: the key is free again
	if _, err := r.Start(model.JobDBImport, "db:svc_1", "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestGoFailsAndForgets(t *testing.T) {
	r := New(nil)
	now := time.Now()
	r.now = func() time.Time { return now }
	j, err := r.Go(model.JobSnapshot, "snap:prj_1", "Copying", nil, func(h *Handle) error { return errors.New("disk full") })
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, _ := r.Get(j.ID)
		if got.State == "failed" {
			if got.Error != "disk full" {
				t.Fatalf("error %q", got.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not fail: %+v", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	j2, _ := r.Go(model.JobSnapshot, "", "", nil, func(*Handle) error { panic("boom") })
	for i := 0; i < 200; i++ {
		if got, _ := r.Get(j2.ID); got.State == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got, _ := r.Get(j2.ID); got.State != "failed" {
		t.Fatalf("a panicking job fails: %+v", got)
	}
	// an hour later, a new Start forgets finished jobs
	now = now.Add(2 * time.Hour)
	if _, err := r.Start(model.JobSnapshot, "x", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Get(j.ID); ok {
		t.Fatal("finished jobs are forgotten after an hour")
	}
}
