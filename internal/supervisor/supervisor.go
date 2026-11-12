// Package supervisor starts, watches and stops service processes.
package supervisor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// Emitter receives events (implemented by events.Hub).
type Emitter interface{ Publish(typ string, data any) }

// Supervisor owns all child processes.
type Supervisor struct {
	home string
	st   *store.Store
	em   Emitter

	// Tunables (tests shorten them).
	HealthTimeout time.Duration
	KillGrace     time.Duration
	DBGrace       time.Duration // databases get longer to shut down cleanly

	mu      sync.Mutex
	procs   map[string]*proc
	logs    map[string]*svcLog
	closing bool
}

// proc is one run of a service. All fields are guarded by Supervisor.mu.
type proc struct {
	state     string
	pid       int
	startedAt time.Time
	lastErr   string
	stopping  bool
	failMsg   string
	spawned   bool
	tree      *tree
	done      chan struct{}
	// Clean stop (databases): a command that asks the server to shut down,
	// run before anything is killed, and how long to wait for it.
	stopBin  string
	stopArgs []string
	stopDesc string
	env      []string
	grace    time.Duration
}

func active(state string) bool {
	return state == model.StateStarting || state == model.StateRunning || state == model.StateStopping
}

// New creates a supervisor. Logs live in <home>/logs.
func New(home string, st *store.Store, em Emitter) *Supervisor {
	return &Supervisor{
		home: home, st: st, em: em,
		HealthTimeout: 30 * time.Second, KillGrace: 5 * time.Second, DBGrace: DBGrace,
		procs: map[string]*proc{}, logs: map[string]*svcLog{},
	}
}

func (s *Supervisor) logPath(id string) string { return filepath.Join(s.home, "logs", id+".log") }

func (s *Supervisor) logFor(id string) *svcLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.logs[id]
	if l == nil {
		l = newSvcLog(s.logPath(id))
		s.logs[id] = l
	}
	return l
}

func (s *Supervisor) logLine(id, stream, text string) {
	line := model.LogLine{Ts: tsNow(), Stream: stream, Text: text}
	s.logFor(id).add(line)
	s.em.Publish("log.line", map[string]any{"serviceId": id, "ts": line.Ts, "stream": line.Stream, "text": line.Text})
}

// Logs returns the last n lines (n is clamped by the caller).
func (s *Supervisor) Logs(id string, n int) []model.LogLine { return s.logFor(id).tail(n) }

// status snapshot; caller holds s.mu.
func (s *Supervisor) statusLocked(id string) (state string, pid int, started *time.Time, lastErr string) {
	p := s.procs[id]
	if p == nil {
		return model.StateStopped, 0, nil, ""
	}
	var st *time.Time
	if !p.startedAt.IsZero() && active(p.state) {
		t := p.startedAt.UTC().Truncate(time.Second)
		st = &t
	}
	pidv := p.pid
	if !active(p.state) {
		pidv = 0
	}
	return p.state, pidv, st, p.lastErr
}

// State returns the current state string of a service.
func (s *Supervisor) State(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, _, _ := s.statusLocked(id)
	return st
}

// Apply overlays runtime status onto a stored service definition.
func (s *Supervisor) Apply(svc model.Service) model.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc.State, svc.Pid, svc.StartedAt, svc.LastError = s.statusLocked(svc.ID)
	return svc
}

func (s *Supervisor) emitStatus(id string) {
	s.mu.Lock()
	st, pid, started, le := s.statusLocked(id)
	s.mu.Unlock()
	s.em.Publish("service.status", map[string]any{"id": id, "state": st, "pid": pid, "startedAt": started, "lastError": le})
}

