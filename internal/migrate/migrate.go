// Package migrate brings a program's settings and data from a normal
// installation into Zero2Dev's portable data folder ("Import"), by copy or
// by move, and reverts it: VS Code, VSCodium, Notepad++, HeidiSQL, DBeaver,
// runtimes found on PATH (adopted into bin\adopted\), the runtimes' user-level
// caches and global packages (npm, Go, Composer), and databases of another
// server (through dbtools.ServerCopier).
//
// Every migration is recorded in <home>\migrations\<migrationId>\migration.json
// with backups (backup-target\, backup-original\), so a revert can put
// everything back.
//
// Contract (docs/API.md, "Amendment: data, databases and environment snapshots
// (0.0.5)", section Import).
package migrate

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/dbtools"
	"zero2dev/internal/jobs"
	"zero2dev/internal/store"
)

// ErrNotImplemented was returned by the Wave 0 stubs; the API still maps it
// to 501 (a Manager without database tools answers it for the database source).
var ErrNotImplemented = errors.New("not implemented yet")

// Programs (Source.Program).
const (
	ProgVSCode    = "vscode"
	ProgVSCodium  = "vscodium"
	ProgNotepadPP = "notepadpp"
	ProgHeidiSQL  = "heidisql"
	ProgDBeaver   = "dbeaver"
	ProgRuntime   = "runtime"    // a registered runtime (found on PATH), adopted into bin\adopted\
	ProgNpmGlobal = "npm-global" // %APPDATA%\npm
	ProgNpmCache  = "npm-cache"  // %LOCALAPPDATA%\npm-cache
	ProgGoCache   = "go-cache"   // %USERPROFILE%\go\pkg\mod and %LOCALAPPDATA%\go-build
	ProgComposer  = "composer"   // %APPDATA%\Composer
	ProgDatabase  = "database"   // databases of another server
)

// Modes and revert choices.
const (
	ModeCopy = "copy"
	ModeMove = "move"

	RevertDiscard  = "discard"   // copy: remove the portable copy, bring back what the target held
	RevertCopyBack = "copy-back" // copy: copy the portable data over the originals (saved first)
	RevertMoveBack = "move-back" // move: put everything back where it was
)

// Migration states.
const (
	StateRunning  = "running" // on disk only while the job runs; listed as failed after a crash
	StateDone     = "done"
	StateReverted = "reverted"
	StateFailed   = "failed"
)

// Item is one folder, file, registry key, runtime or set of databases.
type Item struct {
	Label     string `json:"label"`
	Kind      string `json:"kind"` // dir | file | registry | runtime | databases
	From      string `json:"from"`
	To        string `json:"to"`
	SizeBytes int64  `json:"sizeBytes"`
	Exists    bool   `json:"exists"` // the source is there
}

// Item kinds.
const (
	KindDir       = "dir"
	KindFile      = "file"
	KindRegistry  = "registry"
	KindRuntime   = "runtime"
	KindDatabases = "databases"
)

// Source is a program found on this PC that can be imported.
type Source struct {
	ID        string   `json:"id"` // "vscode", "runtime:rt_1a2b3c4d", "database"
	Program   string   `json:"program"`
	Label     string   `json:"label"`
	Found     bool     `json:"found"`
	Items     []Item   `json:"items"`
	Modes     []string `json:"modes"`               // ["copy","move"] or ["copy"]
	CopyOnly  string   `json:"copyOnlyReason"`      // why Move is not offered, "" when it is
	Blockers  []string `json:"blockers"`            // what must happen first; [] when it can run
	TargetHas bool     `json:"targetHasData"`       // the portable place already holds data (it is backed up first)
	RuntimeID string   `json:"runtimeId,omitempty"` // ProgRuntime: the registered runtime
	Note      string   `json:"note"`                // e.g. "Settings Sync logins are not moved"
	// TargetFreeBytes is the free space of the drive the data goes to (0: unknown).
	TargetFreeBytes int64 `json:"targetFreeBytes"`
}

