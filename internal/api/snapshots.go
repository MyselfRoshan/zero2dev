package api

import (
	"net/http"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
	"zero2dev/internal/snapshot"
)

// Snapshots of projects and database services (0.0.5, docs/API.md "Snapshots").
func (s *Server) snapshotRoutes() {
	s.handle("GET /api/projects/{id}/snapshots", s.listSnapshots(snapshot.ScopeProject))
	s.handle("POST /api/projects/{id}/snapshots", s.takeSnapshot(snapshot.ScopeProject))
	s.handle("GET /api/services/{id}/snapshots", s.listSnapshots(snapshot.ScopeService))
	s.handle("POST /api/services/{id}/snapshots", s.takeSnapshot(snapshot.ScopeService))
	s.handle("GET /api/snapshots/{snap}", s.getSnapshot)
	s.handle("POST /api/snapshots/{snap}/restore", s.restoreSnapshot)
	s.handle("POST /api/snapshots/{snap}/clone", s.cloneSnapshot)
	s.handle("DELETE /api/snapshots/{snap}", s.deleteSnapshot)
}

func (s *Server) snapManager() (*snapshot.Manager, error) {
	if s.Snapshots == nil {
		return nil, notYet(snapshot.ErrNotImplemented)
	}
	return s.Snapshots, nil
}

// listSnapshots answers the snapshots of a project or a database service,
// newest first. An owner that was deleted still lists its snapshots; a
// service that exists and is not a database is a 400.
func (s *Server) listSnapshots(scope string) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		m, err := s.snapManager()
		if err != nil {
			return err
		}
		id := r.PathValue("id")
		if scope == snapshot.ScopeService {
			for _, sv := range s.Store.Snapshot().Services {
				if sv.ID == id && !model.IsDatabase(sv.Kind) {
					return apperr.BadRequest("%s is not a database", sv.Name)
				}
			}
		}
		all, err := m.List(id)
		if err != nil {
			return err
		}
		list := []snapshot.Manifest{}
		for _, mf := range all {
			if mf.Scope == scope {
				list = append(list, mf)
			}
		}
		writeJSON(w, 200, list)
		return nil
	}
}

func (s *Server) takeSnapshot(scope string) handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		m, err := s.snapManager()
		if err != nil {
			return err
		}
		var in struct {
			Name    string `json:"name"`
			Restart bool   `json:"restart"`
		}
		if err := decode(w, r, &in); err != nil {
			return err
		}
		j, err := m.Take(scope, r.PathValue("id"), snapshot.TakeOptions{Name: in.Name, Restart: in.Restart})
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusAccepted, j)
		return nil
	}
}

func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) error {
	m, err := s.snapManager()
	if err != nil {
		return err
	}
	d, err := m.Get(r.PathValue("snap"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, d)
	return nil
}

func (s *Server) restoreSnapshot(w http.ResponseWriter, r *http.Request) error {
	m, err := s.snapManager()
	if err != nil {
		return err
	}
	var in struct {
		Restart bool `json:"restart"`
	}
	if r.ContentLength != 0 {
		if err := decode(w, r, &in); err != nil {
			return err
		}
	}
	j, err := m.Restore(r.PathValue("snap"), in.Restart)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, j)
	return nil
}

func (s *Server) cloneSnapshot(w http.ResponseWriter, r *http.Request) error {
	m, err := s.snapManager()
	if err != nil {
		return err
	}
	var in struct {
		Name         string `json:"name"`
		DomainSuffix string `json:"domainSuffix"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	j, err := m.Clone(r.PathValue("snap"), snapshot.CloneOptions{Name: in.Name, DomainSuffix: in.DomainSuffix})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, j)
	return nil
}

func (s *Server) deleteSnapshot(w http.ResponseWriter, r *http.Request) error {
	m, err := s.snapManager()
	if err != nil {
		return err
	}
	if err := m.Delete(r.PathValue("snap")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
