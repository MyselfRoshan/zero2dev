package supervisor

import (
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
)

// StartFailure is a service that did not start (or whose dependency did not).
type StartFailure struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Error string `json:"error"`
}

// StartReport is the outcome of StartOrdered. Both lists are never nil.
type StartReport struct {
	Started []string       `json:"started"`
	Failed  []StartFailure `json:"failed"`
}

// pollEvery is how often the start waves look at a service that is starting.
const pollEvery = 50 * time.Millisecond

// StartOrdered starts the services in ids together with every inactive
// service they depend on (model.Deps), dependencies first. ids == nil means
// every service; a non-nil empty slice means none. Services that are already
// active are left alone and are not reported (a running dependency counts as
// satisfied). A failure is recorded and the services that depend on it are
// reported failed as well ("waits for <name>"), without being started. It
// answers when every wave has reached running, failed or HealthTimeout.
//
// The start is done in waves: every inactive service whose dependencies are
// all running is ready; the ready ones are started one after the other, highest
// StopTier first (site server, databases, apps), and the wave is awaited when
// something still waiting depends on one of its services. A service nothing
// waits for is reported started as soon as it is spawned. A dependency loop
// (the API rejects those, this is defence in depth) fails the services in it
// instead of waiting forever.
func (s *Supervisor) StartOrdered(ids []string) StartReport {
	rep := StartReport{Started: []string{}, Failed: []StartFailure{}}
	d := s.st.Snapshot()
	byID := serviceIndex(d.Services)
	wanted := ids
	if ids == nil {
		wanted = make([]string, 0, len(d.Services))
		for _, sv := range d.Services {
			wanted = append(wanted, sv.ID)
		}
	}
	todo := s.startClosure(d.Services, byID, wanted)
	started, failed, _ := s.startWaves(byID, todo, false, nil)
	rep.Started = append(rep.Started, started...)
	rep.Failed = append(rep.Failed, failed...)
	return rep
}

// Sequence is the order a project's services start in: the members in the
// order they are listed, except that a member moves down to just after the
// last member it needs (model.Deps, transitively, also through services that
// are not members). Ids that name no service are left out, and so are
// duplicates. A dependency loop (the API rejects those) leaves the members
// that are in it in list order.
func Sequence(members []string, services []model.Service) []string {
	byID := serviceIndex(services)
	var list []string
	for _, id := range members {
		if _, ok := byID[id]; ok && !slices.Contains(list, id) {
			list = append(list, id)
		}
	}
	// needs[m]: the other members m needs, directly or through any service.
	needs := make(map[string]map[string]bool, len(list))
	for _, m := range list {
		reach := map[string]bool{}
		var visit func(id string)
		visit = func(id string) {
			for _, d := range model.Deps(byID[id]) {
				if _, known := byID[d]; known && !reach[d] {
					reach[d] = true
					visit(d)
				}
			}
		}
		visit(m)
		needs[m] = map[string]bool{}
		for _, o := range list {
			if o != m && reach[o] {
				needs[m][o] = true
			}
		}
	}
	out := make([]string, 0, len(list))
	placed := map[string]bool{}
	for len(out) < len(list) {
		next := ""
		for _, m := range list {
			if placed[m] {
				continue
			}
			ok := true
			for o := range needs[m] {
				if !placed[o] && !needs[o][m] { // a loop does not hold anyone back
					ok = false
					break
				}
			}
			if ok {
				next = m
				break
			}
		}
		if next == "" { // cannot happen with the loop rule above; keep list order anyway
			for _, m := range list {
				if !placed[m] {
					next = m
					break
				}
			}
		}
		placed[next] = true
		out = append(out, next)
	}
	return out
}

// StartInOrder starts a project: the members one after the other in their
// Sequence, each awaited until it is running (or has failed) before the next
// one starts, together with the inactive services they need, which start
// before them as in StartOrdered. A member that fails does not stop the
// members after it (unless they need it). Members that are already active are
// left alone. also lists more services to start that have no place in the
// order (the site server of a project with sites); they start in the first wave.
func (s *Supervisor) StartInOrder(members []string, also ...string) StartReport {
	rep := StartReport{Started: []string{}, Failed: []StartFailure{}}
	d := s.st.Snapshot()
	byID := serviceIndex(d.Services)
	seq := Sequence(members, d.Services)
	wanted := append(append([]string{}, also...), seq...)
	todo := s.startClosure(d.Services, byID, wanted)
	after := make(map[string][]string, len(seq))
	for i := 1; i < len(seq); i++ {
		after[seq[i]] = seq[:i]
	}
	started, failed, _ := s.startWaves(byID, todo, false, after)
	rep.Started = append(rep.Started, started...)
	rep.Failed = append(rep.Failed, failed...)
	return rep
}

