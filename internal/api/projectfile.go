package api

import (
	"net/http"

	"zero2dev/internal/apperr"
	"zero2dev/internal/newproject"
	"zero2dev/internal/projectfile"
)

// errProjectFileUnwired is what the routes answer when the server has no project file manager.
var errProjectFileUnwired = &apperr.Error{Status: http.StatusNotImplemented, Code: "not_supported", Message: "not available in this build yet"}

// 0.0.6, part 2 (docs/API.md, "Amendment: 0.0.6, part 2"): export a project as zero2dev.json and
// import one.
func (s *Server) projectFileRoutes() {
	s.handle("POST /api/projects/{id}/export", s.exportProject)
	s.handle("POST /api/projects/import/plan", s.planProjectImport)
	s.handle("POST /api/projects/import", s.startProjectImport)
}

func (s *Server) exportProject(w http.ResponseWriter, r *http.Request) error {
	if s.ProjectFile == nil {
		return errProjectFileUnwired
	}
	var in projectfile.ExportRequest
	if err := decode(w, r, &in); err != nil {
		return err
	}
	res, err := s.ProjectFile.Export(r.PathValue("id"), in)
	if err != nil {
		return err
	}
	if res.Warnings == nil {
		res.Warnings = []string{}
	}
	writeJSON(w, 200, res)
	return nil
}

func (s *Server) planProjectImport(w http.ResponseWriter, r *http.Request) error {
	if s.ProjectFile == nil {
		return errProjectFileUnwired
	}
	var in projectfile.ImportRequest
	if err := decode(w, r, &in); err != nil {
		return err
	}
	p, err := s.ProjectFile.Plan(in)
	if err != nil {
		return err
	}
	for _, l := range []*[]string{&p.Services, &p.Sites, &p.Changes, &p.Problems, &p.Warnings} {
		if *l == nil {
			*l = []string{}
		}
	}
	if p.Installs == nil {
		p.Installs = []newproject.Install{}
	}
	writeJSON(w, 200, p)
	return nil
}

func (s *Server) startProjectImport(w http.ResponseWriter, r *http.Request) error {
	if s.ProjectFile == nil {
		return errProjectFileUnwired
	}
	var in projectfile.ImportRequest
	if err := decode(w, r, &in); err != nil {
		return err
	}
	j, err := s.ProjectFile.Start(in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, j)
	return nil
}
