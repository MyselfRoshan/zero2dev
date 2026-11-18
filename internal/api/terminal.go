package api

import (
	"errors"
	"net/http"

	"zero2dev/internal/apperr"
	"zero2dev/internal/launch"
)

// POST /api/services/{id}/terminal opens a console in the service's folder
// with the environment the service runs with (runtime on PATH, JAVA_HOME,
// database variables, .env, its own variables), so `node -v` there is the
// version the service uses. 204 once the console is open; 501 where Zero2Dev
// cannot open one. Workstream D owns this file.
func (s *Server) openTerminal(w http.ResponseWriter, r *http.Request) error {
	notSupported := &apperr.Error{Status: http.StatusNotImplemented, Code: "not_supported", Message: "opening a terminal is not available"}
	if s.Terminal == nil {
		return notSupported
	}
	sv, err := s.view(r.PathValue("id"))
	if err != nil {
		return err
	}
	env, err := s.Sup.Environ(sv)
	if err != nil {
		return err
	}
	dir := sv.Cwd
	if dir == "" {
		dir = s.Cfg.Home
	}
	if err := s.Terminal(dir, env); err != nil {
		if errors.Is(err, launch.ErrNoTerminal) {
			return notSupported
		}
		return apperr.Internal("%v", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