// StartWithDeps is what POST /api/services/{id}/start calls: it starts the
// inactive services id depends on (model.Deps, transitively, dependencies
// first and waiting for each to be running) and then id itself, returning as
// soon as id is spawned (state "starting"), like Start. An error from Start
// of id is returned unchanged; a dependency that fails to start is returned as
// a 409 naming it, and id is not started.
//
// The target itself is never awaited, so the request is as quick as Start when
// its dependencies already run. When they have to be started first, the call
// blocks until they are running (or failed), and every wave is capped by
// HealthTimeout (plus a short margin), so the answer is bounded by roughly
// HealthTimeout times the length of the dependency chain.
func (s *Supervisor) StartWithDeps(id string) error {
	d := s.st.Snapshot()
	byID := serviceIndex(d.Services)
	if _, ok := byID[id]; !ok || active(s.State(id)) {
		return s.Start(id) // not found / already running: Start words the error
	}
	todo := s.startClosure(d.Services, byID, []string{id})
	if len(todo) <= 1 && !s.hasStartingDep(byID, id) {
		return s.Start(id)
	}
	_, failed, startErr := s.startWaves(byID, todo, false, nil)
	if err, ok := startErr[id]; ok {
		return err
	}
	if len(failed) > 0 {
		// The first failure is the root cause: dependents are only added after it.
		f := failed[0]
		if f.ID == id {
			return apperr.Conflict("%s %s", f.Name, f.Error)
		}
		return apperr.Conflict("%s did not start, so %s was not started: %s", f.Name, byID[id].Name, f.Error)
	}
	return nil
}

// hasStartingDep reports whether a dependency of id is starting right now (it
// is active, so it is not started again, but id should wait for it).
func (s *Supervisor) hasStartingDep(byID map[string]model.Service, id string) bool {
	for _, dep := range model.Deps(byID[id]) {
		if _, known := byID[dep]; known && s.State(dep) == model.StateStarting {
			return true
		}
	}
	return false
}

func serviceIndex(all []model.Service) map[string]model.Service {
	m := make(map[string]model.Service, len(all))
	for _, sv := range all {
		m[sv.ID] = sv
	}
	return m
}

// startClosure returns, in definition order, the inactive services among
// wanted and the inactive services they need, transitively. A service that is
// already active is left out and its own dependencies are not looked at.
func (s *Supervisor) startClosure(all []model.Service, byID map[string]model.Service, wanted []string) []string {
	in := map[string]bool{}
	seen := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		sv, ok := byID[id]
		if !ok || seen[id] {
			return
		}
		seen[id] = true
		if active(s.State(id)) {
			return
		}
		in[id] = true
		for _, dep := range model.Deps(sv) {
			visit(dep)
		}
	}
	for _, id := range wanted {
		visit(id)
	}
	out := make([]string, 0, len(in))
	for _, sv := range all {
		if in[sv.ID] {
			out = append(out, sv.ID)
		}
	}
	return out
}

