package projectfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/netutil"
	"zero2dev/internal/newproject"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
	"zero2dev/internal/validate"
)

// maxFile is the biggest project file that is read.
const maxFile = 4 << 20

var versionRE = regexp.MustCompile(`^\d+(\.\d+){0,2}$`)

// planned is a Plan with what the job needs to carry it out.
type planned struct {
	Plan
	file File
}

// Plan reads the file and works out what Start would do. It changes nothing.
func (m *Manager) Plan(req ImportRequest) (Plan, error) {
	p, err := m.plan(req)
	if err != nil {
		return Plan{}, err
	}
	return p.Plan, nil
}

// readFile finds and reads the project file named by path (a file, or a folder that holds one).
// What is wrong with it is a problem, not an error; only a missing or relative path is.
func readFile(p string) (file File, filePath, dir string, problem string, err error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return File{}, "", "", "", apperr.BadRequest("path is required")
	}
	if !filepath.IsAbs(p) {
		return File{}, "", "", "", apperr.BadRequest("path must be an absolute path")
	}
	filePath = filepath.Clean(p)
	fi, serr := os.Stat(filePath)
	if serr == nil && fi.IsDir() {
		filePath = filepath.Join(filePath, FileName)
		if _, serr = os.Stat(filePath); serr != nil {
			return File{}, filePath, filepath.Dir(filePath), fmt.Sprintf("The folder %s has no %s.", filepath.Dir(filePath), FileName), nil
		}
	} else if serr != nil {
		return File{}, filePath, filepath.Dir(filePath), fmt.Sprintf("%s does not exist.", filePath), nil
	}
	dir = filepath.Dir(filePath)
	fh, oerr := os.Open(filePath)
	if oerr != nil {
		return File{}, filePath, dir, fmt.Sprintf("%s could not be read: %v.", filePath, oerr), nil
	}
	defer fh.Close()
	b, rerr := io.ReadAll(io.LimitReader(fh, maxFile+1))
	switch {
	case rerr != nil:
		return File{}, filePath, dir, fmt.Sprintf("%s could not be read: %v.", filePath, rerr), nil
	case len(b) > maxFile:
		return File{}, filePath, dir, fmt.Sprintf("%s is too big for a project file (over 4 MiB).", filePath), nil
	}
	if jerr := json.Unmarshal(b, &file); jerr != nil {
		return File{}, filePath, dir, fmt.Sprintf("%s is not a valid project file: %v.", filePath, jerr), nil
	}
	return file, filePath, dir, "", nil
}

func (m *Manager) plan(req ImportRequest) (*planned, error) {
	file, filePath, dir, bad, err := readFile(req.Path)
	if err != nil {
		return nil, err
	}
	p := &planned{file: file}
	p.Path, p.Dir = filePath, dir
	p.Services, p.Sites, p.Installs, p.Changes, p.Problems, p.Warnings = []string{}, []string{}, []newproject.Install{}, []string{}, []string{}, []string{}
	if bad != "" {
		p.Problems = append(p.Problems, bad)
		return p, nil
	}
	d := m.St.Snapshot()
	p.Name = strings.TrimSpace(req.Name)
	if p.Name == "" {
		p.Name = strings.TrimSpace(file.Name)
	}
	if probs := check(file, p.Name, dir, d.Settings.DomainSuffix, &p.Warnings); len(probs) > 0 {
		p.Problems = append(p.Problems, probs...)
		return p, nil
	}
	m.planInstalls(p, d)
	dc := d // build appends to the copy's slices: d is a private snapshot
	b, probs := m.build(&dc, file, p.Name, dir, true)
	p.Problems = append(p.Problems, probs...)
	if b != nil {
		p.Services, p.Sites, p.Changes = b.services, b.sites, append(p.Changes, b.changes...)
		p.Warnings = append(p.Warnings, b.warnings...)
	}
	return p, nil
}

// need is a runtime the file asks for.
type need struct {
	kind, min, max string
	who            string // the first service that needs it
}

