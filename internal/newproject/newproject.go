// Package newproject makes a project from a framework of internal/frameworks in one job: it
// installs what is missing (the runtime version the framework needs, Composer, a database server),
// runs the framework's own generator, creates the database and its user, writes the .env, and
// creates the project's services, its site and the project itself, then opens the folder in the
// installed editor. If a step fails it removes only what it created and keeps the generated
// folder (never deleting files it did not write).
//
// Contract: docs/API.md, "Amendment: languages, tasks and new projects (0.0.6)" and its "New
// project details" subsection.
package newproject

import (
	"context"
	"errors"
	"net/http"
	"time"

	"zero2dev/internal/dbtools"
	"zero2dev/internal/installer"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
	"zero2dev/internal/tasks"
)

// ErrNotImplemented is what the API answers 501 for: a server without a Manager.
var ErrNotImplemented = errors.New("not implemented yet")

// Request is the body of POST /api/new-project/plan and POST /api/new-project.
type Request struct {
	Framework string `json:"framework"` // id in the frameworks list
	Name      string `json:"name"`      // "My Shop"
	// Parent is the folder that will hold the project: the project folder is <Parent>\<slug>. It
	// must exist; <Parent>\<slug> must not exist or be empty.
	Parent string `json:"parent"`
	// DatabaseKind is "mariadb", "mysql" or "postgres" to give the project a database (a server
	// of that kind is used when one is installed, else installed and created), "" for none.
	DatabaseKind string `json:"databaseKind"`
	// Domain defaults to <slug>.localhost. Port 0 picks a free one for the main service.
	Domain string `json:"domain"`
	Port   int    `json:"port"`
	// OpenEditor opens the folder in the first installed editor when the project is made.
	OpenEditor bool `json:"openEditor"`
}

// Install is a runtime Zero2Dev installs first.
type Install struct {
	Kind      string `json:"kind"`
	Version   string `json:"version"`
	CatalogID string `json:"catalogId"`
	Name      string `json:"name"`   // "Node.js 22.11.0"
	Reason    string `json:"reason"` // "Laravel needs PHP 8.2 or newer"
}

// DatabasePlan is the database the project gets.
type DatabasePlan struct {
	Kind string `json:"kind"`
	Name string `json:"name"` // the database
	User string `json:"user"` // its user, with all privileges on it only
	// ServiceID is the existing server service used; "" when one is created (then it is installed
	// first when its runtime is missing too).
	ServiceID string `json:"serviceId"`
}

// Plan is POST /api/new-project/plan: exactly what the job would do, without doing anything.
type Plan struct {
	Dir      string        `json:"dir"` // <Parent>\<slug>
	Slug     string        `json:"slug"`
	Domain   string        `json:"domain"`
	Port     int           `json:"port"`
	Installs []Install     `json:"installs"`
	Database *DatabasePlan `json:"database"`
	// Steps are the job's steps in order, in words ("Installing Node.js 22.11.0").
	Steps []string `json:"steps"`
	// Creates lists what exists afterwards: services, the site, the project, the .env.
	Creates []string `json:"creates"`
	// Problems stop the job (the folder is not empty, the domain is taken, no runtime fits); the
	// button stays off. Warnings do not.
	Problems []string `json:"problems"`
	Warnings []string `json:"warnings"`
}

// InstallerAPI is what the job needs from internal/installer (*installer.Installer; tests use a
// fake that "installs" at once).
type InstallerAPI interface {
	Start(catalogID string, opts ...installer.Options) (model.Job, error)
	Get(id string) (model.Job, bool)
}

// DatabaseAPI is what the job needs from internal/dbtools (*dbtools.Manager).
type DatabaseAPI interface {
	CreateDatabase(ctx context.Context, serviceID, name string) error
	CreateUser(ctx context.Context, serviceID, user, password, database string) error
	DropDatabases(ctx context.Context, serviceID string, dbs []string) error
	DropUser(ctx context.Context, serviceID, user string) error
}

// SupervisorAPI is what the job needs from internal/supervisor (*supervisor.Supervisor).
type SupervisorAPI interface {
	Environ(svc model.Service) ([]string, error)
	PrepareService(svc model.Service) error
	ComposerCommand(svc model.Service) (bin string, args []string, err error)
	StartWithDeps(id string) error
	State(id string) string
	Remove(id string) error
	Log(id, stream, text string)
}

// SitesAPI is what the job needs from internal/sites (*sites.Manager).
type SitesAPI interface {
	EnsureServers() (bool, error)
	Regenerate() error
}

// Manager does the work. Fields may be replaced in tests before first use.
type Manager struct {
	Home      string
	St        *store.Store
	Sup       SupervisorAPI
	Installer InstallerAPI
	Runtimes  *runtimes.Manager
	Sites     SitesAPI
	DB        DatabaseAPI
	Jobs      *jobs.Registry
	// Run runs the generator programs (nil: the real runner of internal/tasks).
	Run tasks.Runner
	// Launch opens the folder in an editor (launch.Start); nil skips that step.
	Launch func(bin string, args []string, dir string, env []string) error
	// Emit publishes events (services.changed, sites.changed, projects.changed); may be nil.
	Emit func(typ string, data any)

	// Catalog lists the downloadable runtimes (nil: catalog.Load of Home).
	Catalog func() ([]model.CatalogEntry, error)
	// PortFree reports whether a TCP port can be listened on (nil: netutil.PortFree).
	PortFree func(port int) bool
	// HTTP downloads the files of download steps (nil: the installer's client, which follows the
	// download proxy setting). AllowHTTP lets a download step use http (tests with httptest).
	HTTP      *http.Client
	AllowHTTP bool
	// Rand supplies random bytes for passwords and {random} (nil: crypto/rand).
	Rand func(n int) []byte

	// Timing knobs; zero means the default.
	PollEvery    time.Duration // how often an install job or a starting database is looked at (300 ms)
	StartTimeout time.Duration // how long a database server may take to run (2 min)
	InstallWait  time.Duration // how long an install may take (30 min)
}

// New returns a Manager.
func New(home string, st *store.Store, sup *supervisor.Supervisor, inst *installer.Installer, rt *runtimes.Manager,
	sm *sites.Manager, db *dbtools.Manager, j *jobs.Registry, emit func(string, any)) *Manager {
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
	if db != nil {
		m.DB = db
	}
	return m
}

func (m *Manager) emit(typ string) {
	if m.Emit != nil {
		m.Emit(typ, struct{}{})
	}
}

func (m *Manager) poll() time.Duration {
	if m.PollEvery > 0 {
		return m.PollEvery
	}
	return 300 * time.Millisecond
}

func (m *Manager) startTimeout() time.Duration {
	if m.StartTimeout > 0 {
		return m.StartTimeout
	}
	return 2 * time.Minute
}

func (m *Manager) installWait() time.Duration {
	if m.InstallWait > 0 {
		return m.InstallWait
	}
	return 30 * time.Minute
}
