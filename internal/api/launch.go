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

	args, env, err := toolDataArgs(*rt, s.Cfg.Home)
	if err != nil {
		return apperr.Internal("cannot prepare the data folder of %s: %v", rt.Name, err)
	}
	dir := filepath.Dir(rt.Binary)
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
	if err := s.Launch(rt.Binary, args, dir, env); err != nil {
		return apperr.Internal("%v", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// toolDataArgs keeps the settings, extensions and workspaces of a tool that
// Zero2Dev installed in <home>/<name> (data\code, data\codium, ...), so the
// bin folder only holds programs. Each tool has its own switch for it. Tools
// registered by path are the user's own installs and keep their usual
// settings, so they get nothing.
func toolDataArgs(rt model.Runtime, home string) (args, env []string, err error) {
	name := model.DataDirName(rt.Kind)
	if rt.Source != "installed" || name == "" {
		return nil, nil, nil
	}
	dir := filepath.Join(home, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	switch rt.Kind {
	case "vscode", "vscodium":
		// Portable mode with the data outside the program folder: VS Code and
		// VSCodium keep user-data and extensions in $VSCODE_PORTABLE.
		return nil, []string{"VSCODE_PORTABLE=" + dir}, nil
	case "notepadpp":
		return []string{"-settingsDir=" + dir}, nil, nil
	case "heidisql":
		// The portable build writes portable_settings.txt next to heidisql.exe
		// unless told otherwise; the file is created so the setting is used.
		// O_EXCL: two launches at once must never truncate settings the
		// other (or HeidiSQL itself) already wrote.
		f := filepath.Join(dir, "portable_settings.txt")
		fh, err := os.OpenFile(f, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			err = fh.Close()
		} else if errors.Is(err, os.ErrExist) {
			err = nil
		}
		if err != nil {
			return nil, nil, err
		}
		return []string{"--psettings=" + f}, nil, nil
	case "dbeaver":
		ws := filepath.Join(dir, "workspace")
		if err := os.MkdirAll(ws, 0o755); err != nil {
			return nil, nil, err
		}
		return []string{"-data", ws}, nil, nil
	}
	return nil, nil, nil
}
