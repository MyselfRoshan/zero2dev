package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
)

// GET /api/kinds: what each service kind needs (internal/kinds/profiles.json).
func (s *Server) listKinds(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, err := w.Write(kinds.JSON())
	return err
}

// POST /api/services/preview: the command line a ServiceInput would run,
// without saving or writing anything. Problems become warnings.
func (s *Server) previewService(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		model.ServiceInput
		ID string `json:"id"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if !model.In(in.Kind, model.ServiceKinds) {
		return apperr.BadRequest("kind must be one of %s", strings.Join(model.ServiceKinds, ", "))
	}
	svc := model.Service{
		ID: in.ID, Name: in.Name, Kind: in.Kind, Command: strings.TrimSpace(in.Command), Args: in.Args,
		Settings: in.Settings, CustomArgs: in.CustomArgs, Env: in.Env, Port: in.Port, RuntimeID: in.RuntimeID,
	}
	if svc.ID == "" || strings.ContainsAny(svc.ID, `/\:.`) {
		svc.ID = "svc_new"
	}
	if in.Settings == nil && len(in.Args) > 0 && in.Kind != "custom" {
		svc.CustomArgs = true
	}
	if in.Kind == "custom" {
		svc.CustomArgs = false
	}
	if c := strings.TrimSpace(in.Cwd); c != "" && filepath.IsAbs(c) {
		svc.Cwd = filepath.Clean(c)
	}
	writeJSON(w, 200, s.Sup.Preview(svc))
	return nil
}

// GET /api/services/{id}/command: the command line a saved service runs.
func (s *Server) serviceCommand(w http.ResponseWriter, r *http.Request) error {
	for _, sv := range s.Store.Snapshot().Services {
		if sv.ID == r.PathValue("id") {
			writeJSON(w, 200, s.Sup.Preview(sv))
			return nil
		}
	}
	return apperr.NotFound("service %s not found", r.PathValue("id"))
}

func (s *Server) runtimeByID(id string) (model.Runtime, error) {
	for _, rt := range s.Store.Snapshot().Runtimes {
		if rt.ID == id {
			return rt, nil
		}
	}
	return model.Runtime{}, apperr.NotFound("runtime %s not found", id)
}

// GET /api/runtimes/{id}/php-extensions: the extensions in a PHP runtime's ext folder.
func (s *Server) phpExtensions(w http.ResponseWriter, r *http.Request) error {
	rt, err := s.runtimeByID(r.PathValue("id"))
	if err != nil {
		return err
	}
	if rt.Kind != "php" {
		return apperr.BadRequest("%s is not a PHP runtime", rt.Name)
	}
	exts := kinds.ListExtensions(rt.Binary)
	hasXdebug := false
	for _, e := range exts {
		hasXdebug = hasXdebug || e.Name == "xdebug"
	}
	_, extErr := os.Stat(kinds.ExtDir(rt.Binary))
	writeJSON(w, 200, map[string]any{
		"runtimeId": rt.ID, "version": rt.Version, "extDir": kinds.ExtDir(rt.Binary), "managed": extErr == nil,
		"extensions": exts, "xdebug": hasXdebug,
	})
	return nil
}

// POST /api/runtimes/{id}/xdebug: download Xdebug for a PHP runtime (a job).
func (s *Server) installXdebug(w http.ResponseWriter, r *http.Request) error {
	rt, err := s.runtimeByID(r.PathValue("id"))
	if err != nil {
		return err
	}
	j, err := s.Installer.InstallXdebug(rt)
	if err != nil {
		return err
	}
	writeJSON(w, 202, j)
	return nil
}

// GET /api/project/scripts?dir=: the script names in <dir>/package.json.
func (s *Server) projectScripts(w http.ResponseWriter, r *http.Request) error {
	dir := r.URL.Query().Get("dir")
	if !filepath.IsAbs(dir) {
		return apperr.BadRequest("dir must be an absolute path")
	}
	out := []string{}
	b, err := os.ReadFile(filepath.Join(filepath.Clean(dir), "package.json"))
	if err == nil && len(b) < 4<<20 {
		var pkg struct {
			Scripts map[string]any `json:"scripts"`
		}
		if json.Unmarshal(b, &pkg) == nil {
			for k := range pkg.Scripts {
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	writeJSON(w, 200, map[string]any{"scripts": out})
	return nil
}
