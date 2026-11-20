package supervisor

import (
	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
)

// Reserve holds the services in ids for an operation on their files (a
// snapshot, a restore, a database reset, a migration): while they are held,
// Start refuses them, and Busy reports them, which the API uses to refuse
// editing and deleting them. why is shown in those messages, e.g. "taking a
// snapshot".
//
// It fails with 409 when one of them is active (starting, running or
// stopping) or already held; then nothing is held. Unknown ids are held
// too (the caller checks existence). release ends the hold; calling it more
// than once is harmless.
func (s *Supervisor) Reserve(ids []string, why string) (release func(), err error) {
	names := map[string]string{}
	for _, sv := range s.st.Snapshot().Services {
		names[sv.ID] = sv.Name
	}
	name := func(id string) string {
		if n := names[id]; n != "" {
			return n
		}
		return id
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		if p := s.procs[id]; p != nil && active(p.state) {
			return nil, apperr.Conflict("%s is %s; stop it first", name(id), p.state)
		}
		if w, ok := s.reserved[id]; ok {
			return nil, apperr.Conflict("%s is busy (%s)", name(id), w)
		}
	}
	if s.reserved == nil {
		s.reserved = map[string]string{}
	}
	for _, id := range ids {
		s.reserved[id] = why
	}
	done := false
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if done {
			return
		}
		done = true
		for _, id := range ids {
			delete(s.reserved, id)
		}
	}, nil
}

// Busy returns a 409 naming the operation when the service is held by
// Reserve, else nil.
func (s *Supervisor) Busy(id string) error {
	s.mu.Lock()
	why, ok := s.reserved[id]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	name := id
	for _, sv := range s.st.Snapshot().Services {
		if sv.ID == id {
			name = sv.Name
		}
	}
	return apperr.Conflict("%s is busy (%s); try again when it is done", name, why)
}

// RuntimeFor returns the runtime svc runs with now: its own RuntimeID, else
// the default of its kind, else the first of its kind (rt), or a program found
// on PATH (rt nil, bin set). It is the resolution the next start would use.
func (s *Supervisor) RuntimeFor(svc model.Service) (rt *model.Runtime, bin string, err error) {
	rt, bin, err = findRuntime(svc, s.st.Snapshot())
	if rt != nil {
		bin = rt.Binary
	}
	return rt, bin, err
}

// Log adds a line to a service's log (stream "system", "stdout" or "stderr"),
// as if the service had written it: it is stored and sent as log.line. The
// database tools and snapshots use it to show what they did.
func (s *Supervisor) Log(id, stream, text string) { s.logLine(id, stream, text) }