// startWaves runs the plan described at StartOrdered for the services in todo
// (ids in definition order). It returns what started, what failed (a root
// cause always comes before the services that wait for it) and the raw error
// of every service whose Start itself failed. waitAll makes every wave wait for
// all of its services, not only those something depends on.
//
// after adds an order that is not a dependency (a project's start order): a
// service waits until the services listed for it are running, have failed or
// are not starting at all, but a failure among them does not fail it.
func (s *Supervisor) startWaves(byID map[string]model.Service, todo []string, waitAll bool, after map[string][]string) ([]string, []StartFailure, map[string]error) {
	started, failed := []string{}, []StartFailure{}
	startErr := map[string]error{}
	pending := make(map[string]bool, len(todo))
	for _, id := range todo {
		pending[id] = true
	}
	ready := map[string]bool{} // running, or spawned and nothing waits for it
	bad := map[string]bool{}   // failed, or did not become running
	fail := func(id, msg string) {
		failed = append(failed, StartFailure{ID: id, Name: byID[id].Name, Error: msg})
		bad[id] = true
		delete(pending, id)
	}
	var extDeadline time.Time // for dependencies that were already starting when we began

	const (
		okState = iota
		blockedState
		failedState
	)
	var extWait []string
	// check says whether the dependencies of id are all running (okState), some
	// are still coming up (blockedState) or one of them failed (failedState).
	check := func(id string) int {
		res := okState
		for _, d := range model.Deps(byID[id]) {
			dep, known := byID[d]
			if !known {
				continue
			}
			switch {
			case bad[d]:
				fail(id, "waits for "+dep.Name)
				return failedState
			case ready[d]:
			case pending[d]:
				res = blockedState
			default: // not part of the plan: it was active when the plan was made
				switch s.State(d) {
				case model.StateRunning:
					ready[d] = true
				case model.StateStarting:
					res = blockedState
					extWait = append(extWait, d)
				default:
					bad[d] = true
					fail(id, "waits for "+dep.Name)
					return failedState
				}
			}
		}
		for _, d := range after[id] {
			switch {
			case bad[d], ready[d]:
			case pending[d]:
				res = blockedState
			case s.State(d) == model.StateStarting:
				res = blockedState
				extWait = append(extWait, d)
			}
		}
		return res
	}

	for len(pending) > 0 {
		var runnable []string
		changed := false
		extWait = extWait[:0]
		for _, id := range todo {
			if !pending[id] {
				continue
			}
			switch check(id) {
			case okState:
				runnable = append(runnable, id)
			case failedState:
				changed = true
			}
		}
		if changed {
			continue // dependents of a failure are failed before anything else starts
		}
		if len(runnable) == 0 {
			if len(extWait) > 0 {
				if extDeadline.IsZero() {
					extDeadline = time.Now().Add(s.HealthTimeout + 2*time.Second)
				}
				if time.Now().After(extDeadline) {
					for _, d := range extWait {
						bad[d] = true
					}
				} else {
					time.Sleep(pollEvery)
				}
				continue
			}
			// Nothing can start and nothing is coming up: a dependency loop.
			for _, id := range todo {
				if pending[id] {
					fail(id, "waits for a service that waits for it (dependency loop)")
				}
			}
			break
		}

		sort.SliceStable(runnable, func(i, j int) bool {
			return StopTier(byID[runnable[i]].Kind) > StopTier(byID[runnable[j]].Kind)
		})
		var spawned []string
		for _, id := range runnable {
			if err := s.Start(id); err != nil {
				startErr[id] = err
				fail(id, err.Error())
				continue
			}
			spawned = append(spawned, id)
		}
		var await []string
		for _, id := range spawned {
			if waitAll || hasPendingDependent(byID, pending, after, id) {
				await = append(await, id)
			}
		}
		res := s.awaitRunning(await)
		for _, id := range spawned {
			if msg := res[id]; msg != "" {
				fail(id, msg)
				continue
			}
			ready[id] = true
			delete(pending, id)
			started = append(started, id)
		}
	}
	return started, failed, startErr
}

func hasPendingDependent(byID map[string]model.Service, pending map[string]bool, after map[string][]string, id string) bool {
	for p := range pending {
		if p != id && (slices.Contains(model.Deps(byID[p]), id) || slices.Contains(after[p], id)) {
			return true
		}
	}
	return false
}

// awaitRunning waits until every service in ids is running, has ended, or
// HealthTimeout (plus a margin for the supervisor to kill a service that never
// answered) has passed. The result maps each id to "" when it is running or to
// the reason it is not.
func (s *Supervisor) awaitRunning(ids []string) map[string]string {
	res := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return res
	}
	deadline := time.Now().Add(s.HealthTimeout + 2*time.Second)
	for {
		for _, id := range ids {
			if _, done := res[id]; done {
				continue
			}
			s.mu.Lock()
			st, _, _, lastErr := s.statusLocked(id)
			s.mu.Unlock()
			switch st {
			case model.StateRunning:
				res[id] = ""
			case model.StateStarting:
			default:
				if lastErr == "" {
					lastErr = "it stopped before it was ready"
				}
				res[id] = lastErr
			}
		}
		if len(res) == len(ids) {
			return res
		}
		if time.Now().After(deadline) {
			for _, id := range ids {
				if _, done := res[id]; !done {
					res[id] = fmt.Sprintf("it was not ready after %s", s.HealthTimeout)
				}
			}
			return res
		}
		time.Sleep(pollEvery)
	}
}

