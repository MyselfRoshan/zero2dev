package supervisor

import (
	"sync"
	"testing"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// usageEm records the service.usage batches.
type usageEm struct {
	mu      sync.Mutex
	batches [][]model.Usage
}

func (e *usageEm) Publish(typ string, data any) {
	if typ != "service.usage" {
		return
	}
	e.mu.Lock()
	e.batches = append(e.batches, data.([]model.Usage))
	e.mu.Unlock()
}
func (e *usageEm) all() [][]model.Usage {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([][]model.Usage(nil), e.batches...)
}

type fakeUsage struct {
	mu   sync.Mutex
	cpu  map[string]time.Duration
	mem  map[string]uint64
	byTr map[*tree]string
}

func (f *fakeUsage) sample(t *tree) (time.Duration, uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.byTr[t]
	if !ok {
		return 0, 0, false
	}
	return f.cpu[id], f.mem[id], true
}

func newUsageSup(t *testing.T) (*Supervisor, *usageEm, *fakeUsage) {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(home + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	em := &usageEm{}
	f := &fakeUsage{cpu: map[string]time.Duration{}, mem: map[string]uint64{}, byTr: map[*tree]string{}}
	s := New(home, st, em)
	s.Sample, s.NumCPU = f.sample, 4
	return s, em, f
}

// run adds a running service with a fake tree.
func (f *fakeUsage) run(s *Supervisor, id string) *proc {
	tr := &tree{}
	f.mu.Lock()
	f.byTr[tr] = id
	f.mu.Unlock()
	p := &proc{state: model.StateRunning, tree: tr, done: make(chan struct{})}
	s.mu.Lock()
	s.procs[id] = p
	s.mu.Unlock()
	return p
}

func TestUsageFirstSampleHasNoDeltaThenPercentOfMachine(t *testing.T) {
	s, em, f := newUsageSup(t)
	f.run(s, "a")
	t0 := time.Unix(1000, 0)
	f.cpu["a"], f.mem["a"] = 5*time.Second, 340<<20
	s.sampleUsage(t0)
	b := em.all()
	if len(b) != 1 || len(b[0]) != 1 || b[0][0] != (model.Usage{ID: "a", CPU: 0, MemBytes: 340 << 20}) {
		t.Fatalf("first sample: %+v", b)
	}
	// 2 s later the tree used 1 s of CPU on 4 processors: 12.5 % of the machine.
	f.cpu["a"], f.mem["a"] = 6*time.Second, 400<<20
	s.sampleUsage(t0.Add(2 * time.Second))
	got := em.all()[1][0]
	if got.CPU != 12.5 || got.MemBytes != 400<<20 {
		t.Fatalf("second sample: %+v", got)
	}
	// Apply overlays the last sample.
	svc := s.Apply(model.Service{ID: "a"})
	if svc.CPU != 12.5 || svc.MemBytes != 400<<20 {
		t.Fatalf("Apply: %+v", svc)
	}
	if other := s.Apply(model.Service{ID: "zzz"}); other.CPU != 0 || other.MemBytes != 0 {
		t.Fatalf("a service that does not run has no numbers: %+v", other)
	}
}

func TestUsageRoundsToOneDecimalAndClamps(t *testing.T) {
	if v := cpuPercent(333*time.Millisecond, 2*time.Second, 4); v != 4.2 { // 4.1625
		t.Fatalf("rounding: %v", v)
	}
	// More CPU time than wall time times processors (clock skew): 100 at most.
	if v := cpuPercent(30*time.Second, 2*time.Second, 4); v != 100 {
		t.Fatalf("upper clamp: %v", v)
	}
	// The counter went backwards (a sample glitch): 0, never negative.
	if v := cpuPercent(-time.Second, 2*time.Second, 4); v != 0 {
		t.Fatalf("lower clamp: %v", v)
	}
	if v := cpuPercent(time.Second, 0, 4); v != 0 {
		t.Fatalf("no wall time: %v", v)
	}
}

func TestUsageStoppedServiceLeavesTheBatchAndNothingRunningSendsNothing(t *testing.T) {
	s, em, f := newUsageSup(t)
	s.sampleUsage(time.Unix(1000, 0))
	if n := len(em.all()); n != 0 {
		t.Fatalf("no event while nothing runs, got %d", n)
	}
	pa, _ := f.run(s, "a"), f.run(s, "b")
	s.sampleUsage(time.Unix(1002, 0))
	if b := em.all(); len(b) != 1 || len(b[0]) != 2 || b[0][0].ID != "a" || b[0][1].ID != "b" {
		t.Fatalf("both run, sorted by id: %+v", b)
	}
	s.mu.Lock()
	pa.state = model.StateStopped
	s.mu.Unlock()
	s.sampleUsage(time.Unix(1004, 0))
	if b := em.all(); len(b) != 2 || len(b[1]) != 1 || b[1][0].ID != "b" {
		t.Fatalf("a stopped: %+v", b)
	}
	s.mu.Lock()
	s.procs["b"].state = model.StateStopped
	s.mu.Unlock()
	s.sampleUsage(time.Unix(1006, 0))
	b := em.all()
	if len(b) != 3 || len(b[2]) != 0 {
		t.Fatalf("one final empty batch when the last one stops: %+v", b)
	}
	s.sampleUsage(time.Unix(1008, 0))
	if len(em.all()) != 3 {
		t.Fatalf("then nothing again")
	}
	if a := s.Apply(model.Service{ID: "b"}); a.CPU != 0 || a.MemBytes != 0 {
		t.Fatalf("a stopped service shows 0: %+v", a)
	}
}

func TestUsageServicesStartingOrWithoutNumbersAreLeftOut(t *testing.T) {
	s, em, f := newUsageSup(t)
	p := f.run(s, "a")
	p.state = model.StateStarting
	s.procs["nojob"] = &proc{state: model.StateRunning, tree: &tree{}} // the sampler has no numbers
	s.sampleUsage(time.Unix(1000, 0))
	if n := len(em.all()); n != 0 {
		t.Fatalf("nothing to report: %+v", em.all())
	}
}

func TestUsageGoroutineSamplesAndStopsWithStopAll(t *testing.T) {
	s, em, f := newUsageSup(t)
	s.UsageInterval = 5 * time.Millisecond
	f.run(s, "a")
	s.StartUsage()
	s.StartUsage() // idempotent
	deadline := time.Now().Add(3 * time.Second)
	for len(em.all()) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(em.all()) < 2 {
		t.Fatal("the sampler did not publish")
	}
	s.mu.Lock()
	done := s.usageDone
	s.procs["a"].state = model.StateStopped // StopAll has nothing to wait for
	s.mu.Unlock()
	s.StopAll(t.Context())
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the sampler goroutine did not stop")
	}
	n := len(em.all())
	time.Sleep(30 * time.Millisecond)
	if len(em.all()) != n {
		t.Fatal("events after shutdown")
	}
	s.StartUsage() // after shutdown: no new goroutine
	s.mu.Lock()
	same := s.usageDone == done
	s.mu.Unlock()
	if !same {
		t.Fatal("StartUsage after StopAll started a goroutine")
	}
}
