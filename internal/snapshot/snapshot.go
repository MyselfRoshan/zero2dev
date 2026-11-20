// Package snapshot takes and restores named copies of a project's environment
// (its services and their settings, the runtime version each one used, the
// generated php.ini files, its sites, and the data folders of its database
// services) or of a single database service, and clones a project snapshot
// into a new project.
//
// Snapshots live in <home>\snapshots\<ownerId>\<snapshotId>\ (ownerId is a
// project id or a database service id) with a manifest.json, data\<svcId>\
// and php\<svcId>\php.ini, and move with the data folder. A snapshot being
// taken is written to <snapshotId>.partial first and renamed when complete,
// so a folder without that suffix and with a manifest is always whole.
//
// Contract: docs/API.md, "Amendment: data, databases and environment
// snapshots (0.0.5)", section "Snapshots".
package snapshot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/fsutil"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

// ErrNotImplemented was returned by the Wave 0 stubs. It is kept so the API
// layer's "not built yet" mapping still compiles; nothing returns it now.
var ErrNotImplemented = errors.New("not implemented yet")

// Scopes of a snapshot.
const (
	ScopeProject = "project"
	ScopeService = "service"
)

// Format is the manifest format this package writes and reads.
const Format = 1

// Job steps (job.progress "step").
const (
	StepStopping  = "Stopping"  // take / restore with restart: stopping the running services
	StepCopying   = "Copying"   // take: copying into the snapshot; clone: copying the data
	StepRestoring = "Restoring" // restore: putting data folders and php.ini files back
	StepCreating  = "Creating"  // clone: adding the new project, services and sites
	StepStarting  = "Starting"  // take / restore with restart: starting what was running
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

	// copyTree copies a folder with progress (fsutil.CopyTreeProgress; tests
	// replace it to make a copy fail).
	copyTree func(src, dst string, fn func(done, total int64)) error
	// portFree reports whether a TCP port can be listened on (tests replace it).
	portFree func(port int) bool

	mu    sync.Mutex
	inUse map[string]string // snapshot id -> what a running job does with it
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

// CloneOptions are the switches of Clone.
type CloneOptions struct {
	Name         string // the new project's name
	DomainSuffix string // added to the first label of each site domain; default "-copy"
}

// idRE is what an owner or snapshot id may look like: it becomes a folder
// name, so no dots, slashes or colons.
var idRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (m *Manager) root() string { return filepath.Join(m.Home, "snapshots") }

func (m *Manager) emit(typ string, data any) {
	if m.Emit != nil {
		m.Emit(typ, data)
	}
}

func (m *Manager) copy(src, dst string, fn func(done, total int64)) error {
	if m.copyTree != nil {
		return m.copyTree(src, dst, fn)
	}
	return fsutil.CopyTreeProgress(src, dst, fn)
}

// claim marks a snapshot as used by a job (a restore or a clone), so Delete
// refuses it; the returned func releases it.
func (m *Manager) claim(id, what string) func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inUse == nil {
		m.inUse = map[string]string{}
	}
	m.inUse[id] = what
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.inUse, id)
	}
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := len([]rune(name)); n < 1 || n > 64 || strings.ContainsAny(name, "\r\n\x00") {
		return name, apperr.BadRequest("name must be 1-64 characters")
	}
	return name, nil
}

// readManifest reads dir/manifest.json and fills in the lists JSON may have
// left nil.
func readManifest(dir string) (Manifest, error) {
	var mf Manifest
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return mf, err
	}
	if err := json.Unmarshal(b, &mf); err != nil {
		return mf, err
	}
	if mf.Format != Format {
		return mf, apperr.Conflict("snapshot %s has format %d, which this version cannot read", mf.ID, mf.Format)
	}
	normalize(&mf)
	return mf, nil
}

func normalize(mf *Manifest) {
	if mf.Sites == nil {
		mf.Sites = []model.Site{}
	}
	if mf.Services == nil {
		mf.Services = []ServiceEntry{}
	}
	if mf.Warnings == nil {
		mf.Warnings = []string{}
	}
}

func writeManifest(dir string, mf Manifest) error {
	normalize(&mf)
	b, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(dir, "manifest.json"), b, 0o600)
}

