package newproject

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/catalog"
	"zero2dev/internal/frameworks"
	"zero2dev/internal/model"
	"zero2dev/internal/netutil"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
)

// planned is a Plan with what the job needs to carry it out.
type planned struct {
	Plan
	req  Request
	fw   frameworks.Framework
	vals Values // without the database password and the venv
	// rt is the installed runtime the framework uses (nil: it is installed first, or none needed).
	rt *model.Runtime
	// editor is the runtime that opens the folder (nil: none).
	editor *model.Runtime
	// ports are the ports of fw.Services, in order (0: the service has none).
	ports []int
	// newDBPort is the port of the database service when one is created.
	newDBPort int
}

var kindLabels = map[string]string{"php": "PHP", "node": "Node.js", "python": "Python", "go": "Go", "java": "Java", "rust": "Rust",
	"mariadb": "MariaDB", "mysql": "MySQL", "postgres": "PostgreSQL", "composer": "Composer"}

func kindLabel(kind string) string {
	if l := kindLabels[kind]; l != "" {
		return l
	}
	return kind
}

var dbDefaultPort = map[string]int{"mariadb": 3306, "mysql": 3306, "postgres": 5432}

// systemNames cannot be a project's database or user.
var systemNames = map[string]bool{"mysql": true, "information_schema": true, "performance_schema": true, "sys": true,
	"postgres": true, "template0": true, "template1": true, "root": true, "mariadb": true}

func (m *Manager) catalog() ([]model.CatalogEntry, error) {
	if m.Catalog != nil {
		return m.Catalog()
	}
	return catalog.Load(m.Home)
}

func (m *Manager) portFree(p int) bool {
	if m.PortFree != nil {
		return m.PortFree(p)
	}
	return netutil.PortFree(p)
}

// Plan works out what Start would do. It changes nothing. Everything that stops a project from
// being made is a problem of the plan (the answer is still 200); only an unknown framework or a
// damaged frameworks list is an error.
func (m *Manager) Plan(req Request) (Plan, error) {
	p, err := m.plan(req)
	if err != nil {
		return Plan{}, err
	}
	return p.Plan, nil
}

