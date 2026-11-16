package supervisor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// crashRecorder collects what OnCrash receives.
type crashRecorder struct {
	mu sync.Mutex
	l  []Crash
}

func (r *crashRecorder) add(c Crash) {
	r.mu.Lock()
	r.l = append(r.l, c)
	r.mu.Unlock()
}

func (r *crashRecorder) all() []Crash {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Crash(nil), r.l...)
}

// restartSetup is setup() for a service with restart on crash and tunables in
// milliseconds. mode "exit" crashes at once, "idle" runs until it is killed.
func restartSetup(t *testing.T, mode string, limit int, base, max, stable time.Duration) (*Supervisor, *store.Store, *recorder, *crashRecorder, string) {
	t.Helper()
	s, st, rec, id := setup(t, mode, 0)
	s.RestartBase, s.RestartMax, s.RestartStable = base, max, stable
	cr := &crashRecorder{}
	s.OnCrash = cr.add
	setRestart(t, st, id, true, limit)
	return s, st, rec, cr, id
}

func setRestart(t *testing.T, st *store.Store, id string, on bool, limit int) {
	t.Helper()
	if err := st.Update(func(d *store.Data) error {
		for i := range d.Services {
			if d.Services[i].ID == id {
				d.Services[i].RestartOnCrash, d.Services[i].RestartLimit = on, limit
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func countLog(s *Supervisor, id, sub string) int {
	n := 0
	for _, l := range s.Logs(id, 500) {
		if l.Stream == "system" && strings.Contains(l.Text, sub) {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func killPid(t *testing.T, s *Supervisor, id string) {
	t.Helper()
	s.mu.Lock()
	pid := s.procs[id].pid
	s.mu.Unlock()
	pr, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := pr.Kill(); err != nil {
		t.Fatal(err)
	}
}

func TestRestartBackoffAndLimit(t *testing.T) {
	s, _, rec, cr, id := restartSetup(t, "exit", 4, 50*time.Millisecond, 200*time.Millisecond, time.Minute)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the service to give up", 10*time.Second, func() bool {
		c := cr.all()
		return len(c) > 0 && c[len(c)-1].GaveUp
	})
	// 50, 100, 200, then 400 capped to 200.
	for i, d := range []string{"50ms", "100ms", "200ms", "200ms"} {
		want := fmt.Sprintf("restarting in %s (attempt %d/4)", d, i+1)
		if countLog(s, id, want) != 1 {
			t.Errorf("log lacks %q: %v", want, s.Logs(id, 100))
		}
	}
	if n := countLog(s, id, "starting:"); n != 5 {
		t.Errorf("the service was started %d times, want 1 + 4 restarts", n)
	}
	cs := cr.all()
	if len(cs) != 5 {
		t.Fatalf("OnCrash called %d times, want 5: %+v", len(cs), cs)
	}
	for i, c := range cs[:4] {
		if c.Attempt != i+1 || c.Limit != 4 || c.GaveUp || c.RetryInSec != 1 || c.ID != id || c.Name != "t" || c.Error == "" {
			t.Errorf("crash %d = %+v", i, c)
		}
	}
	if last := cs[4]; !last.GaveUp || last.Attempt != 4 || last.Limit != 4 || last.RetryInSec != 0 {
		t.Errorf("last crash = %+v, want gave up at 4/4", last)
	}
	if rec.count("service.crashed") != 5 {
		t.Errorf("service.crashed published %d times, want 5", rec.count("service.crashed"))
	}
	// It stays failed, keeps the error and the count, and nothing else comes.
	time.Sleep(500 * time.Millisecond)
	if n := countLog(s, id, "starting:"); n != 5 {
		t.Errorf("started again after giving up: %d starts", n)
	}
	sv := s.Apply(model.Service{ID: id})
	if sv.State != model.StateFailed || sv.LastError == "" || sv.Restarts != 4 {
		t.Errorf("status = %s %q restarts %d, want failed, an error, 4", sv.State, sv.LastError, sv.Restarts)
	}
	if !hasLog(s, id, "system", "giving up") {
		t.Error("no giving-up line in the log")
	}
}

func TestCrashWithoutRestartOnCrash(t *testing.T) {
	s, st, _, cr, id := restartSetup(t, "exit", 3, 20*time.Millisecond, 100*time.Millisecond, time.Minute)
	setRestart(t, st, id, false, 3)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, model.StateFailed, 5*time.Second)
	waitFor(t, "the crash report", 2*time.Second, func() bool { return len(cr.all()) == 1 })
	time.Sleep(200 * time.Millisecond)
	c := cr.all()
	if len(c) != 1 || c[0].Limit != 0 || c[0].GaveUp || c[0].Attempt != 1 || c[0].RetryInSec != 0 {
		t.Fatalf("crash = %+v, want one report without a restart plan", c)
	}
	if n := countLog(s, id, "starting:"); n != 1 {
		t.Errorf("started %d times, want 1", n)
	}
}

// A crash, then Stop all, then longer than the restart delay: it must stay down.
func TestRestartCancelledByStopAll(t *testing.T) {
	s, _, _, cr, id := restartSetup(t, "exit", 5, 300*time.Millisecond, time.Minute, time.Minute)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first crash", 5*time.Second, func() bool { return len(cr.all()) == 1 })
	rep := s.StopAllOrdered()
	if len(rep.Failed) != 0 {
		t.Fatalf("Stop all failed: %+v", rep)
	}
	time.Sleep(900 * time.Millisecond)
	if st := s.State(id); st != model.StateFailed {
		t.Errorf("state = %s after Stop all, want failed", st)
	}
	if n := countLog(s, id, "starting:"); n != 1 {
		t.Errorf("the service came back after Stop all: %d starts", n)
	}
	if r := s.Apply(model.Service{ID: id}).Restarts; r != 0 {
		t.Errorf("restarts = %d after Stop all, want 0", r)
	}
}

func TestRestartCancelledByStop(t *testing.T) {
	s, _, _, cr, id := restartSetup(t, "exit", 5, 300*time.Millisecond, time.Minute, time.Minute)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first crash", 5*time.Second, func() bool { return len(cr.all()) == 1 })
	if err := s.Stop(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(800 * time.Millisecond)
	if n := countLog(s, id, "starting:"); n != 1 {
		t.Errorf("the service came back after Stop: %d starts", n)
	}
}

func TestRestartCancelledByShutdown(t *testing.T) {
	s, _, _, cr, id := restartSetup(t, "exit", 5, 300*time.Millisecond, time.Minute, time.Minute)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first crash", 5*time.Second, func() bool { return len(cr.all()) == 1 })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.StopAll(ctx)
	time.Sleep(800 * time.Millisecond)
	if n := countLog(s, id, "starting:"); n != 1 {
		t.Errorf("the service came back while the daemon closes: %d starts", n)
	}
}

func TestRestartCancelledByRemove(t *testing.T) {
	s, st, _, cr, id := restartSetup(t, "exit", 5, 300*time.Millisecond, time.Minute, time.Minute)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first crash", 5*time.Second, func() bool { return len(cr.all()) == 1 })
	if err := st.Update(func(d *store.Data) error { d.Services = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(800 * time.Millisecond)
	if st := s.State(id); st != model.StateStopped {
		t.Errorf("state = %s after Remove, want stopped (unknown)", st)
	}
}

// If the service is deleted without Remove, the timer still must not start anything.
func TestRestartTimerChecksServiceExists(t *testing.T) {
	s, st, _, cr, id := restartSetup(t, "exit", 5, 300*time.Millisecond, time.Minute, time.Minute)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first crash", 5*time.Second, func() bool { return len(cr.all()) == 1 })
	if err := st.Update(func(d *store.Data) error { d.Services = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(800 * time.Millisecond)
	if n := countLog(s, id, "starting:"); n != 1 {
		t.Errorf("a deleted service was started again: %d starts", n)
	}
}

func TestManualStartCancelsPendingRestart(t *testing.T) {
	s, _, _, cr, id := restartSetup(t, "exit", 5, 400*time.Millisecond, time.Minute, time.Minute)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first crash", 5*time.Second, func() bool { return len(cr.all()) == 1 })
	if err := s.Start(id); err != nil { // by hand, while the timer waits
		t.Fatal(err)
	}
	// The manual run crashes too: a new streak, so this is attempt 1 again, not 2.
	waitFor(t, "the second crash", 5*time.Second, func() bool { return len(cr.all()) == 2 })
	if c := cr.all()[1]; c.Attempt != 1 {
		t.Errorf("crash after a manual start = %+v, want attempt 1 (counter reset)", c)
	}
	// One timer only: the cancelled one must not start a third run in its time.
	time.Sleep(250 * time.Millisecond)
	waitFor(t, "the one real restart", 5*time.Second, func() bool { return countLog(s, id, "starting:") >= 3 })
	time.Sleep(100 * time.Millisecond)
	if n := countLog(s, id, "restarting in 400ms (attempt 1/5)"); n != 2 {
		t.Errorf("attempt-1 restarts planned %d times, want 2 (one per streak)", n)
	}
}

func TestRestartCounterResetsAfterStableRun(t *testing.T) {
	s, _, _, cr, id := restartSetup(t, "idle", 3, 30*time.Millisecond, 100*time.Millisecond, 1500*time.Millisecond)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, model.StateRunning, 5*time.Second)
	killPid(t, s, id)
	waitFor(t, "first crash", 5*time.Second, func() bool { return len(cr.all()) == 1 })
	waitState(t, s, id, model.StateStarting, 5*time.Second)
	waitState(t, s, id, model.StateRunning, 5*time.Second)
	if r := s.Apply(model.Service{ID: id}).Restarts; r != 1 {
		t.Errorf("restarts = %d right after a restart, want 1", r)
	}
	time.Sleep(1600 * time.Millisecond) // longer than RestartStable
	if r := s.Apply(model.Service{ID: id}).Restarts; r != 0 {
		t.Errorf("restarts = %d after a stable run, want 0", r)
	}
	killPid(t, s, id)
	waitFor(t, "second crash", 5*time.Second, func() bool { return len(cr.all()) == 2 })
	if c := cr.all()[1]; c.Attempt != 1 || c.GaveUp {
		t.Errorf("crash after a stable run = %+v, want attempt 1", c)
	}
}

func TestRestartRunsWithoutResetBeforeStable(t *testing.T) {
	s, _, _, cr, id := restartSetup(t, "idle", 2, 30*time.Millisecond, 100*time.Millisecond, time.Minute)
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	for want := 1; want <= 2; want++ {
		waitState(t, s, id, model.StateRunning, 5*time.Second)
		killPid(t, s, id)
		waitFor(t, "a crash", 5*time.Second, func() bool { return len(cr.all()) == want })
		if c := cr.all()[want-1]; c.Attempt != want || c.GaveUp {
			t.Fatalf("crash %d = %+v", want, c)
		}
	}
	waitState(t, s, id, model.StateRunning, 5*time.Second)
	killPid(t, s, id)
	waitFor(t, "giving up", 5*time.Second, func() bool { return len(cr.all()) == 3 })
	if c := cr.all()[2]; !c.GaveUp || c.Attempt != 2 {
		t.Errorf("third crash = %+v, want gave up", c)
	}
	waitState(t, s, id, model.StateFailed, 5*time.Second)
}

// The limit and the switch are read from the store when the service crashes
// and when the timer fires, not from the copy the run was started with.
func TestRestartPicksUpConfigChange(t *testing.T) {
	t.Run("limit", func(t *testing.T) {
		s, st, _, cr, id := restartSetup(t, "exit", 10, 150*time.Millisecond, 150*time.Millisecond, time.Minute)
		if err := s.Start(id); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the first crash", 5*time.Second, func() bool { return len(cr.all()) == 1 })
		setRestart(t, st, id, true, 1) // one restart is allowed and it has been planned
		waitFor(t, "giving up", 5*time.Second, func() bool {
			c := cr.all()
			return len(c) > 0 && c[len(c)-1].GaveUp
		})
		if c := cr.all(); len(c) != 2 || c[1].Limit != 1 || c[1].Attempt != 1 {
			t.Errorf("crashes = %+v, want the second one to give up with limit 1", c)
		}
	})
	t.Run("switched off while waiting", func(t *testing.T) {
		s, st, _, cr, id := restartSetup(t, "exit", 5, 300*time.Millisecond, time.Minute, time.Minute)
		if err := s.Start(id); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "the first crash", 5*time.Second, func() bool { return len(cr.all()) == 1 })
		setRestart(t, st, id, false, 5)
		time.Sleep(800 * time.Millisecond)
		if n := countLog(s, id, "starting:"); n != 1 {
			t.Errorf("restarted although restart on crash was switched off: %d starts", n)
		}
	})
}

func TestRestartStatusEventCarriesRestarts(t *testing.T) {
	type ev struct {
		typ  string
		data any
	}
	var mu sync.Mutex
	var events []ev
	s, _, _, _, id := restartSetup(t, "exit", 1, 30*time.Millisecond, 30*time.Millisecond, time.Minute)
	s.em = emitterFunc(func(typ string, data any) {
		mu.Lock()
		events = append(events, ev{typ, data})
		mu.Unlock()
	})
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, model.StateFailed, 5*time.Second)
	waitFor(t, "a status event with restarts 1", 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, e := range events {
			if m, ok := e.data.(map[string]any); ok && e.typ == "service.status" && m["restarts"] == 1 {
				return true
			}
		}
		return false
	})
}

type emitterFunc func(typ string, data any)

func (f emitterFunc) Publish(typ string, data any) { f(typ, data) }

func TestRestartDelay(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60, 60}
	for i, w := range want {
		if got := s.restartDelay(i + 1); got != w*time.Second {
			t.Errorf("delay for attempt %d = %s, want %s", i+1, got, w*time.Second)
		}
	}
	if got := s.restartDelay(10000); got != time.Minute {
		t.Errorf("delay for a huge attempt = %s", got)
	}
}
