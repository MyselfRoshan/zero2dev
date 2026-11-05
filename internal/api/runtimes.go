package api

import (
	"net/http"

	"zero2dev/internal/catalog"
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
		CatalogID string `json:"catalogId"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	job, err := s.Installer.Start(in.CatalogID)
	if err != nil {
		return err
	}
	writeJSON(w, 202, job)
	return nil
}

func (s *Server) importRuntime(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		CatalogID string `json:"catalogId"`
		FilePath  string `json:"filePath"`
		SHA256    string `json:"sha256"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	job, err := s.Installer.Import(in.CatalogID, in.FilePath, in.SHA256)
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
	writeJSON(w, 200, catalog.Annotate(es, s.Cfg.Home, s.Store.Snapshot().Runtimes))
	return nil
}
