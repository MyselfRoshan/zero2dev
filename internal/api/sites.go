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

func (s *Server) enableSite(w http.ResponseWriter, r *http.Request) error {
	return s.setSiteEnabled(w, r, true)
}

func (s *Server) disableSite(w http.ResponseWriter, r *http.Request) error {
	return s.setSiteEnabled(w, r, false)
}

func (s *Server) setSiteEnabled(w http.ResponseWriter, r *http.Request, on bool) error {
	v, err := s.Sites.SetEnabled(r.PathValue("id"), on)
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

// POST /api/sites/enable-all and /disable-all[?project=id]: switch every site
// (or the sites of one project) on or off with one config write and one site
// server restart. Answers {"changed": n}, the number of sites that changed.
func (s *Server) enableAllSites(w http.ResponseWriter, r *http.Request) error {
	return s.setAllSitesEnabled(w, r, true)
}

func (s *Server) disableAllSites(w http.ResponseWriter, r *http.Request) error {
	return s.setAllSitesEnabled(w, r, false)
}

func (s *Server) setAllSitesEnabled(w http.ResponseWriter, r *http.Request, on bool) error {
	p, err := s.projectParam(r)
	if err != nil {
		return err
	}
	var ids []string // nil: every site
	if p != nil {
		ids = append([]string{}, p.SiteIDs...) // non-nil: an empty project has no sites
	}
	n, err := s.Sites.SetEnabledMany(ids, on)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]int{"changed": n})
	return nil
}