func (m *Manager) plan(req Request) (*planned, error) {
	list, err := frameworks.Load(m.Home)
	if err != nil {
		return nil, apperr.Conflict("%v", err)
	}
	fw, ok := frameworks.Get(list, req.Framework)
	if !ok {
		return nil, apperr.BadRequest("unknown framework %q", req.Framework)
	}
	p := &planned{req: req, fw: fw}
	p.Installs, p.Steps, p.Creates, p.Problems, p.Warnings = []Install{}, []string{}, []string{}, []string{}, []string{}
	problem := func(f string, a ...any) { p.Problems = append(p.Problems, fmt.Sprintf(f, a...)) }
	warn := func(f string, a ...any) { p.Warnings = append(p.Warnings, fmt.Sprintf(f, a...)) }
	d := m.St.Snapshot()

	// name, folder
	req.Name = strings.TrimSpace(req.Name)
	p.vals = Values{Name: req.Name, Home: m.Home, DBHost: "127.0.0.1", Rand: m.Rand}
	if msg := CheckName(req.Name); msg != "" {
		problem("%s", msg)
	} else if p.Slug = Slugify(req.Name); p.Slug == "" {
		problem("The name needs at least one letter or digit (a-z, 0-9).")
	} else if ReservedName(p.Slug) {
		problem("%q is a reserved name on Windows. Choose another name.", p.Slug)
		p.Slug = ""
	}
	p.vals.Slug = p.Slug
	parent := strings.TrimSpace(req.Parent)
	switch {
	case parent == "":
		problem("Choose the folder that will hold the project.")
	case !filepath.IsAbs(parent):
		problem("The folder %s is not an absolute path.", parent)
	default:
		parent = filepath.Clean(parent)
		p.vals.Parent = parent
		if fi, err := os.Stat(parent); err != nil || !fi.IsDir() {
			problem("The folder %s does not exist.", parent)
		} else if p.Slug != "" {
			p.Dir = filepath.Join(parent, p.Slug)
			p.vals.Dir = p.Dir
			if fi, err := os.Stat(p.Dir); err == nil {
				entries, rerr := os.ReadDir(p.Dir)
				switch {
				case !fi.IsDir():
					problem("%s exists and is a file.", p.Dir)
				case rerr != nil || len(entries) > 0:
					problem("The folder %s already exists and is not empty.", p.Dir)
				default:
					warn("The folder %s exists and is empty, it will be used.", p.Dir)
				}
			}
		}
	}

	// domain
	if fw.Site != nil {
		dom := strings.ToLower(strings.TrimSpace(req.Domain))
		if dom == "" && p.Slug != "" {
			dom = p.Slug + ".localhost"
		}
		p.Domain, p.vals.Domain = dom, dom
		if dom != "" {
			if err := sites.CheckDomain(dom, "", d); err != nil {
				problem("%s", err.Error())
			}
		}
	}

	// port
	p.ports = make([]int, len(fw.Services))
	used := usedPorts(d)
	taken := func(q int) bool { return used[q] }
	main := true
	for i, tpl := range fw.Services {
		if tpl.Port <= 0 {
			continue
		}
		want := tpl.Port
		if main && req.Port != 0 {
			want = req.Port
		}
		switch {
		case main && (req.Port < 0 || req.Port > 65535):
			problem("The port must be between 1 and 65535.")
		case main && req.Port != 0 && used[want]:
			problem("Port %d is already used by %s.", want, portOwner(d, want))
		case main && req.Port != 0 && !m.portFree(want):
			problem("Port %d is in use by another program.", want)
		case req.Port == 0 || !main:
			if used[want] || !m.portFree(want) {
				q, ok := netutil.NextFreePort(want, taken, m.PortFree)
				if !ok {
					problem("No free port was found.")
					break
				}
				want = q
			}
		}
		used[want] = true
		p.ports[i] = want
		if main {
			p.Port, p.vals.Port = want, want
			main = false
		}
	}

	// runtime and Composer
	rts := d.Runtimes
	if k := fw.Runtime.Kind; k != "" {
		rt := selectRuntime(rts, k, fw.Runtime.Min, fw.Runtime.Max)
		reason := fmt.Sprintf("%s needs %s", fw.Label, kindLabel(k)+boundsText(fw.Runtime.Min, fw.Runtime.Max))
		if rt != nil {
			p.rt = rt
		} else if e, ok := m.pickEntry(k, fw.Runtime.Min, fw.Runtime.Max); ok {
			p.Installs = append(p.Installs, installOf(e, reason))
		} else {
			problem("No %s version%s is available to install on this PC.", kindLabel(k), boundsText(fw.Runtime.Min, fw.Runtime.Max))
		}
	}
	if fw.Composer && !hasComposer(rts) {
		if e, ok := m.pickEntry("composer", "", ""); ok {
			p.Installs = append(p.Installs, installOf(e, fw.Label+" is made with Composer"))
		} else {
			problem("Composer is not installed and no Composer download is available for this PC.")
		}
	}

	// database
	var dbKindFor string
	switch {
	case fw.Database == nil && req.DatabaseKind != "":
		problem("%s does not use a database.", fw.Label)
	case fw.Database == nil:
	case req.DatabaseKind == "" && !fw.Database.Optional:
		problem("%s needs a database: choose %s.", fw.Label, strings.Join(fw.Database.Kinds, " or "))
	case req.DatabaseKind == "":
	case !slices.Contains(fw.Database.Kinds, req.DatabaseKind):
		problem("%s is not offered for %s (use %s).", req.DatabaseKind, fw.Label, strings.Join(fw.Database.Kinds, ", "))
	default:
		dbKindFor = req.DatabaseKind
	}
	if dbKindFor != "" && p.Slug != "" {
		db := &DatabasePlan{Kind: dbKindFor, Name: dbName(p.Slug, 63), User: dbName(p.Slug, 32)}
		p.Database = db
		p.vals.DBKind, p.vals.DB, p.vals.DBUser = dbKindFor, db.Name, db.User
		if systemNames[db.Name] || systemNames[db.User] {
			problem("The name %s is used by the database server itself. Choose another name.", db.Name)
		}
		if sv := findDBService(d, dbKindFor, m); sv != nil {
			db.ServiceID = sv.ID
			p.vals.DBPort = sv.Port
		} else {
			port, ok := netutil.NextFreePort(dbDefaultPort[dbKindFor]-1, taken, m.PortFree)
			if !ok {
				problem("No free port was found for the database server.")
			}
			p.newDBPort, p.vals.DBPort = port, port
			used[port] = true
			if !hasRuntime(rts, dbKindFor) {
				if e, ok := m.pickEntry(dbKindFor, "", ""); ok {
					p.Installs = append(p.Installs, installOf(e, fmt.Sprintf("%s needs a %s server", fw.Label, kindLabel(dbKindFor))))
				} else {
					problem("No %s download is available for this PC.", kindLabel(dbKindFor))
				}
			}
		}
	}

	// editor
	if req.OpenEditor {
		for _, k := range []string{"vscode", "vscodium"} {
			for i := range rts {
				if rts[i].Kind == k && p.editor == nil && fileExists(rts[i].Binary) {
					p.editor = &rts[i]
				}
			}
		}
		if p.editor == nil && m.Launch != nil {
			warn("No code editor (VS Code or VSCodium) is installed: the folder will not be opened.")
		}
	}
	if fw.Site != nil && !slices.ContainsFunc(rts, func(r model.Runtime) bool { return r.Kind == "caddy" || r.Kind == "nginx" }) {
		warn("No site server is installed. The site is created, install Caddy on the Runtimes page to open %s.", p.Domain)
	}
	for _, st := range fw.Steps {
		if st.Type == frameworks.StepDownload && st.Unverified {
			warn("%s: %s.", st.Label, strings.TrimRight(st.Reason, "."))
		}
	}

	m.describe(p)
	return p, nil
}

