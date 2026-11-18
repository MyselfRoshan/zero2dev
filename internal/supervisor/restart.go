package supervisor

import (
	"errors"
	"fmt"
	"math"
	"time"

	"zero2dev/internal/model"
)

// Crash describes one unexpected exit of a service. It is passed to
// Supervisor.OnCrash (the tray balloon) and published as the "service.crashed"
// event.
//
// For a service with restart on crash, Attempt is the number of the restart
// that is now planned (1 for the first crash since the last manual start) and
// Limit is the service's restartLimit; with GaveUp the limit is reached, Attempt
// equals Limit and nothing is planned. For a service without restart on crash,
// Attempt is 1, Limit is 0 and GaveUp is false: no restart is planned.
type Crash struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Error      string `json:"error"`      // the exit message
	Attempt    int    `json:"attempt"`    // 1-based number of this crash since the last manual start
	Limit      int    `json:"limit"`      // the service's restartLimit
	RetryInSec int    `json:"retryInSec"` // delay before the next start; 0 when GaveUp
	GaveUp     bool   `json:"gaveUp"`     // the limit is reached: no further restart
}

// crashState is the restart bookkeeping of one service. All fields are guarded
// by Supervisor.mu. The record exists from the first crash of a service with
// restart on crash until a manual Start, Stop, Remove or the daemon's close.
type crashState struct {
	attempts int         // restarts planned or done in this streak
	p        *proc       // the failed run the pending timer belongs to
	timer    *time.Timer // pending restart; nil once fired or when the service gave up
}

var errRestartCancelled = errors.New("crash restart cancelled")

// cancelRestart drops a pending crash restart of service id (its timer) and
// resets its crash counter. Stop, Stop all, a manual Start, Remove and the
// daemon's close do this; a restart timer that fires anyway re-checks the
// slot, the state and "closing" before starting.
func (s *Supervisor) cancelRestart(id string) {
	s.mu.Lock()
	s.cancelRestartLocked(id)
	s.mu.Unlock()
}

// cancelRestartLocked is cancelRestart; the caller holds s.mu.
func (s *Supervisor) cancelRestartLocked(id string) {
	cs := s.crash[id]
	if cs == nil {
		return
	}
	if cs.timer != nil {
		cs.timer.Stop()
	}
	delete(s.crash, id)
}

// restartsLocked is the number of crash restarts of the current streak, as
// shown in the service status; the caller holds s.mu. A service that has been
// running for RestartStable counts as having started over.
func (s *Supervisor) restartsLocked(id string) int {
	cs := s.crash[id]
	if cs == nil {
		return 0
	}
	if p := s.procs[id]; p != nil && active(p.state) && s.stable(time.Since(p.startedAt)) && !p.startedAt.IsZero() {
		return 0
	}
	return cs.attempts
}

// stable reports whether a run of this length ends the crash streak.
func (s *Supervisor) stable(ran time.Duration) bool {
	return s.RestartStable > 0 && ran >= s.RestartStable
}

// restartDelay is the wait before restart attempt n (1-based): RestartBase,
// doubled for every further attempt, at most RestartMax.
func (s *Supervisor) restartDelay(n int) time.Duration {
	d, limit := s.RestartBase, s.RestartMax
	if d <= 0 {
		d = time.Second
	}
	if limit <= 0 {
		limit = time.Minute
	}
	for i := 1; i < n && d < limit; i++ {
		d *= 2
	}
	return min(d, limit)
}

// service returns the current definition of a service from the store.
func (s *Supervisor) service(id string) (model.Service, bool) {
	for _, sv := range s.st.Snapshot().Services {
		if sv.ID == id {
			return sv, true
		}
	}
	return model.Service{}, false
}

// crashed handles the unexpected exit of run p of service id (msg is the exit
// message, ran how long it lived): it reports the crash and, when the current
// service definition has restart on crash and the limit is not reached yet,
// plans the next start. expect is the crash record of a restart that failed to
// start (nil when called from wait); a record that was cancelled meanwhile ends
// it. The caller must not hold s.mu.
func (s *Supervisor) crashed(id string, p *proc, msg string, ran time.Duration, expect *crashState) {
	svc, exists := s.service(id) // the current definition, not the one the run was started with

	s.mu.Lock()
	if !exists || s.closing || s.procs[id] != p || p.state != model.StateFailed || (expect != nil && s.crash[id] != expect) {
		if !exists {
			s.cancelRestartLocked(id)
		}
		s.mu.Unlock()
		return
	}
	c := Crash{ID: id, Name: svc.Name, Error: msg, Attempt: 1}
	if !svc.RestartOnCrash {
		s.cancelRestartLocked(id)
		s.mu.Unlock()
		s.report(c)
		return
	}

	cs := s.crash[id]
	if cs == nil {
		cs = &crashState{}
		s.crash[id] = cs
	}
	if s.stable(ran) {
		cs.attempts = 0 // it ran long enough: a new streak
	}
	limit := svc.RestartLimit
	if limit < 1 {
		limit = model.DefaultRestartLimit
	}
	limit = min(limit, model.MaxRestartLimit)
	c.Limit = limit
	if cs.attempts >= limit {
		cs.timer = nil
		c.Attempt, c.GaveUp = limit, true
		s.mu.Unlock()
		s.logLine(id, "system", fmt.Sprintf("giving up: restarted %d times and it keeps failing", limit))
		s.report(c)
		return
	}
	cs.attempts++
	delay := s.restartDelay(cs.attempts)
	c.Attempt, c.RetryInSec = cs.attempts, int(math.Ceil(delay.Seconds()))
	cs.p = p
	cs.timer = time.AfterFunc(delay, func() { s.fireRestart(id, p, cs) })
	s.mu.Unlock()
	s.logLine(id, "system", fmt.Sprintf("restarting in %s (attempt %d/%d)", delay, c.Attempt, limit))
	s.report(c)
}

// report publishes the service.crashed event and calls OnCrash.
func (s *Supervisor) report(c Crash) {
	s.em.Publish("service.crashed", c)
	if s.OnCrash != nil {
		s.OnCrash(c)
	}
}

// fireRestart is the restart timer of run p. It starts the service again only
// when nothing changed since the timer was set: the crash record is still the
// current one, the slot still holds the same failed run, the service still
// exists and wants restarts, and the daemon is not closing.
func (s *Supervisor) fireRestart(id string, p *proc, cs *crashState) {
	svc, exists := s.service(id)
	s.mu.Lock()
	ok := exists && svc.RestartOnCrash && !s.closing && s.crash[id] == cs && s.procs[id] == p && p.state == model.StateFailed
	if ok {
		cs.timer = nil
	} else if s.crash[id] == cs {
		s.cancelRestartLocked(id)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	err := s.start(id, cs)
	if err == nil || errors.Is(err, errRestartCancelled) {
		return
	}
	// The start itself failed (port taken, folder gone ...): it counts as the next crash.
	s.logLine(id, "system", "restart failed: "+err.Error())
	s.mu.Lock()
	if s.crash[id] == cs && s.procs[id] == p {
		p.lastErr = err.Error()
	}
	s.mu.Unlock()
	s.crashed(id, p, err.Error(), 0, cs)
	s.emitStatus(id)
}
