// Package validate holds the checks the HTTP API makes on a service or a project before it is
// stored (docs/API.md "ServiceInput", "ProjectInput" and "Auth and request hygiene" rule 8), so
// New project (internal/newproject) creates its services and project exactly as the API would.
package validate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/fsutil"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

// Service normalises and checks a ServiceInput (docs/API.md
// "ServiceInput" and "Auth and request hygiene" rule 8). selfID is the service
// being updated ("" when creating).
func Service(in model.ServiceInput, d store.Data, selfID string) (model.Service, error) {
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
	if err := Links(&sv, in, d, selfID); err != nil {
		return sv, err
	}
	return sv, nil
}

// Links checks the fields that tie a service to others and to its
// folder: dependsOn, databaseId (a database service), the dependency loop,
// envFile (relative, inside the folder) and the crash restart limit.
func Links(sv *model.Service, in model.ServiceInput, d store.Data, selfID string) error {
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
		// Reject parent segments before cleaning can erase them.
		if slices.Contains(strings.FieldsFunc(ef, func(r rune) bool { return r == '/' || r == '\\' }), "..") {
			return apperr.BadRequest("envFile must stay inside the service folder (no ..)")
		}
		// A colon anywhere is a drive ("C:.env", also "./C:.env", which Windows' Clean keeps as
		// `.\C:.env`) or an NTFS stream (".env:x"); neither is a file inside the folder. Checked
		// before cleaning, so the answer is the same on every OS.
		if strings.ContainsRune(ef, ':') {
			return apperr.BadRequest("envFile must be a path relative to the service folder")
		}
		ef = filepath.Clean(ef)
		if strings.ContainsRune(ef, 0) || filepath.IsAbs(ef) || filepath.VolumeName(ef) != "" || fsutil.HasWindowsDrivePrefix(ef) || strings.HasPrefix(ef, "/") || strings.HasPrefix(ef, `\`) {
			return apperr.BadRequest("envFile must be a path relative to the service folder")
		}
		sv.EnvFile = ef
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

// Project normalises and checks a ProjectInput: a name of 1-64
// characters, and service / site ids that exist (listed once each).
func Project(in model.ProjectInput, d store.Data) (model.Project, error) {
	p := model.Project{Name: strings.TrimSpace(in.Name), ServiceIDs: []string{}, SiteIDs: []string{}}
	if n := len([]rune(p.Name)); n < 1 || n > 64 {
		return p, apperr.BadRequest("name must be 1-64 characters")
	}
	svc, site := map[string]bool{}, map[string]bool{}
	for _, sv := range d.Services {
		svc[sv.ID] = true
	}
	for _, st := range d.Sites {
		site[st.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range in.ServiceIDs {
		if !svc[id] {
			return p, apperr.BadRequest("service %s not found", id)
		}
		if !seen["s"+id] {
			seen["s"+id] = true
			p.ServiceIDs = append(p.ServiceIDs, id)
		}
	}
	for _, id := range in.SiteIDs {
		if !site[id] {
			return p, apperr.BadRequest("site %s not found", id)
		}
		if !seen["t"+id] {
			seen["t"+id] = true
			p.SiteIDs = append(p.SiteIDs, id)
		}
	}
	return p, nil
}