// Database is the "database" source's input: another server and what to copy.
type Database struct {
	Kind            string   `json:"kind"` // mariadb | mysql | postgres
	Host            string   `json:"host"`
	Port            int      `json:"port"`
	User            string   `json:"user"`
	Password        string   `json:"password"`
	Databases       []string `json:"databases"`
	TargetServiceID string   `json:"targetServiceId"`
}

// Request is the body of POST /api/migrations.
type Request struct {
	Source   string    `json:"source"` // Source.ID
	Mode     string    `json:"mode"`
	Items    []string  `json:"items"` // labels to include; empty means all
	Database *Database `json:"database"`
}

// Migration is the record of one migration.
type Migration struct {
	ID          string     `json:"id"` // mig_xxxxxxxx
	Source      string     `json:"source"`
	Program     string     `json:"program"`
	Label       string     `json:"label"`
	Mode        string     `json:"mode"`
	CreatedAt   time.Time  `json:"createdAt"`
	State       string     `json:"state"` // done | reverted | failed
	RevertedAt  *time.Time `json:"revertedAt"`
	Items       []Item     `json:"items"`
	RevertHow   []string   `json:"revertOptions"` // what Revert accepts
	Databases   []string   `json:"databases,omitempty"`
	ServiceID   string     `json:"serviceId,omitempty"` // database: the Zero2Dev service
	RuntimeID   string     `json:"runtimeId,omitempty"` // runtime: the adopted runtime
	Description string     `json:"description"`         // one line, e.g. "Copied VS Code: Settings, Extensions (414 MB)"
	FinishedAt  *time.Time `json:"finishedAt"`          // when its last job ended
	// Set by List and the manage calls, not stored:
	BackupBytes    int64 `json:"backupBytes"`    // what backup-target\ and backup-original\ hold
	CanCleanup     bool  `json:"canCleanup"`     // a done copy whose originals can be removed
	OriginalsBytes int64 `json:"originalsBytes"` // what removing the originals frees
}

// Env says where the user-level folders are. Tests point them at temp folders.
type Env struct {
	AppData      string // %APPDATA%
	LocalAppData string // %LOCALAPPDATA%
	UserProfile  string // %USERPROFILE%
}

// Manager does the work. Fields may be replaced in tests before first use.
//
// When Env is left empty, the Manager works on the current user's folders: Env
// comes from %APPDATA%, %LOCALAPPDATA% and %USERPROFILE%, Reg is the real
// HKCU and Running the real process list (Windows only). When Env is set (as
// tests do), a nil Reg is an empty registry and a nil Running sees nothing
// running, so a test can never reach the real user's data.
type Manager struct {
	Home string // the data folder
	Bin  string // the bin folder (adopted runtimes go to <Bin>\adopted\)
	St   *store.Store
	Jobs *jobs.Registry
	DB   dbtools.ServerCopier
	Env  Env
	Emit func(typ string, data any) // migrations.changed; may be nil
	// Running reports whether a program with this image name runs (Code.exe ...).
	Running func(image string) bool
	// Reg is HKEY_CURRENT_USER (HeidiSQL keeps its settings there).
	Reg Registry
	// SameVolume reports whether two paths are on one volume, where a move is
	// a rename. nil compares the drive letters.
	SameVolume func(a, b string) bool
	// Now is the clock (nil: time.Now).
	Now func() time.Time
	// Free reports the free bytes of the drive holding a path (nil: the real
	// drive; 0 = unknown).
	Free func(path string) int64

	mu     sync.Mutex
	active map[string]bool // migration ids whose job runs
}

// New returns a Manager for the current user's folders.
func New(home, bin string, st *store.Store, j *jobs.Registry, db dbtools.ServerCopier, emit func(string, any)) *Manager {
	return &Manager{Home: home, Bin: bin, St: st, Jobs: j, DB: db, Emit: emit}
}

