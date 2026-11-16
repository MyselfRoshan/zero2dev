package api

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/fsutil"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// view returns the stored service with live status overlaid.
func (s *Server) view(id string) (model.Service, error) {
	for _, sv := range s.Store.Snapshot().Services {
		if sv.ID == id {
			return s.Sup.Apply(sv), nil
		}
	}
	return model.Service{}, apperr.NotFound("service %s not found", id)
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) error {
	svcs := s.Store.Snapshot().Services
	out := make([]model.Service, 0, len(svcs))
	for _, sv := range svcs {
		out = append(out, s.Sup.Apply(sv))
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	writeJSON(w, 200, out)
	return nil
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) error {
	v, err := s.view(r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, v)
	return nil
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) error {
	var in model.ServiceInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	var created model.Service
	err := s.Store.Update(func(d *store.Data) error {
		sv, err := validateService(in, *d, "")
		if err != nil {
			return err
		}
		sv.ID = model.NewID("svc_")
		d.Services = append(d.Services, sv)
		created = sv
		return nil
	})
	if err != nil {
		return err
	}
	s.Hub.Publish("services.changed", struct{}{})
	writeJSON(w, 201, s.Sup.Apply(created))
	return nil
}

func (s *Server) updateService(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var in model.ServiceInput
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if st := s.Sup.State(id); st == model.StateStarting || st == model.StateRunning || st == model.StateStopping {
		if _, err := s.view(id); err != nil {
			return err
		}
		return apperr.Conflict("service is %s; stop it before editing", st)
	}
	var updated model.Service
	err := s.Store.Update(func(d *store.Data) error {
		for i := range d.Services {
			if d.Services[i].ID != id {
				continue
			}
			sv, err := validateService(in, *d, id)
			if err != nil {
				return err
			}
			sv.ID = id
			d.Services[i] = sv
			updated = sv
			return nil
		}
		return apperr.NotFound("service %s not found", id)
	})
	if err != nil {
		return err
	}
	s.Hub.Publish("services.changed", struct{}{})
	_ = s.Sites.Regenerate() // sites may point at this service's port
	writeJSON(w, 200, s.Sup.Apply(updated))
	return nil
}

func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if _, err := s.view(id); err != nil {
		return err
	}
	if err := s.Sup.Remove(id); err != nil { // stops it and removes its logs
		return err
	}
	if err := s.Store.Update(func(d *store.Data) error {
		out := d.Services[:0]
		for _, sv := range d.Services {
			if sv.ID != id {
				out = append(out, sv)
			}
		}
		d.Services = out
		return nil
	}); err != nil {
		return err
	}
	s.Hub.Publish("services.changed", struct{}{})
	_ = s.Sites.Regenerate()
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) action(w http.ResponseWriter, r *http.Request, f func(string) error) error {
	id := r.PathValue("id")
	if _, err := s.view(id); err != nil {
		return err
	}
	if err := f(id); err != nil {
		return err
	}
	v, _ := s.view(id)
	writeJSON(w, http.StatusAccepted, v)
	return nil
}

func (s *Server) startService(w http.ResponseWriter, r *http.Request) error {
	return s.action(w, r, s.Sup.StartWithDeps) // starts inactive dependencies first
}
func (s *Server) stopService(w http.ResponseWriter, r *http.Request) error {
	return s.action(w, r, s.Sup.Stop)
}

// POST /api/services/start-all[?project=id]: start every service that is not
// running (or the services of one project), dependencies first. Answers when
// they are running, failed or timed out with {"started": [ids], "failed":
// [{id, name, error}]}.
func (s *Server) startAllServices(w http.ResponseWriter, r *http.Request) error {
	p, err := s.projectParam(r)
	if err != nil {
		return err
	}
	if p != nil {
		writeJSON(w, http.StatusOK, s.startProject(*p))
		return nil
	}
	writeJSON(w, http.StatusOK, s.Sup.StartOrdered(nil))
	return nil
}

