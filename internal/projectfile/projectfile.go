// Package projectfile exports a project as zero2dev.json and imports one: from the file, or from a
// folder that holds it (a cloned repository). The file describes what the project is made of, not
// where it lives on one PC: paths are relative to the file's folder, runtimes are a kind and a
// version range, services refer to each other by name, and nothing secret is ever written.
//
// It shares its shapes with New project (internal/frameworks, internal/newproject): the same
// runtime selection, installs, port and domain checks and validation, so an import is a plan first
// and a job second.
//
// Contract: docs/API.md, "Amendment: 0.0.6, part 2" and its "Project file details" subsection.
package projectfile

import (
	"time"

	"zero2dev/internal/catalog"
	"zero2dev/internal/installer"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/netutil"
	"zero2dev/internal/newproject"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

// FileName is the name of the project file; an import of a folder looks for it there.
const FileName = "zero2dev.json"

// Format is the only version field: a file with another format is refused.
const Format = 1

// File is zero2dev.json.
type File struct {
	Format   int       `json:"format"` // 1
	Name     string    `json:"name"`
	Runtimes []Runtime `json:"runtimes"`
	Services []Service `json:"services"`
	Sites    []Site    `json:"sites"`
}

// Runtime is a runtime kind and the versions the project works with (inclusive, "8.2" or "20.0.0";
// empty means no bound). It is never a path or an id.
type Runtime struct {
	Kind string `json:"kind"`
	Min  string `json:"min,omitempty"`
	Max  string `json:"max,omitempty"`
}

// Service is a service of the project. Ref is a name that is unique in the file and what the other
// services and the sites point at; nothing in the file is a Zero2Dev id.
type Service struct {
	Ref        string            `json:"ref"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Command    string            `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Settings   map[string]string `json:"settings,omitempty"`
	CustomArgs bool              `json:"customArgs,omitempty"`
	// Cwd is relative to the folder of the file, with "/" ("." is that folder itself).
	Cwd    string `json:"cwd"`
	Port   int    `json:"port,omitempty"`
	Health string `json:"healthPath,omitempty"`
	// Env holds the variables that are not secret; a key that looks like one (PASSWORD, SECRET,
	// TOKEN, KEY, CREDENTIAL, AUTH, PRIVATE, SESSION ...) is never written.
	Env            map[string]string `json:"env,omitempty"`
	EnvFile        string            `json:"envFile,omitempty"`
	Autostart      bool              `json:"autostart,omitempty"`
	RestartOnCrash bool              `json:"restartOnCrash,omitempty"`
	RestartLimit   int               `json:"restartLimit,omitempty"`
	DependsOn      []string          `json:"dependsOn,omitempty"` // refs
	Database       string            `json:"database,omitempty"`  // a ref of a database service
	// Runtime is set only when the service named one runtime; a service that follows the default
	// has none.
	Runtime *Runtime `json:"runtime,omitempty"`
}

// Site is a local site of the project.
type Site struct {
	Name string `json:"name"`
	// Domain is written as the label before the suffix ("shop" for shop.localhost) so an import
	// uses the importing PC's domain suffix.
	Domain       string `json:"domain"`
	Type         string `json:"type"`           // proxy | php | static
	Root         string `json:"root,omitempty"` // relative to the folder of the file, with "/"
	Service      string `json:"service,omitempty"`
	UpstreamPort int    `json:"upstreamPort,omitempty"`
	HTTPS        bool   `json:"https"`
	Enabled      bool   `json:"enabled"`
}

// ExportRequest is the body of POST /api/projects/{id}/export.
type ExportRequest struct {
	// Path is the file to write (the file dialog's Save as). A folder is the same as
	// <folder>\zero2dev.json. An existing file is replaced only with Overwrite.
	Path      string `json:"path"`
	Overwrite bool   `json:"overwrite"`
}

// ExportResult is the answer of an export.
type ExportResult struct {
	Path string `json:"path"` // the file written
	// Warnings say what was left out or could not be made relative: "Variable DB_PASSWORD of
	// Shop API was not written", "The folder of Worker is outside <dir>: Worker was left out"...
	// (an absolute path is never written: the service is skipped with a warning).
	Warnings []string `json:"warnings"`
}

// ImportRequest is the body of POST /api/projects/import/plan and POST /api/projects/import.
type ImportRequest struct {
	// Path is a zero2dev.json, or a folder that holds one (a cloned repository). The project's
	// folders are resolved against the file's folder.
	Path string `json:"path"`
	// Name replaces the project's name (default: the file's).
	Name string `json:"name"`
}

// Plan is POST /api/projects/import/plan: what the import would do, without doing anything.
type Plan struct {
	Path     string   `json:"path"`     // the file read
	Dir      string   `json:"dir"`      // its folder: the base of every relative path
	Name     string   `json:"name"`     // the project that is created
	Services []string `json:"services"` // "Shop API (PHP, port 8000)"
	Sites    []string `json:"sites"`    // "https://shop.localhost -> Shop API"
	// Installs are the runtimes that are missing and will be installed first.
	Installs []newproject.Install `json:"installs"`
	// Changes lists what differs from the file because this PC needs it: a port taken (8000 -> 8001),
	// a domain taken, a database service that is created.
	Changes  []string `json:"changes"`
	Problems []string `json:"problems"` // stop the import (unknown format or kind, a folder that does not exist, a cycle)
	Warnings []string `json:"warnings"`
}

// Logger is what the import needs from the supervisor (*supervisor.Supervisor): a line in a
// service's log.
type Logger interface {
	Log(id, stream, text string)
}

// Manager does the work. Fields may be replaced in tests before first use.
type Manager struct {
	Home      string
	St        *store.Store
	Sup       Logger
	Installer newproject.InstallerAPI
	Runtimes  *runtimes.Manager
	Sites     newproject.SitesAPI
	Jobs      *jobs.Registry
	// Emit publishes events (services.changed, sites.changed, projects.changed); may be nil.
	Emit func(typ string, data any)

	// Catalog lists the downloadable runtimes (nil: catalog.Load of Home).
	Catalog func() ([]model.CatalogEntry, error)
	// PortFree reports whether a TCP port can be listened on (nil: netutil.PortFree).
	PortFree func(port int) bool
	// Timing knobs; zero means the default (the same as New project).
	PollEvery   time.Duration // how often an install job is looked at (300 ms)
	InstallWait time.Duration // how long an install may take (30 min)
}

// New returns a Manager.
func New(home string, st *store.Store, sup *supervisor.Supervisor, inst *installer.Installer, rt *runtimes.Manager,
	sm *sites.Manager, j *jobs.Registry, emit func(string, any)) *Manager {
	m := &Manager{Home: home, St: st, Runtimes: rt, Jobs: j, Emit: emit}
	// A nil pointer must not become a non-nil interface.
	if sup != nil {
		m.Sup = sup
	}
	if inst != nil {
		m.Installer = inst
	}
	if sm != nil {
		m.Sites = sm
	}
	return m
}

func (m *Manager) emit(typ string) {
	if m.Emit != nil {
		m.Emit(typ, struct{}{})
	}
}

func (m *Manager) portFree(p int) bool {
	if m.PortFree != nil {
		return m.PortFree(p)
	}
	return netutil.PortFree(p)
}

func (m *Manager) catalog() ([]model.CatalogEntry, error) {
	if m.Catalog != nil {
		return m.Catalog()
	}
	return catalog.Load(m.Home)
}
