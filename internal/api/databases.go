package api

import (
	"errors"
	"net/http"

	"zero2dev/internal/apperr"
	"zero2dev/internal/dbtools"
	"zero2dev/internal/migrate"
	"zero2dev/internal/snapshot"
)

// notYet turns a Wave 0 stub's error into 501 (the feature is not built yet).
func notYet(err error) error {
	if errors.Is(err, dbtools.ErrNotImplemented) || errors.Is(err, snapshot.ErrNotImplemented) || errors.Is(err, migrate.ErrNotImplemented) {
		return &apperr.Error{Status: http.StatusNotImplemented, Code: "not_supported", Message: "not available in this build yet"}
	}
	return err
}

// Database tools of a database service (0.0.5, docs/API.md "Databases").
func (s *Server) databaseRoutes() {
	s.handle("GET /api/services/{id}/database", s.dbInfo)
	s.handle("POST /api/services/{id}/database/databases", s.dbCreateDatabase)
	s.handle("POST /api/services/{id}/database/users", s.dbCreateUser)
	s.handle("POST /api/services/{id}/database/import", s.dbImport)
	s.handle("POST /api/services/{id}/database/export", s.dbExport)
	s.handle("POST /api/services/{id}/database/reset", s.dbReset)
	s.handle("PUT /api/services/{id}/database/password", s.dbPassword)
}

func (s *Server) dbManager() (*dbtools.Manager, error) {
	if s.DB == nil {
		return nil, notYet(dbtools.ErrNotImplemented)
	}
	return s.DB, nil
}

func (s *Server) dbInfo(w http.ResponseWriter, r *http.Request) error {
	m, err := s.dbManager()
	if err != nil {
		return err
	}
	info, err := m.Info(r.Context(), r.PathValue("id"))
	if err != nil {
		return notYet(err)
	}
	writeJSON(w, 200, info)
	return nil
}

func (s *Server) dbCreateDatabase(w http.ResponseWriter, r *http.Request) error {
	m, err := s.dbManager()
	if err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := m.CreateDatabase(r.Context(), r.PathValue("id"), in.Name); err != nil {
		return notYet(err)
	}
	writeJSON(w, 201, map[string]string{"name": in.Name})
	return nil
}

func (s *Server) dbCreateUser(w http.ResponseWriter, r *http.Request) error {
	m, err := s.dbManager()
	if err != nil {
		return err
	}
	var in struct {
		User     string `json:"user"`
		Password string `json:"password"`
		Database string `json:"database"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if err := m.CreateUser(r.Context(), r.PathValue("id"), in.User, in.Password, in.Database); err != nil {
		return notYet(err)
	}
	writeJSON(w, 201, map[string]string{"user": in.User, "database": in.Database})
	return nil
}

func (s *Server) dbImport(w http.ResponseWriter, r *http.Request) error {
	m, err := s.dbManager()
	if err != nil {
		return err
	}
	var in struct {
		Database string `json:"database"`
		FilePath string `json:"filePath"`
		Create   bool   `json:"create"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	j, err := m.Import(r.PathValue("id"), in.Database, in.FilePath, in.Create)
	if err != nil {
		return notYet(err)
	}
	writeJSON(w, http.StatusAccepted, j)
	return nil
}

func (s *Server) dbExport(w http.ResponseWriter, r *http.Request) error {
	m, err := s.dbManager()
	if err != nil {
		return err
	}
	var in struct {
		Database  string `json:"database"`
		FilePath  string `json:"filePath"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	j, err := m.Export(r.PathValue("id"), in.Database, in.FilePath, in.Overwrite)
	if err != nil {
		return notYet(err)
	}
	writeJSON(w, http.StatusAccepted, j)
	return nil
}

func (s *Server) dbReset(w http.ResponseWriter, r *http.Request) error {
	m, err := s.dbManager()
	if err != nil {
		return err
	}
	sv, err := m.Reset(r.PathValue("id"))
	if err != nil {
		return notYet(err)
	}
	s.Hub.Publish("services.changed", struct{}{})
	writeJSON(w, 200, s.Sup.Apply(sv))
	return nil
}

func (s *Server) dbPassword(w http.ResponseWriter, r *http.Request) error {
	m, err := s.dbManager()
	if err != nil {
		return err
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	sv, err := m.SetPassword(r.Context(), r.PathValue("id"), in.Password)
	if err != nil {
		return notYet(err)
	}
	s.Hub.Publish("services.changed", struct{}{})
	writeJSON(w, 200, s.Sup.Apply(sv))
	return nil
}
