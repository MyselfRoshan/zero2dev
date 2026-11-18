// Package migrate brings a program's settings and data from a normal
// installation into Zero2Dev's portable data folder ("Bring in"), by copy or
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
// (0.0.5)", section Bring in). Wave 0 stub: the signatures are final, the
// bodies are filled in by the migrate-backend work.
package migrate

import (
	"context"
	"errors"
	"time"

	"zero2dev/internal/dbtools"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// ErrNotImplemented is returned by the Wave 0 stubs.
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

// Item is one folder, file, registry key, runtime or set of databases.
type Item struct {
	Label     string `json:"label"`
	Kind      string `json:"kind"` // dir | file | registry | runtime | databases
	From      string `json:"from"`
	To        string `json:"to"`
	SizeBytes int64  `json:"sizeBytes"`
	Exists    bool   `json:"exists"` // the source is there
}

// Source is a program found on this PC that can be brought in.
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
	Description string     `json:"description"`         // one line, e.g. "Copied VS Code settings and 42 extensions"
}

// Env says where the user-level folders are. Tests point them at temp folders.
type Env struct {
	AppData      string // %APPDATA%
	LocalAppData string // %LOCALAPPDATA%
	UserProfile  string // %USERPROFILE%
}

// Manager does the work. Fields may be replaced in tests before first use.
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
}

// New returns a Manager for the current user's folders.
func New(home, bin string, st *store.Store, j *jobs.Registry, db dbtools.ServerCopier, emit func(string, any)) *Manager {
	return &Manager{Home: home, Bin: bin, St: st, Jobs: j, DB: db, Emit: emit}
}

// Sources lists what can be brought in from this PC.
func (m *Manager) Sources(ctx context.Context) ([]Source, error) { return nil, ErrNotImplemented }

// Start starts a migrate job.
func (m *Manager) Start(req Request) (model.Job, error) { return model.Job{}, ErrNotImplemented }

// List returns the migrations, newest first.
func (m *Manager) List() ([]Migration, error) { return nil, ErrNotImplemented }

// Revert starts a migrate-revert job; how is one of the record's RevertHow.
func (m *Manager) Revert(id, how string) (model.Job, error) { return model.Job{}, ErrNotImplemented }

// ListRemoteDatabases lists the databases of another server, for the
// database source's form.
func (m *Manager) ListRemoteDatabases(ctx context.Context, src Database) ([]dbtools.Database, error) {
	return nil, ErrNotImplemented
}
