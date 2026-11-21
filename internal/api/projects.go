package api

import (
	"net/http"
	"slices"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
	"zero2dev/internal/validate"
)

// Projects group services and sites that are started and stopped together
// (docs/API.md "Projects"). The names and signatures below are the contract
// with services.go, sites.go and server.go.

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) error {
	ps := s.Store.Snapshot().Projects
	if ps == nil {
		ps = []model.Project{}
	}
	writeJSON(w, http.StatusOK, ps)
	return nil
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) error {
	var in model.ProjectInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	var created model.Project
	err := s.Store.Update(func(d *store.Data) error {
		p, err := validateProject(in, *d)
		if err != nil {
			return err
		}
		p.ID = model.NewID("prj_")
		d.Projects = append(d.Projects, p)
		created = p
		return nil
	})
	if err != nil {
		return err
	}
	s.Hub.Publish("projects.changed", struct{}{})
	writeJSON(w, http.StatusCreated, created)
	return nil
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var in model.ProjectInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	var updated model.Project
	err := s.Store.Update(func(d *store.Data) error {
		for i := range d.Projects {
			if d.Projects[i].ID != id {
				continue
			}
			p, err := validateProject(in, *d)
			if err != nil {
				return err
			}
			p.ID = id
			d.Projects[i] = p
			updated = p
			return nil
		}
		return apperr.NotFound("project %s not found", id)
	})
	if err != nil {
		return err
	}
	s.Hub.Publish("projects.changed", struct{}{})
	writeJSON(w, http.StatusOK, updated)
	return nil
}

// deleteProject removes the group only; its services and sites stay.
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	err := s.Store.Update(func(d *store.Data) error {
		for i := range d.Projects {
			if d.Projects[i].ID == id {
				d.Projects = append(d.Projects[:i], d.Projects[i+1:]...)
				return nil
			}
		}
		return apperr.NotFound("project %s not found", id)
	})
	if err != nil {
		return err
	}
	s.Hub.Publish("projects.changed", struct{}{})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// validateProject normalises and checks a ProjectInput (see internal/validate).
func validateProject(in model.ProjectInput, d store.Data) (model.Project, error) {
	return validate.Project(in, d)
}

// projectByID returns the project with that id, or a 404.
func (s *Server) projectByID(id string) (model.Project, error) {
	for _, p := range s.Store.Snapshot().Projects {
		if p.ID == id {
			return p, nil
		}
	}
	return model.Project{}, apperr.NotFound("project %s not found", id)
}

// projectParam is the project named by ?project=id, or nil when the query has
// none (the bulk routes then act on everything).
func (s *Server) projectParam(r *http.Request) (*model.Project, error) {
	id := r.URL.Query().Get("project")
	if id == "" {
		return nil, nil
	}
	p, err := s.projectByID(id)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// startProject brings a project up: it enables the project's sites with one
// bulk change (one config write, one site server restart), makes sure the
// site server service is among the services to start when the project has a
// site, and starts the members in the project's start order, each with its
// dependencies (supervisor.StartInOrder, which leaves running services
// alone); the site server has no place in that order. A failure to switch
// the sites on is reported like a failed start and does not stop the
// services from starting. Members and sites deleted since the project was
// read are skipped.
func (s *Server) startProject(p model.Project) supervisor.StartReport {
	d := s.Store.Snapshot()
	ids := existingServices(p.ServiceIDs, d.Services)
	var siteErr error
	var also []string
	if sites := existingSites(p.SiteIDs, d.Sites); len(sites) > 0 {
		if _, err := s.Sites.SetEnabledMany(sites, true); err != nil {
			siteErr = err
		} else {
			_, _ = s.Sites.EnsureServers() // the site server service may not exist yet
			if id := siteServerServiceID(s.Store.Snapshot()); id != "" && !slices.Contains(ids, id) {
				also = append(also, id)
			}
		}
	}
	rep := s.Sup.StartInOrder(ids, also...)
	if siteErr != nil {
		rep.Failed = append(rep.Failed, supervisor.StartFailure{Name: "Sites", Error: "could not enable the sites: " + siteErr.Error()})
	}
	return rep
}

// stopProject stops the project's services only (sites, the site server and
// every other service keep running), in the reverse of the start order. The
// result is never nil-listed.
func (s *Server) stopProject(p model.Project) supervisor.StopReport {
	ids := existingServices(p.ServiceIDs, s.Store.Snapshot().Services)
	return s.Sup.StopInOrder(ids)
}

// existingServices keeps the ids that still name a service, in the order of
// ids (a project's start order). The result is never nil: a nil list would
// mean "every service" to the supervisor.
func existingServices(ids []string, all []model.Service) []string {
	out := []string{}
	for _, id := range ids {
		if slices.ContainsFunc(all, func(sv model.Service) bool { return sv.ID == id }) && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// existingSites keeps the ids that still name a site; never nil, because nil
// means "every site" to Sites.SetEnabledMany.
func existingSites(ids []string, all []model.Site) []string {
	out := []string{}
	for _, st := range all {
		if slices.Contains(ids, st.ID) {
			out = append(out, st.ID)
		}
	}
	return out
}

// siteServerServiceID is the service that serves the sites for the current
// siteServer setting (Caddy, or the managed nginx), or "" when there is none.
func siteServerServiceID(d store.Data) string {
	nginx := sites.Server(d.Settings) == sites.ServerNginx
	for _, sv := range d.Services {
		if (nginx && sites.IsSitesNginx(sv)) || (!nginx && sv.Kind == "caddy") {
			return sv.ID
		}
	}
	return ""
}
