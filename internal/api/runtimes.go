package api

import (
	"context"
	"net/http"
	"time"

	"zero2dev/internal/apperr"

	"zero2dev/internal/catalog"
	"zero2dev/internal/installer"
)

func (s *Server) listRuntimes(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, s.Runtimes.List())
	return nil
}

func (s *Server) detectRuntimes(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, s.Runtimes.Detect())
	return nil
}

func (s *Server) registerRuntime(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Kind    string `json:"kind"`
		Binary  string `json:"binary"`
		Version string `json:"version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	rt, err := s.Runtimes.Register(in.Kind, in.Binary, in.Version)
	if err != nil {
		return err
	}
	writeJSON(w, 201, rt)
	return nil
}

func (s *Server) installRuntime(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		CatalogID        string `json:"catalogId"`
		AcceptUnverified bool   `json:"acceptUnverified"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	job, err := s.Installer.Start(in.CatalogID, installer.Options{AcceptUnverified: in.AcceptUnverified})
	if err != nil {
		return err
	}
	writeJSON(w, 202, job)
	return nil
}

func (s *Server) importRuntime(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		CatalogID        string `json:"catalogId"`
		FilePath         string `json:"filePath"`
		SHA256           string `json:"sha256"`
		AcceptUnverified bool   `json:"acceptUnverified"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	job, err := s.Installer.Import(in.CatalogID, in.FilePath, in.SHA256, installer.Options{AcceptUnverified: in.AcceptUnverified})
	if err != nil {
		return err
	}
	writeJSON(w, 202, job)
	return nil
}

func (s *Server) deleteRuntime(w http.ResponseWriter, r *http.Request) error {
	if err := s.Runtimes.Delete(r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listCatalog(w http.ResponseWriter, r *http.Request) error {
	es, err := s.Catalog()
	if err != nil {
		return err
	}
	writeJSON(w, 200, catalog.Annotate(es, s.Cfg.BinDir, s.Store.Snapshot().Runtimes))
	return nil
}

func (s *Server) catalogStatus(w http.ResponseWriter, r *http.Request) error {
	if s.Remote == nil {
		writeJSON(w, 200, catalog.Status{Sources: []catalog.SourceStatus{}})
		return nil
	}
	writeJSON(w, 200, s.Remote.Status())
	return nil
}

// refreshCatalog asks the publishers for their current version lists (25 s at most).
func (s *Server) refreshCatalog(w http.ResponseWriter, r *http.Request) error {
	if s.Remote == nil {
		return apperr.Conflict("version discovery is not available")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	st, added, err := s.Remote.Refresh(ctx)
	if err != nil {
		return apperr.Internal("%v", err)
	}
	s.Hub.Publish("catalog.changed", struct{}{})
	writeJSON(w, 200, struct {
		catalog.Status
		Added []string `json:"added"`
	}{st, added})
	return nil
}

func (s *Server) setDefaultRuntime(w http.ResponseWriter, r *http.Request) error {
	rt, err := s.Runtimes.SetDefault(r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, rt)
	return nil
}