// rangeFor is the runtime range of a service: its own, else the file's for the kind; own says it
// is the service's own. ok is false for a service with no runtime (custom).
func rangeFor(f File, s Service) (r Runtime, own, ok bool) {
	kind, ok := runtimeKindOf(s.Kind)
	if !ok {
		return Runtime{}, false, false
	}
	if s.Runtime != nil && s.Runtime.Kind == kind {
		return *s.Runtime, true, true
	}
	for _, r := range f.Runtimes {
		if r.Kind == kind {
			return r, false, true
		}
	}
	return Runtime{Kind: kind}, false, true
}

// needs lists the distinct runtime ranges the services of the file run on.
func needs(f File) []need {
	var out []need
	for _, s := range f.Services {
		r, _, ok := rangeFor(f, s)
		if !ok {
			continue
		}
		if !slices.ContainsFunc(out, func(n need) bool { return n.kind == r.Kind && n.min == r.Min && n.max == r.Max }) {
			out = append(out, need{kind: r.Kind, min: r.Min, max: r.Max, who: s.Name})
		}
	}
	return out
}

// planInstalls adds to p the runtimes that are missing (the same selection as New project).
func (m *Manager) planInstalls(p *planned, d store.Data) {
	es, cerr := m.catalog()
	for _, n := range needs(p.file) {
		if newproject.SelectRuntime(d.Runtimes, n.kind, n.min, n.max) != nil {
			continue
		}
		reason := fmt.Sprintf("%s needs %s", n.who, newproject.KindLabel(n.kind)+newproject.BoundsText(n.min, n.max))
		var e model.CatalogEntry
		ok := false
		if cerr == nil {
			e, ok = newproject.PickEntry(es, n.kind, n.min, n.max)
		}
		switch {
		case !ok:
			p.Problems = append(p.Problems, fmt.Sprintf("No %s version%s is available to install on this PC.", newproject.KindLabel(n.kind), newproject.BoundsText(n.min, n.max)))
		case !slices.ContainsFunc(p.Installs, func(in newproject.Install) bool { return in.CatalogID == e.ID }):
			p.Installs = append(p.Installs, newproject.InstallOf(e, reason))
		}
	}
}