// StopOrdered stops the active services in ids (nil means every service; a
// non-nil empty slice means none) without shutting the daemon down, dependents
// before what they depend on, and answers when they have stopped or timed out.
// A service that cannot be stopped is reported and does not hold the others
// back. Stopping also cancels a pending crash restart (see restart.go), also
// for services that are not running at the moment.
//
// The stop is done in waves: the services in ids that no other service still
// waiting to be stopped depends on, and of those only the lowest StopTier (apps,
// then databases, then the site server). Services of one wave stop at the same
// time and a wave is finished before the next starts, so no app loses its
// database while it is still running. Without dependencies this is exactly the
// tier order. Dependents outside ids do not hold a service back: the caller
// asked for it explicitly. A dependency loop is stopped as one last wave.
func (s *Supervisor) StopOrdered(ids []string) StopReport { return s.stopWaves(ids, nil) }

// StopInOrder stops a project: its active members in the reverse of their
// Sequence, one after the other, each awaited before the next. Dependencies
// keep their rule (dependents first), which the Sequence already follows.
func (s *Supervisor) StopInOrder(members []string) StopReport {
	seq := Sequence(members, s.st.Snapshot().Services)
	return s.stopWaves(seq, seq)
}

// stopWaves is StopOrdered for ids, where seq (may be nil) adds an order: a
// service in seq stops only after every active service later in seq.
func (s *Supervisor) stopWaves(ids, seq []string) StopReport {
	rep := StopReport{Stopped: []string{}, Failed: []StopFailure{}}
	pending := map[string]model.Service{}
	for _, svc := range s.st.Snapshot().Services {
		if ids != nil && !slices.Contains(ids, svc.ID) {
			continue
		}
		s.cancelRestart(svc.ID)
		if active(s.State(svc.ID)) {
			pending[svc.ID] = svc
		}
	}
	for len(pending) > 0 {
		needed := map[string]bool{}
		for _, sv := range pending {
			for _, d := range model.Deps(sv) {
				if d != sv.ID {
					needed[d] = true
				}
			}
		}
		for i, id := range seq {
			if _, ok := pending[id]; ok {
				for _, earlier := range seq[:i] {
					needed[earlier] = true
				}
			}
		}
		var free []model.Service
		for id, sv := range pending {
			if !needed[id] {
				free = append(free, sv)
			}
		}
		if len(free) == 0 { // a loop: stop what is left in one go
			for _, sv := range pending {
				free = append(free, sv)
			}
		}
		lowest := tierSiteServer
		for _, sv := range free {
			lowest = min(lowest, StopTier(sv.Kind))
		}
		var (
			mu sync.Mutex
			wg sync.WaitGroup
		)
		for _, svc := range free {
			if StopTier(svc.Kind) != lowest {
				continue
			}
			delete(pending, svc.ID)
			wg.Add(1)
			go func(svc model.Service) {
				defer wg.Done()
				err := s.StopWait(svc.ID)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					rep.Failed = append(rep.Failed, StopFailure{ID: svc.ID, Name: svc.Name, Error: err.Error()})
					return
				}
				rep.Stopped = append(rep.Stopped, svc.ID)
			}(svc)
		}
		wg.Wait()
	}
	sort.Strings(rep.Stopped)
	sort.Slice(rep.Failed, func(i, j int) bool { return rep.Failed[i].ID < rep.Failed[j].ID })
	return rep
}

// FindCycle looks for a loop in the "needs" relation (model.Deps of every
// service) and returns it as a closed path of service ids, e.g. [a b c a], or
// nil when there is none. Links to services that do not exist are ignored.
// Used by the API to reject a service whose dependencies would form a loop.
func FindCycle(services []model.Service) []string {
	deps := make(map[string][]string, len(services))
	for _, sv := range services {
		deps[sv.ID] = model.Deps(sv)
	}
	const (
		white = iota
		grey
		black
	)
	colour := map[string]int{}
	var path []string
	var visit func(id string) []string
	visit = func(id string) []string {
		colour[id] = grey
		path = append(path, id)
		for _, d := range deps[id] {
			if _, known := deps[d]; !known {
				continue
			}
			switch colour[d] {
			case grey:
				i := slices.Index(path, d)
				return append(append([]string{}, path[i:]...), d)
			case white:
				if c := visit(d); c != nil {
					return c
				}
			}
		}
		path = path[:len(path)-1]
		colour[id] = black
		return nil
	}
	for _, sv := range services {
		if colour[sv.ID] == white {
			if c := visit(sv.ID); c != nil {
				return c
			}
		}
	}
	return nil
}
