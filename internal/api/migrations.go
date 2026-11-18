package api

import (
	"net/http"

	"zero2dev/internal/dbtools"
	"zero2dev/internal/migrate"
)

// Bring in: migrations of installed programs into the data folder (0.0.5,
// docs/API.md "Bring in").
func (s *Server) migrationRoutes() {
	s.handle("GET /api/migrations/sources", s.migrationSources)
	s.handle("POST /api/migrations/databases", s.migrationRemoteDatabases)
	s.handle("GET /api/migrations", s.listMigrations)
	s.handle("POST /api/migrations", s.startMigration)
	s.handle("POST /api/migrations/{id}/revert", s.revertMigration)
}

func (s *Server) migrateManager() (*migrate.Manager, error) {
	if s.Migrate == nil {
		return nil, notYet(migrate.ErrNotImplemented)
	}
	return s.Migrate, nil
}

func (s *Server) migrationSources(w http.ResponseWriter, r *http.Request) error {
	m, err := s.migrateManager()
	if err != nil {
		return err
	}
	list, err := m.Sources(r.Context())
	if err != nil {
		return notYet(err)
	}
	if list == nil {
		list = []migrate.Source{}
	}
	writeJSON(w, 200, list)
	return nil
}

func (s *Server) migrationRemoteDatabases(w http.ResponseWriter, r *http.Request) error {
	m, err := s.migrateManager()
	if err != nil {
		return err
	}
	var in migrate.Database
	if err := decode(w, r, &in); err != nil {
		return err
	}
	list, err := m.ListRemoteDatabases(r.Context(), in)
	if err != nil {
		return notYet(err)
	}
	if list == nil {
		list = []dbtools.Database{}
	}
	writeJSON(w, 200, map[string]any{"databases": list})
	return nil
}

func (s *Server) listMigrations(w http.ResponseWriter, r *http.Request) error {
	m, err := s.migrateManager()
	if err != nil {
		return err
	}
	list, err := m.List()
	if err != nil {
		return notYet(err)
	}
	if list == nil {
		list = []migrate.Migration{}
	}
	writeJSON(w, 200, list)
	return nil
}

func (s *Server) startMigration(w http.ResponseWriter, r *http.Request) error {
	m, err := s.migrateManager()
	if err != nil {
		return err
	}
	var in migrate.Request
	if err := decode(w, r, &in); err != nil {
		return err
	}
	j, err := m.Start(in)
	if err != nil {
		return notYet(err)
	}
	writeJSON(w, http.StatusAccepted, j)
	return nil
}

func (s *Server) revertMigration(w http.ResponseWriter, r *http.Request) error {
	m, err := s.migrateManager()
	if err != nil {
		return err
	}
	var in struct {
		How string `json:"how"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	j, err := m.Revert(r.PathValue("id"), in.How)
	if err != nil {
		return notYet(err)
	}
	writeJSON(w, http.StatusAccepted, j)
	return nil
}