// relPath checks a path of the file (a service folder, a site root, with "/") and joins it below
// dir. "" is returned for an empty path.
func relPath(dir, rel string) (string, error) {
	if rel == "" {
		return "", nil
	}
	if looksAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) || strings.ContainsAny(rel, ":\x00") {
		return "", errors.New("is not a relative path")
	}
	c := path.Clean(strings.ReplaceAll(rel, `\`, "/"))
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", errors.New("is outside the folder of the project file")
	}
	return filepath.Join(dir, filepath.FromSlash(c)), nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func okVersion(v string) bool { return v == "" || versionRE.MatchString(v) }

// check validates the file itself, without looking at this PC's services and sites: the format, the
// kinds, the refs and the paths. Every problem is returned (none: the file is usable).
func check(f File, name, dir, suffix string, warnings *[]string) []string {
	var probs []string
	problem := func(format string, a ...any) { probs = append(probs, fmt.Sprintf(format, a...)) }
	if f.Format != Format {
		problem("%s has format %d: this Zero2Dev reads format %d.", FileName, f.Format, Format)
		return probs
	}
	if n := len([]rune(name)); n < 1 || n > 64 {
		problem("The project name must be 1-64 characters.")
	}
	runtimeOK := func(who string, r Runtime) {
		if !model.In(r.Kind, model.RuntimeKinds) {
			problem("%s: %q is not a runtime kind.", who, r.Kind)
		}
		if !okVersion(r.Min) || !okVersion(r.Max) {
			problem("%s: the versions of the %s runtime must look like 8.2 or 20.0.0.", who, r.Kind)
		}
	}
	for _, r := range f.Runtimes {
		runtimeOK("Runtimes", r)
	}

	byRef := map[string]Service{}
	label := func(i int, s Service) string {
		switch {
		case s.Name != "":
			return s.Name
		case s.Ref != "":
			return s.Ref
		}
		return "Service " + strconv.Itoa(i+1)
	}
	for i, s := range f.Services {
		who := label(i, s)
		switch {
		case strings.TrimSpace(s.Ref) == "":
			problem("%s has no ref.", who)
		case s.Ref != strings.TrimSpace(s.Ref):
			problem("The ref %q of %s must not start or end with a space.", s.Ref, who)
		default:
			if _, dup := byRef[s.Ref]; dup {
				problem("The ref %q is used by more than one service.", s.Ref)
			}
			byRef[s.Ref] = s
		}
		if n := len([]rune(strings.TrimSpace(s.Name))); n < 1 || n > 64 {
			problem("%s: the name must be 1-64 characters.", who)
		}
		if !model.In(s.Kind, model.ServiceKinds) {
			problem("%s: %q is not a service kind (use %s).", who, s.Kind, strings.Join(model.ServiceKinds, ", "))
			continue
		}
		if s.Runtime != nil {
			runtimeOK(who, *s.Runtime)
			if k, ok := runtimeKindOf(s.Kind); !ok || s.Runtime.Kind != k {
				*warnings = append(*warnings, fmt.Sprintf("%s: the runtime %s does not run a %s service and is ignored.", who, s.Runtime.Kind, s.Kind))
			}
		}
		if s.Port < 0 || s.Port > 65535 {
			problem("%s: the port must be 0-65535.", who)
		}
		abs, err := relPath(dir, s.Cwd)
		switch {
		case err != nil:
			problem("The folder of %s (%q) %v.", who, s.Cwd, err)
		case abs == "" && kinds.FolderRequired(s.Kind):
			problem("%s needs a folder (cwd).", who)
		case abs != "" && !isDir(abs):
			problem("The folder %s of %s does not exist.", abs, who)
		}
		if s.EnvFile != "" {
			if ef := strings.ReplaceAll(s.EnvFile, `\`, "/"); looksAbs(ef) || strings.HasPrefix(ef, "/") || strings.ContainsRune(ef, ':') ||
				slices.Contains(strings.Split(ef, "/"), "..") {
				problem("%s: envFile must be a path inside the service folder.", who)
			} else if abs != "" && !fileThere(filepath.Join(abs, filepath.FromSlash(ef))) {
				*warnings = append(*warnings, fmt.Sprintf("%s reads %s, which does not exist yet: create it with the variables the project needs (a project file never holds secrets).", who, ef))
			}
		}
	}
	for i, s := range f.Services {
		who := label(i, s)
		for _, r := range s.DependsOn {
			if _, ok := byRef[r]; !ok {
				problem("%s depends on %q, which is not a service of the file.", who, r)
			}
		}
		if s.Database != "" {
			if db, ok := byRef[s.Database]; !ok {
				problem("%s uses the database %q, which is not a service of the file.", who, s.Database)
			} else if !model.IsDatabase(db.Kind) {
				problem("%s uses %q as its database, but that is not a database service.", who, s.Database)
			}
		}
	}
	if len(probs) == 0 {
		var all []model.Service
		for _, s := range f.Services {
			all = append(all, model.Service{ID: s.Ref, Name: s.Name, DependsOn: s.DependsOn, DatabaseID: s.Database})
		}
		if cyc := supervisor.FindCycle(all); cyc != nil {
			problem("These services depend on each other in a loop: %s.", strings.Join(append(cyc, cyc[0]), " -> "))
		}
	}

	if suffix == "" {
		suffix = "localhost"
	}
	for i, s := range f.Sites {
		who := s.Name
		if who == "" {
			who = "Site " + strconv.Itoa(i+1)
		}
		if n := len([]rune(strings.TrimSpace(s.Name))); n < 1 || n > 64 {
			problem("%s: the name must be 1-64 characters.", who)
		}
		dom := domainOf(s.Domain, suffix)
		if err := sites.CheckDomain(dom, "", store.Data{Settings: model.Settings{DomainSuffix: suffix}}); err != nil {
			problem("The domain %q of the site %s is not usable: %v.", s.Domain, who, err)
		}
		switch s.Type {
		case "static":
			abs, err := relPath(dir, s.Root)
			switch {
			case s.Root == "":
				problem("The static site %s needs a root folder.", who)
			case err != nil:
				problem("The folder of the site %s (%q) %v.", who, s.Root, err)
			case !isDir(abs):
				problem("The folder %s of the site %s does not exist.", abs, who)
			}
		case "proxy", "php":
			if s.Service != "" {
				svc, ok := byRef[s.Service]
				switch {
				case !ok:
					problem("The site %s points at %q, which is not a service of the file.", who, s.Service)
				case s.Type == "php" && svc.Kind != "php":
					problem("The site %s is a php site, but %q is not a php service.", who, s.Service)
				}
			} else if s.Type == "php" {
				problem("The php site %s needs a service.", who)
			} else if s.UpstreamPort < 1 || s.UpstreamPort > 65535 {
				problem("The proxy site %s needs a service or an upstreamPort.", who)
			}
		default:
			problem("The site %s has the type %q (use static, proxy or php).", who, s.Type)
		}
	}
	return probs
}

func fileThere(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// domainOf is the full domain of a file's domain: the label plus this PC's suffix.
func domainOf(label, suffix string) string {
	l := strings.ToLower(strings.TrimSpace(label))
	l = strings.TrimSuffix(l, "."+suffix)
	return l + "." + suffix
}

// built is what an import would add to the state.
type built struct {
	services []string // summaries
	sites    []string
	changes  []string
	warnings []string
	project  model.Project
}

// build adds the services, the sites and the project of the file to d with new ids, free ports
// (the file's when free), unique domains and runtimes matched by kind and range. pending says
// that runtimes may still be missing (the plan; they are installed first). It stops at the first
// service or site that cannot be created. The caller discards d when there are problems.
func (m *Manager) build(d *store.Data, f File, name, dir string, pending bool) (*built, []string) {
	b := &built{}
	var probs []string
	fail := func(format string, a ...any) (*built, []string) {
		return b, append(probs, fmt.Sprintf(format, a...))
	}

	ids := map[string]string{}
	for _, s := range f.Services {
		ids[s.Ref] = model.NewID("svc_")
	}
	used := map[int]bool{d.Settings.ProxyHTTPPort: true, d.Settings.ProxyHTTPSPort: true}
	for _, sv := range d.Services {
		used[sv.Port] = true
	}
	for _, s := range d.Sites {
		used[s.UpstreamPort] = true
	}
	ports := map[string]int{} // ref -> port
	remap := map[int]int{}    // the file's port -> the port used (for proxy sites without a service)
	for _, s := range f.Services {
		if s.Port <= 0 {
			continue
		}
		port := s.Port
		if used[port] || !m.portFree(port) {
			q, ok := netutil.NextFreePort(port, func(p int) bool { return used[p] }, m.portFree)
			if !ok {
				return fail("No free port was found for %s.", s.Name)
			}
			b.changes = append(b.changes, fmt.Sprintf("Port %d is taken: %s uses %d", port, s.Name, q))
			port = q
		}
		used[port] = true
		ports[s.Ref] = port
		if _, ok := remap[s.Port]; !ok {
			remap[s.Port] = port
		}
	}

	// Create the services dependencies first (the checks of a service need the ones it uses).
	byRef := map[string]Service{}
	for _, s := range f.Services {
		byRef[s.Ref] = s
	}
	var order []Service
	done := map[string]bool{}
	var visit func(s Service)
	visit = func(s Service) {
		if done[s.Ref] {
			return
		}
		done[s.Ref] = true
		for _, r := range append(append([]string{}, s.DependsOn...), s.Database) {
			if dep, ok := byRef[r]; ok {
				visit(dep)
			}
		}
		order = append(order, s)
	}
	for _, s := range f.Services {
		visit(s)
	}

	for _, s := range order {
		in := model.ServiceInput{
			Name: s.Name, Kind: s.Kind, Command: s.Command, Args: s.Args, Settings: s.Settings, CustomArgs: s.CustomArgs,
			Env: s.Env, Port: ports[s.Ref], HealthPath: s.Health, Autostart: s.Autostart, EnvFile: s.EnvFile,
			RestartOnCrash: s.RestartOnCrash, RestartLimit: s.RestartLimit, DatabaseID: ids[s.Database],
		}
		if s.Kind != "custom" && in.Settings == nil {
			in.Settings = map[string]string{}
		}
		if s.Kind == "custom" {
			in.Settings = nil
		}
		for _, r := range s.DependsOn {
			in.DependsOn = append(in.DependsOn, ids[r])
		}
		if abs, _ := relPath(dir, s.Cwd); abs != "" {
			in.Cwd = abs
		}
		if r, own, ok := rangeFor(f, s); ok {
			rt := newproject.SelectRuntime(d.Runtimes, r.Kind, r.Min, r.Max)
			switch {
			case rt == nil && pending:
			case rt == nil:
				return fail("No %s%s is installed.", newproject.KindLabel(r.Kind), newproject.BoundsText(r.Min, r.Max))
			case own || !rt.Default:
				in.RuntimeID = rt.ID
			}
		}
		sv, err := validate.Service(in, *d, "")
		if err != nil {
			return fail("Creating the service %s failed: %v", s.Name, err)
		}
		sv.ID = ids[s.Ref]
		if model.IsDatabase(sv.Kind) && slices.ContainsFunc(d.Services, func(o model.Service) bool { return o.Kind == sv.Kind }) {
			b.changes = append(b.changes, fmt.Sprintf("This PC already has a %s service: a separate one is created for the project", newproject.KindLabel(sv.Kind)))
		}
		d.Services = append(d.Services, sv)
	}
	svcByID := map[string]model.Service{}
	for _, sv := range d.Services {
		svcByID[sv.ID] = sv
	}
	for _, s := range f.Services {
		sv := svcByID[ids[s.Ref]]
		line := fmt.Sprintf("%s (%s", sv.Name, newproject.KindLabel(sv.Kind))
		if sv.Port > 0 {
			line += ", port " + strconv.Itoa(sv.Port)
		}
		b.services = append(b.services, line+")")
	}

	suffix := d.Settings.DomainSuffix
	if suffix == "" {
		suffix = "localhost"
	}
	domains := map[string]bool{}
	for _, s := range d.Sites {
		domains[s.Domain] = true
	}
	var siteIDs []string
	for _, s := range f.Sites {
		want := domainOf(s.Domain, suffix)
		label := strings.TrimSuffix(want, "."+suffix)
		dom := want
		for n := 2; domains[dom]; n++ {
			tail := "-" + strconv.Itoa(n)
			l := label
			if len(l)+len(tail) > 63 {
				l = strings.TrimRight(l[:63-len(tail)], "-")
			}
			dom = l + tail + "." + suffix
		}
		if dom != want {
			b.changes = append(b.changes, fmt.Sprintf("Domain %s is taken: %s uses %s", want, s.Name, dom))
		}
		enabled := s.Enabled
		in := model.SiteInput{Name: s.Name, Domain: dom, Type: s.Type, HTTPS: s.HTTPS, Enabled: &enabled}
		target := ""
		switch {
		case s.Type == "static":
			in.Root, _ = relPath(dir, s.Root)
			target = in.Root
		case s.Service != "":
			in.ServiceID = ids[s.Service]
			target = byRef[s.Service].Name
		default:
			in.UpstreamPort = s.UpstreamPort
			if p, ok := remap[s.UpstreamPort]; ok {
				in.UpstreamPort = p
			}
			target = "localhost:" + strconv.Itoa(in.UpstreamPort)
		}
		site, err := sites.Validate(in, "", *d)
		if err != nil {
			return fail("Creating the site %s failed: %v", s.Name, err)
		}
		site.ID = model.NewID("site_")
		domains[site.Domain] = true
		d.Sites = append(d.Sites, site)
		siteIDs = append(siteIDs, site.ID)
		scheme := "http"
		if s.HTTPS {
			scheme = "https"
		}
		b.sites = append(b.sites, fmt.Sprintf("%s://%s -> %s", scheme, site.Domain, target))
	}

	svcIDs := make([]string, 0, len(f.Services))
	for _, s := range f.Services {
		svcIDs = append(svcIDs, ids[s.Ref])
	}
	pr, err := validate.Project(model.ProjectInput{Name: name, ServiceIDs: svcIDs, SiteIDs: siteIDs}, *d)
	if err != nil {
		return fail("Creating the project failed: %v", err)
	}
	pr.ID = model.NewID("prj_")
	d.Projects = append(d.Projects, pr)
	b.project = pr
	return b, probs
}
