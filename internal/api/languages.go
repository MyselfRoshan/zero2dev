package api

import (
	"net/http"

	"zero2dev/internal/apperr"
	"zero2dev/internal/detect"
	"zero2dev/internal/frameworks"
	"zero2dev/internal/kinds"
	"zero2dev/internal/newproject"
	"zero2dev/internal/tasks"
)

// 0.0.6 (docs/API.md, "Amendment: languages, tasks and new projects"): one-off tasks of a service,
// project detection, the frameworks list and New project.
func (s *Server) languageRoutes() {
	s.handle("GET /api/services/{id}/tasks", s.listTasks)
	s.handle("POST /api/services/{id}/tasks", s.startTask)
	s.handle("GET /api/project/detect", s.detectProject)
	s.handle("GET /api/frameworks", s.listFrameworks)
	s.handle("POST /api/new-project/plan", s.planNewProject)
	s.handle("POST /api/new-project", s.startNewProject)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) error {
	if s.Tasks == nil {
		return notYet(tasks.ErrNotImplemented)
	}
	l, err := s.Tasks.List(r.PathValue("id"))
	if err != nil {
		return notYet(err)
	}
	if l.Presets == nil {
		l.Presets = []kinds.Task{}
	}
	if l.Scripts == nil {
		l.Scripts = []tasks.Script{}
	}
	writeJSON(w, 200, l)
	return nil
}

func (s *Server) startTask(w http.ResponseWriter, r *http.Request) error {
	if s.Tasks == nil {
		return notYet(tasks.ErrNotImplemented)
	}
	var in tasks.Request
	if err := decode(w, r, &in); err != nil {
		return err
	}
	j, err := s.Tasks.Start(r.PathValue("id"), in)
	if err != nil {
		return notYet(err)
	}
	writeJSON(w, http.StatusAccepted, j)
	return nil
}

func (s *Server) detectProject(w http.ResponseWriter, r *http.Request) error {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		return apperr.BadRequest("dir is required")
	}
	res, err := detect.Dir(dir)
	if err != nil {
		return notYet(err)
	}
	if res.Settings == nil {
		res.Settings = map[string]string{}
	}
	if res.Markers == nil {
		res.Markers = []string{}
	}
	writeJSON(w, 200, res)
	return nil
}

func (s *Server) listFrameworks(w http.ResponseWriter, r *http.Request) error {
	list, err := frameworks.Load(s.Cfg.Home)
	if err != nil {
		return apperr.Conflict("%v", err)
	}
	writeJSON(w, 200, map[string]any{"frameworks": list})
	return nil
}

func (s *Server) planNewProject(w http.ResponseWriter, r *http.Request) error {
	if s.NewProject == nil {
		return notYet(newproject.ErrNotImplemented)
	}
	var in newproject.Request
	if err := decode(w, r, &in); err != nil {
		return err
	}
	p, err := s.NewProject.Plan(in)
	if err != nil {
		return notYet(err)
	}
	if p.Installs == nil {
		p.Installs = []newproject.Install{}
	}
	for _, l := range []*[]string{&p.Steps, &p.Creates, &p.Problems, &p.Warnings} {
		if *l == nil {
			*l = []string{}
		}
	}
	writeJSON(w, 200, p)
	return nil
}

func (s *Server) startNewProject(w http.ResponseWriter, r *http.Request) error {
	if s.NewProject == nil {
		return notYet(newproject.ErrNotImplemented)
	}
	var in newproject.Request
	if err := decode(w, r, &in); err != nil {
		return err
	}
	j, err := s.NewProject.Start(in)
	if err != nil {
		return notYet(err)
	}
	writeJSON(w, http.StatusAccepted, j)
	return nil
}
