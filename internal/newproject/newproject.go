// Package newproject makes a project from a framework of internal/frameworks in one job: it
// installs what is missing (the runtime version the framework needs, Composer, a database server),
// runs the framework's own generator, creates the database and its user, writes the .env, and
// creates the project's services, its site and the project itself, then opens the folder in the
// installed editor. If a step fails it removes only what it created and keeps the generated
// folder (never deleting files it did not write).
//
// Contract (docs/API.md, "Amendment: languages, tasks and new projects (0.0.6)"). Wave 0 stub:
// the signatures are final, the bodies are filled in by the new-project work.
package newproject

import (
	"errors"

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

// ErrNotImplemented is returned by the Wave 0 stubs (the API answers 501).
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

// Manager does the work. Fields may be replaced in tests before first use.
type Manager struct {
	Home      string
	St        *store.Store
	Sup       *supervisor.Supervisor
	Installer *installer.Installer
	Runtimes  *runtimes.Manager
	Sites     *sites.Manager
	DB        *dbtools.Manager
	Jobs      *jobs.Registry
	// Run runs the generator programs (nil: the real runner of internal/tasks).
	Run tasks.Runner
	// Launch opens the folder in an editor (launch.Start); nil skips that step.
	Launch func(bin string, args []string, dir string, env []string) error
	// Emit publishes events (services.changed, sites.changed, projects.changed); may be nil.
	Emit func(typ string, data any)
}

// New returns a Manager.
func New(home string, st *store.Store, sup *supervisor.Supervisor, inst *installer.Installer, rt *runtimes.Manager,
	sm *sites.Manager, db *dbtools.Manager, j *jobs.Registry, emit func(string, any)) *Manager {
	return &Manager{Home: home, St: st, Sup: sup, Installer: inst, Runtimes: rt, Sites: sm, DB: db, Jobs: j, Emit: emit}
}

// Plan works out what Start would do. It changes nothing.
func (m *Manager) Plan(req Request) (Plan, error) { return Plan{}, ErrNotImplemented }

// Start runs the job (type "new-project"; job.done carries projectId and path). It answers
// the same problems as Plan with a 409, so a stale plan cannot start.
func (m *Manager) Start(req Request) (model.Job, error) { return model.Job{}, ErrNotImplemented }