// List returns the snapshots of an owner (project or service id), newest first.
// An owner that no longer exists still has its snapshots; an owner without any
// has an empty list.
func (m *Manager) List(ownerID string) ([]Manifest, error) {
	out := []Manifest{}
	if !idRE.MatchString(ownerID) {
		return out, nil
	}
	ents, err := os.ReadDir(filepath.Join(m.root(), ownerID))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range ents {
		if !e.IsDir() || !idRE.MatchString(e.Name()) { // skips <id>.partial
			continue
		}
		mf, err := readManifest(filepath.Join(m.root(), ownerID, e.Name()))
		if err != nil || mf.ID != e.Name() {
			continue // not a snapshot (or one this version cannot read)
		}
		out = append(out, mf)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// find returns the folder and manifest of a snapshot, or a 404.
func (m *Manager) find(id string) (string, Manifest, error) {
	if idRE.MatchString(id) {
		owners, _ := os.ReadDir(m.root())
		for _, o := range owners {
			if !o.IsDir() {
				continue
			}
			dir := filepath.Join(m.root(), o.Name(), id)
			if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
				continue
			}
			mf, err := readManifest(dir)
			if err != nil {
				return "", Manifest{}, err
			}
			return dir, mf, nil
		}
	}
	return "", Manifest{}, apperr.NotFound("snapshot %s not found", id)
}

// Get returns one snapshot with the runtimes it needs that are missing.
func (m *Manager) Get(id string) (Detail, error) {
	_, mf, err := m.find(id)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Manifest: mf, Missing: m.missing(mf, m.St.Snapshot())}, nil
}

// Delete removes a snapshot. It is a 409 while a restore or a clone uses it.
func (m *Manager) Delete(id string) error {
	dir, mf, err := m.find(id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	what, busy := m.inUse[id]
	m.mu.Unlock()
	if busy {
		return apperr.Conflict("snapshot %q is in use (%s); try again when it is done", mf.Name, what)
	}
	if err := os.RemoveAll(dir); err != nil {
		return apperr.Internal("could not delete snapshot %q: %v", mf.Name, err)
	}
	_ = os.Remove(filepath.Dir(dir)) // the owner's folder, when it is empty now
	m.emit("snapshots.changed", map[string]string{"ownerId": mf.OwnerID})
	return nil
}

// cleared is a stored service without its status fields, with its own copies
// of the slices and maps.
func cleared(sv model.Service) model.Service {
	sv.State, sv.Pid, sv.StartedAt, sv.LastError, sv.Restarts = "", 0, nil, "", 0
	sv.Args = append([]string{}, sv.Args...)
	sv.DependsOn = append([]string{}, sv.DependsOn...)
	sv.Env = copyMap(sv.Env)
	sv.Settings = copyMap(sv.Settings)
	return sv
}

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// active reports whether a supervisor state is starting, running or stopping.
func active(state string) bool {
	return state == model.StateStarting || state == model.StateRunning || state == model.StateStopping
}

// joinNames lists names as "a", "a and b" or "a, b and c".
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// runningError is the 409 for services that run while restart is false.
// details.running lists their ids, comma-separated.
func runningError(names, ids []string, doing string) error {
	verb, pron := "are", "them"
	if len(names) == 1 {
		verb, pron = "is", "it"
	}
	return apperr.Conflict("%s %s running; stop %s first, or %s with restart", joinNames(names), verb, pron, doing).
		With("running", strings.Join(ids, ","))
}

// activeMembers returns the names and ids of the services in ids that are
// starting, running or stopping, and the first Busy error among them.
func (m *Manager) activeMembers(ids []string, d store.Data) (names, running []string, busy error) {
	for _, id := range ids {
		if busy == nil {
			busy = m.Sup.Busy(id)
		}
		if active(m.Sup.State(id)) {
			running = append(running, id)
			names = append(names, serviceName(d, id))
		}
	}
	return names, running, busy
}

func serviceName(d store.Data, id string) string {
	for _, sv := range d.Services {
		if sv.ID == id {
			return sv.Name
		}
	}
	return id
}

// stopAll stops the services in ids: a project in its stop order, a single
// service with StopWait. It returns an error naming the ones that did not stop.
func (m *Manager) stopAll(project bool, ids []string) error {
	if project {
		rep := m.Sup.StopInOrder(ids)
		if len(rep.Failed) > 0 {
			var names []string
			for _, f := range rep.Failed {
				names = append(names, f.Name+" ("+f.Error+")")
			}
			return apperr.Conflict("could not stop %s", joinNames(names))
		}
		return nil
	}
	for _, id := range ids {
		if err := m.Sup.StopWait(id); err != nil {
			return apperr.Conflict("could not stop %s: %v", serviceName(m.St.Snapshot(), id), err)
		}
	}
	return nil
}

// startAgain starts the services in ids again (in the project's start order)
// and logs a line on each that did not come up. It never fails the job: the
// snapshot or restore itself is done.
func (m *Manager) startAgain(ids []string) {
	if len(ids) == 0 {
		return
	}
	rep := m.Sup.StartInOrder(ids)
	for _, f := range rep.Failed {
		if f.ID != "" {
			m.Sup.Log(f.ID, "system", "snapshot: could not start again: "+f.Error)
		}
	}
}
