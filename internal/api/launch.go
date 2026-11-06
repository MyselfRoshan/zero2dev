package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"zero2dev/internal/apperr"
	"zero2dev/internal/launch"
	"zero2dev/internal/model"
)

// launchRuntime opens an installed code editor or database tool.
// Body (optional): {"path": "C:\\work\\app"} opens a folder or file in an
// editor; {"serviceId": "svc_..."} opens a database service in a database tool.
// 204 once the program has started; it is not supervised.
func (s *Server) launchRuntime(w http.ResponseWriter, r *http.Request) error {
	if s.Launch == nil {
		return &apperr.Error{Status: http.StatusNotImplemented, Code: "not_supported", Message: "opening programs is not available"}
	}
	var in struct {
		Path      string `json:"path"`
		ServiceID string `json:"serviceId"`
	}
	if r.ContentLength != 0 {
		if err := decode(w, r, &in); err != nil {
			return err
		}
	}
	d := s.Store.Snapshot()
	var rt *model.Runtime
	for i := range d.Runtimes {
		if d.Runtimes[i].ID == r.PathValue("id") {
			rt = &d.Runtimes[i]
		}
	}
	if rt == nil {
		return apperr.NotFound("runtime %s not found", r.PathValue("id"))
	}
	if !model.Launchable(rt.Kind) {
		return apperr.BadRequest("%s is run as a service, not opened", rt.Name)
	}
	if st, err := os.Stat(rt.Binary); err != nil || st.IsDir() {
		return apperr.Conflict("%s is missing at %s", rt.Name, rt.Binary)
	}

	var args []string
	dir := filepath.Dir(rt.Binary)
	if rt.Kind == "dbeaver" {
		// Keep DBeaver's workspace (connections, scripts) inside the data folder.
		ws := filepath.Join(s.Cfg.Home, "tools", "dbeaver-workspace")
		if err := os.MkdirAll(ws, 0o755); err != nil {
			return apperr.Internal("cannot create the DBeaver workspace: %v", err)
		}
		args = append(args, "-data", ws)
	}
	switch {
	case in.ServiceID != "":
		if model.KindCategory[rt.Kind] != model.CatDBTool {
			return apperr.BadRequest("%s cannot open a database", rt.Name)
		}
		var svc *model.Service
		for i := range d.Services {
			if d.Services[i].ID == in.ServiceID {
				svc = &d.Services[i]
			}
		}
		if svc == nil {
			return apperr.NotFound("service %s not found", in.ServiceID)
		}
		if !model.IsDatabase(svc.Kind) {
			return apperr.BadRequest("service %q is not a database", svc.Name)
		}
		ca, err := launch.ConnectArgs(rt.Kind, *svc)
		if errors.Is(err, launch.ErrNoConnection) {
			return apperr.BadRequest("%s cannot open %s databases", rt.Name, svc.Kind)
		} else if err != nil {
			return apperr.BadRequest("%v", err)
		}
		args = append(args, ca...)
	case in.Path != "":
		if !filepath.IsAbs(in.Path) {
			return apperr.BadRequest("path must be absolute")
		}
		p := filepath.Clean(in.Path)
		st, err := os.Stat(p)
		if err != nil {
			return apperr.BadRequest("%s does not exist", p)
		}
		if st.IsDir() {
			dir = p
		} else {
			dir = filepath.Dir(p)
		}
		args = append(args, p)
	}
	if err := s.Launch(rt.Binary, args, dir); err != nil {
		return apperr.Internal("%v", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