// Start launches a service. It returns once the process is spawned (state
// "starting"); health checks move it to "running" asynchronously.
func (s *Supervisor) Start(id string) error {
	d := s.st.Snapshot()
	var svc *model.Service
	for i := range d.Services {
		if d.Services[i].ID == id {
			svc = &d.Services[i]
		}
	}
	if svc == nil {
		return apperr.NotFound("service %s not found", id)
	}

	// Reserve the slot so concurrent starts cannot both proceed.
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return apperr.Conflict("daemon is shutting down")
	}
	prev := s.procs[id]
	if prev != nil && active(prev.state) {
		st := prev.state
		s.mu.Unlock()
		return apperr.Conflict("service is already %s", st)
	}
	p := &proc{state: model.StateStarting, done: make(chan struct{})}
	s.procs[id] = p
	s.mu.Unlock()

	revert := func() {
		s.mu.Lock()
		if s.procs[id] == p {
			if prev != nil {
				s.procs[id] = prev
			} else {
				delete(s.procs, id)
			}
		}
		s.mu.Unlock()
	}

	r, err := s.resolve(*svc, d)
	if err != nil {
		revert()
		return err
	}
	if svc.Port > 0 && !portFree(svc.Port) {
		revert()
		return apperr.Conflict("port %d is already in use", svc.Port)
	}
	if err := s.prepare(id, r); err != nil {
		s.logLine(id, "system", err.Error())
		revert()
		return apperr.Conflict("%v", err)
	}
	if err := s.initDB(id, r.db, r.env); err != nil {
		s.logLine(id, "system", err.Error())
		revert()
		return apperr.Conflict("%v", err)
	}
	if st, err := os.Stat(r.workdir); err != nil || !st.IsDir() {
		if svc.Cwd != "" {
			revert()
			return apperr.Conflict("service folder %s does not exist or is not a directory", svc.Cwd)
		}
		r.workdir = filepath.Dir(r.bin) // e.g. a database with its own args: no data folder of Zero2Dev's
	}

	cmd := exec.Command(r.bin, r.args...) // never a shell
	cmd.Dir = r.workdir
	cmd.Env = r.env
	outW := &lineWriter{emit: func(t string) { s.logLine(id, "stdout", t) }}
	errW := &lineWriter{emit: func(t string) { s.logLine(id, "stderr", t) }}
	cmd.Stdout, cmd.Stderr = outW, errW
	cmd.WaitDelay = 2 * time.Second // do not hang on pipes held by orphaned grandchildren
	configure(cmd)
	s.logLine(id, "system", fmt.Sprintf("starting: %s %v", r.bin, r.args))
	if err := cmd.Start(); err != nil {
		s.logLine(id, "system", "failed to start: "+err.Error())
		revert()
		return apperr.Conflict("failed to start %s: %v", filepath.Base(r.bin), err)
	}
	tr, terr := attach(cmd)
	if terr != nil {
		s.logLine(id, "system", "warning: process tree tracking unavailable: "+terr.Error())
	}

	s.mu.Lock()
	p.tree, p.pid, p.startedAt, p.spawned = tr, cmd.Process.Pid, time.Now(), true
	p.grace, p.env = s.KillGrace, r.env
	if r.db != nil && r.db.stopBin != "" {
		p.stopBin, p.stopArgs, p.stopDesc = r.db.stopBin, r.db.stopArgs, r.db.stopDesc
		p.grace = max(s.DBGrace, s.KillGrace)
	}
	s.mu.Unlock()
	s.emitStatus(id)

	go s.wait(id, p, cmd, outW, errW)
	go s.watchHealth(id, p, *svc)
	return nil
}

// wait reaps the process and records the final state.
func (s *Supervisor) wait(id string, p *proc, cmd *exec.Cmd, outW, errW *lineWriter) {
	err := cmd.Wait()
	outW.Flush()
	errW.Flush()
	p.tree.close() // kill leftover descendants

	msg := "exit status 0"
	if err != nil {
		msg = err.Error()
	}
	s.mu.Lock()
	if p.stopping {
		p.state, p.lastErr = model.StateStopped, ""
	} else {
		if p.failMsg != "" {
			msg = p.failMsg
		}
		p.state, p.lastErr = model.StateFailed, msg
	}
	final := p.state
	close(p.done)
	s.mu.Unlock()
	if final == model.StateStopped {
		s.logLine(id, "system", "stopped")
	} else {
		s.logLine(id, "system", "exited unexpectedly: "+msg)
	}
	s.emitStatus(id)
}

// watchHealth promotes starting -> running or fails the service.
func (s *Supervisor) watchHealth(id string, p *proc, svc model.Service) {
	promote := func() {
		s.mu.Lock()
		ok := p.state == model.StateStarting
		if ok {
			p.state = model.StateRunning
		}
		s.mu.Unlock()
		if ok {
			s.emitStatus(id)
		}
	}
	if svc.Port == 0 {
		select {
		case <-time.After(500 * time.Millisecond):
			promote()
		case <-p.done:
		}
		return
	}
	deadline := time.NewTimer(s.HealthTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-deadline.C:
			s.mu.Lock()
			still := p.state == model.StateStarting
			if still {
				p.failMsg = fmt.Sprintf("health check timed out after %s", s.HealthTimeout)
			}
			s.mu.Unlock()
			if still {
				s.logLine(id, "system", "health check timed out; killing process")
				p.tree.kill()
			}
			return
		case <-tick.C:
			if probe(svc.Port, svc.HealthPath) {
				promote()
				return
			}
		}
	}
}