// POST /api/services/stop-all[?project=id]: stop every running service,
// dependents first (apps, then databases, then the site server), or only the
// services of one project. Answers when they have stopped (or timed out) with
// {"stopped": [ids], "failed": [{id, name, error}]}; Zero2Dev keeps running.
func (s *Server) stopAllServices(w http.ResponseWriter, r *http.Request) error {
	p, err := s.projectParam(r)
	if err != nil {
		return err
	}
	if p != nil {
		writeJSON(w, http.StatusOK, s.stopProject(*p))
		return nil
	}
	writeJSON(w, http.StatusOK, s.Sup.StopAllOrdered())
	return nil
}

func (s *Server) restartService(w http.ResponseWriter, r *http.Request) error {
	return s.action(w, r, s.Sup.Restart)
}

func (s *Server) serviceLogs(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.view(r.PathValue("id")); err != nil {
		return err
	}
	tail := 500
	if q := r.URL.Query().Get("tail"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			return apperr.BadRequest("tail must be a non-negative integer")
		}
		tail = min(n, 5000)
	}
	writeJSON(w, 200, map[string]any{"lines": s.Sup.Logs(r.PathValue("id"), tail)})
	return nil
}

// validateService normalises and checks a ServiceInput (docs/API.md
// "ServiceInput" and "Auth and request hygiene" rule 8). selfID is the service
// being updated ("" when creating).
func validateService(in model.ServiceInput, d store.Data, selfID string) (model.Service, error) {
	sv := model.Service{
		Name: strings.TrimSpace(in.Name), Kind: in.Kind, Command: strings.TrimSpace(in.Command),
		Args: append([]string{}, in.Args...), Env: map[string]string{}, Port: in.Port,
		HealthPath: strings.TrimSpace(in.HealthPath), RuntimeID: in.RuntimeID, Autostart: in.Autostart,
		State: model.StateStopped, Settings: map[string]string{}, CustomArgs: in.CustomArgs,
	}
	for k, v := range in.Settings {
		sv.Settings[k] = v
	}
	// A client that predates settings sends args but no settings: its args
	// are the whole command line, as before.
	if in.Settings == nil && len(in.Args) > 0 && in.Kind != "custom" {
		sv.CustomArgs = true
	}
	if sv.Kind == "custom" {
		sv.CustomArgs = false // a custom command's args always are the whole list
	}
	if n := len([]rune(sv.Name)); n < 1 || n > 64 {
		return sv, apperr.BadRequest("name must be 1-64 characters")
	}
	if !model.In(sv.Kind, model.ServiceKinds) {
		return sv, apperr.BadRequest("kind must be one of %s", strings.Join(model.ServiceKinds, ", "))
	}
	if sv.Kind == "custom" && sv.Command == "" {
		return sv, apperr.BadRequest("command is required for custom services")
	}
	if strings.ContainsRune(sv.Command, 0) {
		return sv, apperr.BadRequest("command contains invalid characters")
	}
	if len(sv.Args) > 256 || len(in.Env) > 256 {
		return sv, apperr.BadRequest("too many args or env entries")
	}
	for _, a := range sv.Args {
		if strings.ContainsRune(a, 0) {
			return sv, apperr.BadRequest("args contain invalid characters")
		}
	}
	for k, v := range in.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return sv, apperr.BadRequest("invalid env entry %q", k)
		}
		sv.Env[k] = v
	}
	if sv.Port < 0 || sv.Port > 65535 {
		return sv, apperr.BadRequest("port must be 0-65535")
	}
	if sv.HealthPath != "" && !strings.HasPrefix(sv.HealthPath, "/") {
		return sv, apperr.BadRequest("healthPath must start with /")
	}
	if !sv.CustomArgs {
		if err := kinds.Validate(sv.Kind, sv.Settings); err != nil {
			return sv, apperr.BadRequest("%v", err)
		}
	}
	// Databases and the web servers have no project folder: the folder is
	// optional for them (their data lives in the data folder).
	if cwd := strings.TrimSpace(in.Cwd); cwd != "" || kinds.FolderRequired(sv.Kind) {
		if !filepath.IsAbs(cwd) {
			return sv, apperr.BadRequest("cwd must be an absolute path")
		}
		sv.Cwd = filepath.Clean(cwd)
		if st, err := os.Stat(sv.Cwd); err != nil || !st.IsDir() {
			return sv, apperr.BadRequest("cwd %s is not an existing directory", sv.Cwd)
		}
	}
	if sv.RuntimeID != "" {
		found := false
		for _, r := range d.Runtimes {
			found = found || r.ID == sv.RuntimeID
		}
		if !found {
			return sv, apperr.BadRequest("runtime %s not found", sv.RuntimeID)
		}
	}
	if err := validateLinks(&sv, in, d, selfID); err != nil {
		return sv, err
	}
	return sv, nil
}

