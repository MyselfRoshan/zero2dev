// Package snapshot takes and restores named copies of a project's environment
// (its services and their settings, the runtime version each one used, the
// generated php.ini files, its sites, and the data folders of its database
// services) or of a single database service, and clones a project snapshot
// into a new project.
//
// Snapshots live in <home>\snapshots\<ownerId>\<snapshotId>\ (ownerId is a
// project id or a database service id) with a manifest.json, data\<svcId>\
// and php\<svcId>\php.ini, and move with the data folder.
//
// Contract (docs/API.md, "Amendment: data, databases and environment snapshots
// (0.0.5)"). Wave 0 stub: the signatures are final, the bodies are filled in by
// the snapshot-backend work.
package snapshot

import (
	"errors"
	"time"

	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

// ErrNotImplemented is returned by the Wave 0 stubs.
var ErrNotImplemented = errors.New("not implemented yet")

// Scopes of a snapshot.
const (
	ScopeProject = "project"
	ScopeService = "service"
)

// RuntimeRef is the runtime a service ran with when the snapshot was taken.
type RuntimeRef struct {
	Kind      string `json:"kind"`
	Version   string `json:"version"`
	CatalogID string `json:"catalogId"` // "" for a registered program
	Binary    string `json:"binary"`
	// FollowedDefault: the service had no runtimeId of its own and used the
	// default of its kind.
	FollowedDefault bool `json:"followedDefault"`
}

// ServiceEntry is one service in a snapshot.
type ServiceEntry struct {
	Service model.Service `json:"service"` // as stored, status fields cleared
	Runtime *RuntimeRef   `json:"runtime"` // nil when none could be resolved
	HasData bool          `json:"hasData"` // its data folder is in the snapshot
	PHPIni  bool          `json:"phpIni"`  // its php.ini is in the snapshot
}

// Manifest is manifest.json.
type Manifest struct {
	Format    int            `json:"format"` // 1
	ID        string         `json:"id"`     // snp_xxxxxxxx
	Name      string         `json:"name"`
	Scope     string         `json:"scope"`
	OwnerID   string         `json:"ownerId"`
	CreatedAt time.Time      `json:"createdAt"`
	Version   string         `json:"version"` // Zero2Dev version that took it
	SizeBytes int64          `json:"sizeBytes"`
	Project   *model.Project `json:"project"` // nil for a service snapshot
	Sites     []model.Site   `json:"sites"`
	Services  []ServiceEntry `json:"services"`
	Warnings  []string       `json:"warnings"` // e.g. a database with custom arguments was not copied
}

// Missing is a runtime a snapshot needs that is not installed now.
type Missing struct {
	Kind        string   `json:"kind"`
	Version     string   `json:"version"`
	CatalogID   string   `json:"catalogId"`
	Installable bool     `json:"installable"` // the catalog has it
	Services    []string `json:"services"`    // names of the services that need it
}

// Detail is GET /api/snapshots/{id}: the manifest plus what restoring needs.
type Detail struct {
	Manifest
	Missing []Missing `json:"missing"`
}

// Sites is what snapshot needs from internal/sites.
type Sites interface {
	Regenerate() error
}

// Manager does the work. Fields may be replaced in tests before first use.
type Manager struct {
	Home    string
	Version string
	St      *store.Store
	Sup     *supervisor.Supervisor
	Jobs    *jobs.Registry
	Sites   Sites
	Emit    func(typ string, data any) // publishes snapshots.changed etc.; may be nil
	// Catalog lists the installable runtimes (for Missing.Installable).
	Catalog func() ([]model.CatalogEntry, error)
}

// New returns a Manager.
func New(home, version string, st *store.Store, sup *supervisor.Supervisor, j *jobs.Registry, s Sites, emit func(string, any), cat func() ([]model.CatalogEntry, error)) *Manager {
	return &Manager{Home: home, Version: version, St: st, Sup: sup, Jobs: j, Sites: s, Emit: emit, Catalog: cat}
}

// TakeOptions are the switches of Take.
type TakeOptions struct {
	Name string
	// Restart stops the running services first and starts them again
	// afterwards; without it a running service is a 409.
	Restart bool
}

// List returns the snapshots of an owner (project or service id), newest first.
func (m *Manager) List(ownerID string) ([]Manifest, error) { return nil, ErrNotImplemented }

// Get returns one snapshot with the runtimes it needs that are missing.
func (m *Manager) Get(id string) (Detail, error) { return Detail{}, ErrNotImplemented }

// Take starts a snapshot job for a project (scope project) or a database
// service (scope service).
func (m *Manager) Take(scope, ownerID string, opt TakeOptions) (model.Job, error) {
	return model.Job{}, ErrNotImplemented
}

// Restore starts a snapshot-restore job. It is a 409 while a needed runtime is
// missing, or while a service runs and restart is false.
func (m *Manager) Restore(id string, restart bool) (model.Job, error) {
	return model.Job{}, ErrNotImplemented
}

// CloneOptions are the switches of Clone.
type CloneOptions struct {
	Name         string // the new project's name
	DomainSuffix string // added to the first label of each site domain; default "-copy"
}

// Clone starts a snapshot-clone job that creates a new project from a project
// snapshot (new ids, ports and domains); the job's ProjectID is the new project.
func (m *Manager) Clone(id string, opt CloneOptions) (model.Job, error) {
	return model.Job{}, ErrNotImplemented
}

// Delete removes a snapshot.
func (m *Manager) Delete(id string) error { return ErrNotImplemented }
