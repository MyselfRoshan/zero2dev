package api

import (
	"net/http"

	"zero2dev/internal/model"
)

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, s.Sites.List())
	return nil
}

func (s *Server) createSite(w http.ResponseWriter, r *http.Request) error {
	var in model.SiteInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	v, err := s.Sites.Create(in)
	if err != nil {
		return err
	}
	writeJSON(w, 201, v)
	return nil
}

func (s *Server) updateSite(w http.ResponseWriter, r *http.Request) error {
	var in model.SiteInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	v, err := s.Sites.Update(r.PathValue("id"), in)
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}

func (s *Server) deleteSite(w http.ResponseWriter, r *http.Request) error {
	if err := s.Sites.Delete(r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