// describe fills Steps and Creates in words.
func (m *Manager) describe(p *planned) {
	fw := p.fw
	for _, in := range p.Installs {
		p.Steps = append(p.Steps, "Installing "+in.Name)
	}
	if db := p.Database; db != nil {
		if db.ServiceID == "" {
			p.Steps = append(p.Steps, fmt.Sprintf("Starting %s and creating the database %s", kindLabel(db.Kind), db.Name))
		} else {
			p.Steps = append(p.Steps, fmt.Sprintf("Creating the database %s on %s", db.Name, kindLabel(db.Kind)))
		}
	}
	for _, st := range fw.Steps {
		label, err := p.vals.Expand(st.Label)
		if err != nil {
			label = st.Label
		}
		p.Steps = append(p.Steps, label)
	}
	if len(p.envLines()) > 0 {
		p.Steps = append(p.Steps, "Writing the "+p.envFile()+" file")
	}
	p.Steps = append(p.Steps, "Creating the project")
	if p.editor != nil {
		p.Steps = append(p.Steps, "Opening the folder in "+p.editor.Name)
	}

	if p.Dir != "" {
		p.Creates = append(p.Creates, "Folder "+p.Dir)
	}
	for i, tpl := range fw.Services {
		name, _ := p.vals.Expand(tpl.Name)
		s := fmt.Sprintf("Service %s (%s", name, kindLabel(tpl.Kind))
		if p.ports[i] > 0 {
			s += ", port " + strconv.Itoa(p.ports[i])
		}
		p.Creates = append(p.Creates, s+")")
	}
	if db := p.Database; db != nil {
		if db.ServiceID == "" {
			p.Creates = append(p.Creates, fmt.Sprintf("Service %s (%s, port %d)", kindLabel(db.Kind), kindLabel(db.Kind), p.newDBPort))
		}
		p.Creates = append(p.Creates, fmt.Sprintf("Database %s with the user %s (all privileges on it only)", db.Name, db.User))
	}
	if len(p.envLines()) > 0 {
		p.Creates = append(p.Creates, "File "+p.envFile()+" with the connection settings")
	}
	if fw.Site != nil && p.Domain != "" {
		scheme := "http"
		if fw.Site.HTTPS {
			scheme = "https"
		}
		p.Creates = append(p.Creates, fmt.Sprintf("Site %s://%s", scheme, p.Domain))
	}
	p.Creates = append(p.Creates, "Project "+p.req.Name)
}

// envFile is the .env file the framework writes, relative to the project folder.
func (p *planned) envFile() string {
	if p.fw.EnvFile != "" {
		return p.fw.EnvFile
	}
	return ".env"
}

// envLines are the framework's env lines that apply: those that need the database only when the
// project has one.
func (p *planned) envLines() []frameworks.EnvLine {
	var out []frameworks.EnvLine
	for _, l := range p.fw.Env {
		if p.Database == nil && frameworks.UsesDatabase(l.Value) {
			continue
		}
		out = append(out, l)
	}
	return out
}

func usedPorts(d store.Data) map[int]bool {
	used := map[int]bool{d.Settings.ProxyHTTPPort: true, d.Settings.ProxyHTTPSPort: true}
	for _, sv := range d.Services {
		if sv.Port > 0 {
			used[sv.Port] = true
		}
	}
	for _, s := range d.Sites {
		if s.UpstreamPort > 0 {
			used[s.UpstreamPort] = true
		}
	}
	return used
}

func portOwner(d store.Data, port int) string {
	for _, sv := range d.Services {
		if sv.Port == port {
			return "the service " + sv.Name
		}
	}
	for _, s := range d.Sites {
		if s.UpstreamPort == port {
			return "the site " + s.Domain
		}
	}
	return "Zero2Dev's site server"
}