// validateLinks checks the fields that tie a service to others and to its
// folder: dependsOn, databaseId (a database service), the dependency loop,
// envFile (relative, inside the folder) and the crash restart limit.
func validateLinks(sv *model.Service, in model.ServiceInput, d store.Data, selfID string) error {
	byID := make(map[string]model.Service, len(d.Services))
	for _, o := range d.Services {
		byID[o.ID] = o
	}
	sv.DependsOn = []string{}
	if len(in.DependsOn) > 64 {
		return apperr.BadRequest("too many dependencies (max 64)")
	}
	for _, id := range in.DependsOn {
		if selfID != "" && id == selfID {
			return apperr.BadRequest("a service cannot depend on itself")
		}
		if _, ok := byID[id]; !ok {
			return apperr.BadRequest("dependency %s not found", id)
		}
		if !slices.Contains(sv.DependsOn, id) {
			sv.DependsOn = append(sv.DependsOn, id)
		}
	}
	if id := in.DatabaseID; id != "" {
		if selfID != "" && id == selfID {
			return apperr.BadRequest("a service cannot use itself as its database")
		}
		db, ok := byID[id]
		if !ok {
			return apperr.BadRequest("database %s not found", id)
		}
		if !model.IsDatabase(db.Kind) {
			return apperr.BadRequest("service %q is not a database", db.Name)
		}
		sv.DatabaseID = id
	}

	// The loop check sees the service as it will be saved (a new one has no id yet).
	probe := *sv
	probe.ID = selfID
	if probe.ID == "" {
		probe.ID = "\x00new"
	}
	all := make([]model.Service, 0, len(d.Services)+1)
	for _, o := range d.Services {
		if o.ID != selfID {
			all = append(all, o)
		}
	}
	all = append(all, probe)
	if cyc := supervisor.FindCycle(all); cyc != nil {
		names := make([]string, 0, len(cyc))
		for _, id := range cyc {
			if id == probe.ID {
				names = append(names, sv.Name)
			} else {
				names = append(names, byID[id].Name)
			}
		}
		return apperr.BadRequest("these services depend on each other in a loop: %s", strings.Join(names, " -> "))
	}

	if ef := strings.TrimSpace(in.EnvFile); ef != "" {
		if strings.ContainsRune(ef, 0) || filepath.IsAbs(ef) || filepath.VolumeName(ef) != "" || fsutil.HasWindowsDrivePrefix(ef) || strings.HasPrefix(ef, "/") || strings.HasPrefix(ef, `\`) {
			return apperr.BadRequest("envFile must be a path relative to the service folder")
		}
		if slices.Contains(strings.FieldsFunc(ef, func(r rune) bool { return r == '/' || r == '\\' }), "..") {
			return apperr.BadRequest("envFile must stay inside the service folder (no ..)")
		}
		sv.EnvFile = filepath.Clean(ef)
	}

	sv.RestartOnCrash = in.RestartOnCrash
	switch {
	case in.RestartLimit == 0:
		sv.RestartLimit = model.DefaultRestartLimit
	case in.RestartLimit < 1 || in.RestartLimit > model.MaxRestartLimit:
		return apperr.BadRequest("restartLimit must be 1-%d", model.MaxRestartLimit)
	default:
		sv.RestartLimit = in.RestartLimit
	}
	return nil
}