// production reports whether the Manager works on the real user's folders.
func (m *Manager) production() bool { return m.Env == (Env{}) }

func (m *Manager) env() Env {
	if m.production() {
		return Env{AppData: os.Getenv("APPDATA"), LocalAppData: os.Getenv("LOCALAPPDATA"), UserProfile: os.Getenv("USERPROFILE")}
	}
	return m.Env
}

func (m *Manager) reg() Registry {
	if m.Reg != nil {
		return m.Reg
	}
	if m.production() {
		return systemRegistry()
	}
	return emptyRegistry{}
}

func (m *Manager) running(image string) bool {
	if image == "" {
		return false
	}
	if m.Running != nil {
		return m.Running(image)
	}
	if m.production() {
		return processRunning(image)
	}
	return false
}

func (m *Manager) sameVolume(a, b string) bool {
	if m.SameVolume != nil {
		return m.SameVolume(a, b)
	}
	return defaultSameVolume(a, b)
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m *Manager) emit(typ string) {
	if m.Emit != nil {
		m.Emit(typ, struct{}{})
	}
}

func (m *Manager) setActive(id string, on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		m.active = map[string]bool{}
	}
	if on {
		m.active[id] = true
	} else {
		delete(m.active, id)
	}
}

func (m *Manager) isActive(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[id]
}

// Sources lists what can be imported from this PC.
func (m *Manager) Sources(ctx context.Context) ([]Source, error) {
	plans := m.plans()
	out := make([]Source, 0, len(plans))
	for _, p := range plans {
		out = append(out, p.src)
	}
	return out, nil
}

// List returns the migrations, newest first. One whose job still runs is
// left out; one whose job never finished (the daemon stopped) is "failed".
func (m *Manager) List() ([]Migration, error) {
	recs, err := m.loadAll()
	if err != nil {
		return nil, err
	}
	out := []Migration{}
	for _, r := range recs {
		if m.isActive(r.ID) {
			continue
		}
		out = append(out, m.view(r))
	}
	return out, nil
}

// ListRemoteDatabases lists the databases of another server, for the
// database source's form.
func (m *Manager) ListRemoteDatabases(ctx context.Context, src Database) ([]dbtools.Database, error) {
	if m.DB == nil {
		return nil, ErrNotImplemented
	}
	if err := validateServer(src); err != nil {
		return nil, err
	}
	dir, err := m.clientDir(src.Kind, "")
	if err != nil {
		return nil, err
	}
	list, err := m.DB.ListRemote(ctx, remoteTarget(src, dir))
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) || errors.Is(err, dbtools.ErrNotImplemented) {
			return nil, err
		}
		return nil, apperr.Conflict("cannot list the databases of %s:%d: %v", src.Host, src.Port, err)
	}
	return list, nil
}

func remoteTarget(src Database, clientDir string) dbtools.Target {
	return dbtools.Target{Kind: src.Kind, Host: strings.TrimSpace(src.Host), Port: src.Port, User: src.User, Password: src.Password, ClientDir: clientDir}
}

// Settings.Portable flags a migration switches.
const (
	portNpmGlobal = "npmGlobal"
	portComposer  = "composer"
	portGoCache   = "goCache"
)

// portableFlag switches one Settings.Portable flag.
func (m *Manager) portableFlag(name string, on bool) error {
	if name == "" || m.St == nil {
		return nil
	}
	return m.St.Update(func(d *store.Data) error {
		switch name {
		case portNpmGlobal:
			d.Settings.Portable.NpmGlobal = on
		case portComposer:
			d.Settings.Portable.Composer = on
		case portGoCache:
			d.Settings.Portable.GoCache = on
		}
		return nil
	})
}

// installed reports whether Zero2Dev installed a runtime of kind.
func installed(d store.Data, kind string) bool {
	for _, r := range d.Runtimes {
		if r.Kind == kind && r.Source == "installed" {
			return true
		}
	}
	return false
}
