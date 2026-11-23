package projectfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/catalog"
	"zero2dev/internal/fsutil"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// secretWords are the parts of a variable name (case-insensitive) that make its value a secret:
// such a variable is never written to a project file.
var secretWords = []string{"PASSWORD", "SECRET", "TOKEN", "KEY", "CREDENTIAL", "AUTH", "PRIVATE", "SIGNATURE", "COOKIE", "SESSION"}

// IsSecretKey reports whether an environment variable (or setting) name looks like it holds a secret.
func IsSecretKey(key string) bool {
	u := strings.ToUpper(key)
	for _, w := range secretWords {
		if strings.Contains(u, w) {
			return true
		}
	}
	return false
}

// looksAbs recognises a Windows absolute path ("C:\x", "C:/x", "\\server\share") on any OS; a value
// like "/api" is not one.
func looksAbs(s string) bool {
	return fsutil.HasWindowsDrivePrefix(s) || strings.HasPrefix(s, `\\`)
}

// Export writes the project as a project file. It is a 404 for an unknown project and a 409 for an
// existing file without Overwrite.
func (m *Manager) Export(projectID string, req ExportRequest) (ExportResult, error) {
	d := m.St.Snapshot()
	var pr *model.Project
	for i := range d.Projects {
		if d.Projects[i].ID == projectID {
			pr = &d.Projects[i]
		}
	}
	if pr == nil {
		return ExportResult{}, apperr.NotFound("project %s not found", projectID)
	}
	file, err := exportTarget(req)
	if err != nil {
		return ExportResult{}, err
	}
	f, warnings := buildFile(d, *pr, filepath.Dir(file))
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return ExportResult{}, apperr.Internal("the project file could not be made: %v", err)
	}
	if err := fsutil.WriteFileAtomic(file, buf.Bytes(), 0o644); err != nil {
		return ExportResult{}, apperr.Internal("writing %s failed: %v", file, err)
	}
	return ExportResult{Path: file, Warnings: warnings}, nil
}

// exportTarget turns the request's path into the file to write: a folder is <folder>\zero2dev.json.
func exportTarget(req ExportRequest) (string, error) {
	p := strings.TrimSpace(req.Path)
	if p == "" {
		return "", apperr.BadRequest("path is required")
	}
	if !filepath.IsAbs(p) {
		return "", apperr.BadRequest("path must be an absolute path")
	}
	p = filepath.Clean(p)
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		p = filepath.Join(p, FileName)
	}
	if fi, err := os.Stat(filepath.Dir(p)); err != nil || !fi.IsDir() {
		return "", apperr.BadRequest("the folder %s does not exist", filepath.Dir(p))
	}
	if _, err := os.Stat(p); err == nil && !req.Overwrite {
		return "", apperr.Conflict("%s exists", p)
	}
	return p, nil
}

var nonRef = regexp.MustCompile(`[^a-z0-9]+`)