var healthClient = &http.Client{
	Timeout:       time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// probe checks the service port: HTTP GET (status < 500) if healthPath is
// set, otherwise a plain TCP connect.
func probe(port int, healthPath string) bool {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if healthPath == "" {
		c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}
	resp, err := healthClient.Get("http://" + addr + healthPath)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// Stop requests termination and returns immediately (state "stopping").
func (s *Supervisor) Stop(id string) error {
	s.mu.Lock()
	p := s.procs[id]
	if p == nil || !active(p.state) || p.state == model.StateStopping {
		s.mu.Unlock()
		return nil
	}
	if !p.spawned {
		s.mu.Unlock()
		return apperr.Conflict("service is still starting, try again")
	}
	p.stopping, p.state = true, model.StateStopping
	stopBin, stopArgs, stopDesc, env, grace := p.stopBin, p.stopArgs, p.stopDesc, p.env, p.grace
	s.mu.Unlock()
	s.emitStatus(id)
	if grace <= 0 {
		grace = s.KillGrace
	}
	if stopBin != "" {
		// Ask the database to shut down and wait for it; only then force it.
		s.logLine(id, "system", "stopping: asking "+stopDesc+" to shut the server down")
		go func() {
			if err := s.runTool(id, stopBin, stopArgs, env, grace); err != nil {
				s.logLine(id, "system", stopDesc+" failed: "+err.Error())
			}
			select {
			case <-p.done:
			case <-time.After(grace):
				s.logLine(id, "system", "the server did not shut down in time; ending it")
				p.tree.kill()
			}
		}()
		return nil
	}
	s.logLine(id, "system", "stopping")
	p.tree.terminate()
	go func() {
		select {
		case <-p.done:
		case <-time.After(grace):
			p.tree.kill()
		}
	}()
	return nil
}

// StopWait stops and waits (bounded) until the process is gone.
func (s *Supervisor) StopWait(id string) error {
	if err := s.Stop(id); err != nil {
		return err
	}
	s.mu.Lock()
	p := s.procs[id]
	s.mu.Unlock()
	if p == nil {
		return nil
	}
	s.mu.Lock()
	wait := max(p.grace, s.KillGrace)
	if p.stopBin != "" {
		wait *= 2 // the stop command, then the wait for the exit
	}
	s.mu.Unlock()
	select {
	case <-p.done:
	case <-time.After(wait + 3*time.Second):
		return apperr.Conflict("service did not stop in time")
	}
	return nil
}

// Restart stops (waiting for exit) then starts again.
func (s *Supervisor) Restart(id string) error {
	if err := s.StopWait(id); err != nil {
		return err
	}
	return s.Start(id)
}

// Remove stops a service and forgets its logs (used on delete).
func (s *Supervisor) Remove(id string) error {
	if err := s.StopWait(id); err != nil {
		return err
	}
	s.mu.Lock()
	l := s.logs[id]
	delete(s.logs, id)
	delete(s.procs, id)
	s.mu.Unlock()
	if l != nil {
		l.close()
	}
	_ = os.Remove(s.logPath(id))
	_ = os.Remove(s.logPath(id) + ".1")
	return nil
}

// StopAll stops every running service (daemon shutdown) and refuses new starts.
func (s *Supervisor) StopAll(ctx context.Context) {
	s.mu.Lock()
	s.closing = true
	var ids []string
	for id, p := range s.procs {
		if active(p.state) {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = s.Stop(id)
			s.mu.Lock()
			p := s.procs[id]
			s.mu.Unlock()
			select {
			case <-p.done:
			case <-ctx.Done():
				if p.tree != nil {
					p.tree.kill()
				}
			}
		}(id)
	}
	wg.Wait()
	s.mu.Lock()
	for _, l := range s.logs {
		l.close()
	}
	s.mu.Unlock()
}

// AutostartAll starts every service flagged autostart.
func (s *Supervisor) AutostartAll() {
	for _, svc := range s.st.Snapshot().Services {
		if svc.Autostart {
			if err := s.Start(svc.ID); err != nil {
				s.logLine(svc.ID, "system", "autostart failed: "+err.Error())
			}
		}
	}
}
