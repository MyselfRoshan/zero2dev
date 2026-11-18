package api

import (
	"net/http"

	"zero2dev/internal/apperr"
	"zero2dev/internal/envreg"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// environmentView is what GET and PUT /api/environment answer: the user's choices and,
// next to them, what the Windows user environment holds right now.
type environmentView struct {
	Config model.EnvConfig `json:"config"`
	Report envreg.Report   `json:"report"`
}

func (s *Server) environment(d store.Data) environmentView {
	rep := envreg.Report{Path: []envreg.PathStatus{}, Vars: []envreg.VarStatus{}}
	if s.EnvReport != nil {
		rep = s.EnvReport(d)
	}
	return environmentView{Config: d.Env, Report: rep}
}

func (s *Server) getEnvironment(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, s.environment(s.Store.Snapshot()))
	return nil
}

// envInput is the whole config: it replaces the saved one. "seeded" is accepted and
// ignored because the UI echoes the config it read.
type envInput struct {
	Seeded bool            `json:"seeded"`
	Path   []model.EnvPath `json:"path"`
	Vars   []model.EnvVar  `json:"vars"`
}

// putEnvironment saves the config and applies it to the user environment. The apply is
// part of the update, so a failure (409, with the reason) saves nothing and the config
// always matches the registry.
func (s *Server) putEnvironment(w http.ResponseWriter, r *http.Request) error {
	var in envInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	var view environmentView
	err := s.Store.Update(func(d *store.Data) error {
		cfg, err := envreg.Validate(model.EnvConfig{Path: in.Path, Vars: in.Vars}, d.Runtimes)
		if err != nil {
			return apperr.BadRequest("%v", err)
		}
		d.Env = cfg
		if s.EnvApply != nil {
			if err := s.EnvApply(*d); err != nil {
				return apperr.Conflict("cannot change the user environment: %v", err)
			}
		}
		view = s.environment(*d)
		return nil
	})
	if err != nil {
		return err
	}
	s.Hub.Publish("environment.changed", struct{}{})
	writeJSON(w, 200, view)
	return nil
}