// refOf derives a ref from a name ("My Shop" -> "my-shop"); kind when the name has no letters.
func refOf(name, kind string) string {
	r := strings.Trim(nonRef.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if r == "" {
		r = kind
	}
	return r
}

// relTo is abs relative to base with "/" ("." is base itself); false when abs is not inside base.
func relTo(base, abs string) (string, bool) {
	if !filepath.IsAbs(abs) {
		return "", false
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// versionMin is the major.minor of a runtime version ("8.2.12" -> "8.2"); "" when it is not a number.
func versionMin(v string) string {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	var out []string
	for _, p := range parts {
		if len(out) == 2 || p == "" || strings.Trim(p, "0123456789") != "" {
			break
		}
		out = append(out, p)
	}
	return strings.Join(out, ".")
}

// buildFile makes the project file of pr: base is the folder of the file. It never contains an id,
// a binary, an absolute path, a password, the contents of an env file or a variable that looks like
// a secret; what was left out is in the warnings.
func buildFile(d store.Data, pr model.Project, base string) (File, []string) {
	f := File{Format: Format, Name: pr.Name, Runtimes: []Runtime{}, Services: []Service{}, Sites: []Site{}}
	warnings := []string{}
	warn := func(format string, a ...any) { warnings = append(warnings, fmt.Sprintf(format, a...)) }

	svcByID := map[string]model.Service{}
	for _, s := range d.Services {
		svcByID[s.ID] = s
	}
	rtByID := map[string]model.Runtime{}
	for _, r := range d.Runtimes {
		rtByID[r.ID] = r
	}

	// Which services are written, and under which ref.
	refs := map[string]string{} // service id -> ref
	cwds := map[string]string{}
	usedRef := map[string]bool{}
	var kept []model.Service
	for _, id := range pr.ServiceIDs {
		sv, ok := svcByID[id]
		if !ok {
			continue
		}
		if why := notPortable(sv); why != "" {
			warn("%s was left out: %s", sv.Name, why)
			continue
		}
		cwd := ""
		if sv.Cwd != "" {
			rel, ok := relTo(base, sv.Cwd)
			if !ok {
				warn("The folder of %s is outside %s: %s was left out", sv.Name, base, sv.Name)
				continue
			}
			cwd = rel
		}
		ref := refOf(sv.Name, sv.Kind)
		for n := 2; usedRef[ref]; n++ {
			ref = fmt.Sprintf("%s-%d", refOf(sv.Name, sv.Kind), n)
		}
		usedRef[ref] = true
		refs[id], cwds[id] = ref, cwd
		kept = append(kept, sv)
	}

	rangeOf := map[string]*Runtime{} // runtime kind -> the lowest range a service named
	var kinds []string
	for _, sv := range kept {
		out := Service{
			Ref: refs[sv.ID], Name: sv.Name, Kind: sv.Kind, Command: sv.Command, Cwd: cwds[sv.ID], Port: sv.Port,
			Health: sv.HealthPath, EnvFile: filepath.ToSlash(sv.EnvFile), Autostart: sv.Autostart, RestartOnCrash: sv.RestartOnCrash,
			CustomArgs: sv.CustomArgs,
		}
		if sv.RestartLimit != model.DefaultRestartLimit {
			out.RestartLimit = sv.RestartLimit
		}
		for _, a := range sv.Args {
			if k, _, ok := strings.Cut(a, "="); ok && IsSecretKey(k) {
				warn("An argument (%s) of %s was not written", k, sv.Name)
				continue
			}
			out.Args = append(out.Args, a)
		}
		for _, k := range slices.Sorted(maps.Keys(sv.Settings)) {
			v := sv.Settings[k]
			switch {
			case IsSecretKey(k):
				warn("Setting %s of %s was not written", k, sv.Name)
			case looksAbs(v):
				warn("Setting %s of %s was not written: its value is an absolute path", k, sv.Name)
			default:
				if out.Settings == nil {
					out.Settings = map[string]string{}
				}
				out.Settings[k] = v
			}
		}
		for _, k := range slices.Sorted(maps.Keys(sv.Env)) {
			v := sv.Env[k]
			switch {
			case IsSecretKey(k):
				warn("Variable %s of %s was not written", k, sv.Name)
			case looksAbs(v):
				warn("Variable %s of %s was not written: its value is an absolute path", k, sv.Name)
			default:
				if out.Env == nil {
					out.Env = map[string]string{}
				}
				out.Env[k] = v
			}
		}
		for _, dep := range sv.DependsOn {
			if r, ok := refs[dep]; ok {
				out.DependsOn = append(out.DependsOn, r)
			} else {
				warn("%s depends on a service that is not in the project file: the link was not written", sv.Name)
			}
		}
		if sv.DatabaseID != "" {
			if r, ok := refs[sv.DatabaseID]; ok {
				out.Database = r
			} else {
				warn("%s uses a database service that is not in the project file: the link was not written", sv.Name)
			}
		}
		if kind, ok := runtimeKindOf(sv.Kind); ok {
			if !slices.Contains(kinds, kind) {
				kinds = append(kinds, kind)
			}
			if rt, ok := rtByID[sv.RuntimeID]; ok && rt.Kind == kind {
				r := &Runtime{Kind: kind, Min: versionMin(rt.Version)}
				out.Runtime = r
				if cur := rangeOf[kind]; cur == nil || r.Min != "" && (cur.Min == "" || catalog.CompareVersions(r.Min, cur.Min) < 0) {
					rangeOf[kind] = r
				}
			}
		}
		if sv.AdminPassword != "" {
			warn("The administrator password of %s was not written", sv.Name)
		}
		f.Services = append(f.Services, out)
	}
	for _, k := range kinds {
		r := Runtime{Kind: k}
		if own := rangeOf[k]; own != nil {
			r = *own
		}
		f.Runtimes = append(f.Runtimes, r)
	}

	suffix := d.Settings.DomainSuffix
	siteByID := map[string]model.Site{}
	for _, s := range d.Sites {
		siteByID[s.ID] = s
	}
	for _, id := range pr.SiteIDs {
		s, ok := siteByID[id]
		if !ok {
			continue
		}
		out := Site{Name: s.Name, Domain: domainLabel(s.Domain, suffix), Type: s.Type, HTTPS: s.HTTPS, Enabled: s.Enabled}
		switch {
		case s.Type == "static":
			rel, ok := relTo(base, s.Root)
			if !ok {
				warn("The folder of the site %s is outside %s: the site was left out", s.Domain, base)
				continue
			}
			out.Root = rel
		case s.ServiceID != "":
			r, ok := refs[s.ServiceID]
			if !ok {
				warn("The service of the site %s is not in the project file: the site was left out", s.Domain)
				continue
			}
			out.Service = r
		default:
			out.UpstreamPort = s.UpstreamPort
		}
		f.Sites = append(f.Sites, out)
	}
	return f, warnings
}

// notPortable says why a service cannot be written without an absolute path ("" when it can).
func notPortable(sv model.Service) string {
	if looksAbs(sv.Command) {
		return "its command is an absolute path"
	}
	for _, a := range sv.Args {
		if looksAbs(a) {
			return "an argument is an absolute path"
		}
	}
	return ""
}

// runtimeKindOf is the runtime kind a service of this kind runs on (the same name), false for
// "custom".
func runtimeKindOf(serviceKind string) (string, bool) {
	if serviceKind != "custom" && model.In(serviceKind, model.RuntimeKinds) {
		return serviceKind, true
	}
	return "", false
}

// domainLabel is the part of a domain before the suffix ("shop" for shop.localhost).
func domainLabel(domain, suffix string) string {
	for _, s := range []string{suffix, "localhost", "test"} {
		if s != "" && strings.HasSuffix(domain, "."+s) {
			return strings.TrimSuffix(domain, "."+s)
		}
	}
	first, _, _ := strings.Cut(domain, ".")
	return first
}