// dbName is the database or user name for a slug: letters, digits and _, at most max characters.
func dbName(slug string, max int) string {
	s := strings.ReplaceAll(slug, "-", "_")
	if len(s) > max {
		s = s[:max]
	}
	return s
}

// findDBService returns an existing database service of the kind: a running one first.
func findDBService(d store.Data, kind string, m *Manager) *model.Service {
	var first *model.Service
	for i := range d.Services {
		sv := &d.Services[i]
		if sv.Kind != kind || sv.Port <= 0 {
			continue
		}
		if m.Sup != nil && m.Sup.State(sv.ID) == model.StateRunning {
			return sv
		}
		if first == nil {
			first = sv
		}
	}
	return first
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func hasRuntime(rts []model.Runtime, kind string) bool {
	return slices.ContainsFunc(rts, func(r model.Runtime) bool { return r.Kind == kind && fileExists(r.Binary) })
}

func hasComposer(rts []model.Runtime) bool {
	return slices.ContainsFunc(rts, func(r model.Runtime) bool {
		return r.Kind == "composer" && r.Binary != "" && fileExists(model.ComposerPhar(r))
	})
}

func installOf(e model.CatalogEntry, reason string) Install {
	name := e.Name
	if name == "" {
		name = strings.TrimSpace(kindLabel(e.Kind) + " " + e.Version)
	}
	return Install{Kind: e.Kind, Version: e.Version, CatalogID: e.ID, Name: name, Reason: reason}
}

func boundsText(min, max string) string {
	switch {
	case min != "" && max != "":
		return " " + min + " to " + max
	case min != "":
		return " " + min + " or newer"
	case max != "":
		return " " + max + " or older"
	}
	return ""
}

// fits reports whether version is inside the inclusive bounds. A bound with fewer parts than the
// version compares only those parts (max "8.4" accepts 8.4.9).
func fits(version, min, max string) bool {
	if min != "" && catalog.CompareVersions(version, min) < 0 {
		return false
	}
	if max != "" {
		n := len(strings.Split(max, "."))
		parts := strings.Split(version, ".")
		if len(parts) > n {
			parts = parts[:n]
		}
		if catalog.CompareVersions(strings.Join(parts, "."), max) > 0 {
			return false
		}
	}
	return true
}

// selectRuntime picks the installed runtime of kind inside the bounds: the default of the kind
// when it fits, else the newest that does. nil when none fits.
func selectRuntime(rts []model.Runtime, kind, min, max string) *model.Runtime {
	var best *model.Runtime
	for i := range rts {
		r := &rts[i]
		if r.Kind != kind || !fileExists(r.Binary) || !fits(r.Version, min, max) {
			continue
		}
		switch {
		case best == nil, r.Default && !best.Default,
			r.Default == best.Default && catalog.CompareVersions(r.Version, best.Version) > 0:
			best = r
		}
	}
	return best
}

// pickEntry is the newest catalog entry of kind that this machine can run and that is inside the
// bounds; entries of the "mainline" channel only when nothing else fits.
func (m *Manager) pickEntry(kind, min, max string) (model.CatalogEntry, bool) {
	es, err := m.catalog()
	if err != nil {
		return model.CatalogEntry{}, false
	}
	return PickEntry(es, kind, min, max)
}

// PickEntry is pickEntry on a given catalog (shared with the project file import).
func PickEntry(es []model.CatalogEntry, kind, min, max string) (model.CatalogEntry, bool) {
	var best, mainline *model.CatalogEntry
	for i := range es {
		e := &es[i]
		if e.Kind != kind || !catalog.Supported(*e) || !fits(e.Version, min, max) {
			continue
		}
		slot := &best
		if e.Channel == "mainline" {
			slot = &mainline
		}
		if *slot == nil || catalog.CompareVersions(e.Version, (*slot).Version) > 0 {
			*slot = e
		}
	}
	if best == nil {
		best = mainline
	}
	if best == nil {
		return model.CatalogEntry{}, false
	}
	return *best, true
}

// SelectRuntime is selectRuntime for other packages: the installed runtime of kind inside the
// bounds (the default of the kind when it fits, else the newest), nil when none fits.
func SelectRuntime(rts []model.Runtime, kind, min, max string) *model.Runtime {
	return selectRuntime(rts, kind, min, max)
}

// KindLabel is the human name of a runtime or service kind ("PHP", "Node.js").
func KindLabel(kind string) string { return kindLabel(kind) }

// BoundsText says a version range in words (" 8.2 or newer"); "" for no bounds.
func BoundsText(min, max string) string { return boundsText(min, max) }

// InstallOf is the Install the plan shows for a catalog entry.
func InstallOf(e model.CatalogEntry, reason string) Install { return installOf(e, reason) }
