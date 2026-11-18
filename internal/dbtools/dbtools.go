// Package dbtools works on the data of database services: list, create
// databases and users, import and export dumps, set the administrator
// password, reset the data folder, and copy databases from another server.
// It runs the database's own client programs (found next to the server
// binary) through an injectable Runner, so tests never need real database
// binaries. Passwords travel in the environment (MYSQL_PWD, PGPASSWORD,
// REDISCLI_AUTH), never on a command line.
//
// Contract (docs/API.md, "Amendment: data, databases and environment snapshots
// (0.0.5)"). Wave 0 stub: the signatures are final, the bodies are filled in by
// the db-backend work.
package dbtools

import (
	"context"
	"errors"
	"io"
	"time"

	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// ErrNotImplemented is returned by the Wave 0 stubs.
var ErrNotImplemented = errors.New("not implemented yet")

// Cmd is one run of a client program.
type Cmd struct {
	Bin     string    // absolute path of the program
	Args    []string  // never a shell string
	Env     []string  // extra variables (KEY=VALUE), added to the daemon's environment
	Dir     string    // working folder; "" is the program's folder
	Stdin   io.Reader // nil: no input
	Stdout  io.Writer // nil: output goes to the service log
	Timeout time.Duration
}

// Runner runs c and waits for it. Its error says what failed, including the
// last lines the program wrote to stderr.
type Runner func(ctx context.Context, c Cmd) error

// Supervisor is what dbtools needs from internal/supervisor (*Supervisor
// implements it; tests use a fake).
type Supervisor interface {
	State(id string) string
	RuntimeFor(svc model.Service) (*model.Runtime, string, error)
	DataDir(kind, serviceID string) string
	Reserve(ids []string, why string) (release func(), err error)
	Log(id, stream, text string)
}

// Target is a database server to talk to: a Zero2Dev service (Host
// "127.0.0.1") or another server (migrations). ClientDir is the folder of the
// client programs (mysql, mysqldump, psql, pg_dump ...).
type Target struct {
	Kind      string // mariadb | mysql | postgres | redis
	Host      string
	Port      int
	User      string
	Password  string
	ClientDir string
}

// Database is one database of a server.
type Database struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"` // 0 when the server does not say
}

// Info is GET /api/services/{id}/database.
type Info struct {
	ServiceID   string     `json:"serviceId"`
	Kind        string     `json:"kind"`
	Running     bool       `json:"running"`
	User        string     `json:"user"`        // the administrator: root or postgres ("" for redis)
	HasPassword bool       `json:"hasPassword"` // AdminPassword is set
	Databases   []Database `json:"databases"`   // [] while stopped or for redis
	Users       []string   `json:"users"`       // [] while stopped or for redis
	Supports    Supports   `json:"supports"`
	DataDir     string     `json:"dataDir"`
}

// Supports says which operations the kind has.
type Supports struct {
	Databases bool `json:"databases"` // create databases and users (not redis)
	Dumps     bool `json:"dumps"`     // import and export (not redis)
	Password  bool `json:"password"`
	Reset     bool `json:"reset"`
}

// ServerCopier copies databases from another server into a Zero2Dev database
// service (internal/migrate uses it; *Manager implements it).
type ServerCopier interface {
	// ListRemote lists the databases of src (system databases left out).
	ListRemote(ctx context.Context, src Target) ([]Database, error)
	// CopyFromServer dumps each of dbs from src and imports it into the
	// service dstServiceID (which must be running), creating the database.
	// progress (may be nil) gets a percent and a step.
	CopyFromServer(ctx context.Context, src Target, dbs []string, dstServiceID string, progress func(percent int, step string)) error
	// DropDatabases removes dbs from the service (the revert of a copy).
	DropDatabases(ctx context.Context, serviceID string, dbs []string) error
}

// Manager does the work. Fields may be replaced in tests before first use.
type Manager struct {
	St   *store.Store
	Sup  Supervisor
	Jobs *jobs.Registry
	Run  Runner
	Home string
}

// New returns a Manager that runs programs with the default Runner.
func New(st *store.Store, sup Supervisor, j *jobs.Registry, home string) *Manager {
	return &Manager{St: st, Sup: sup, Jobs: j, Run: nil, Home: home}
}

// Info describes the service's server and, while it runs, its databases and users.
func (m *Manager) Info(ctx context.Context, serviceID string) (Info, error) {
	return Info{}, ErrNotImplemented
}

// CreateDatabase creates a database (the server must run).
func (m *Manager) CreateDatabase(ctx context.Context, serviceID, name string) error {
	return ErrNotImplemented
}

// CreateUser creates a user with a password and, when database is not "",
// gives it every privilege on that database.
func (m *Manager) CreateUser(ctx context.Context, serviceID, user, password, database string) error {
	return ErrNotImplemented
}

// Import starts a db-import job: filePath (.sql, or gzip-compressed, detected
// by its first bytes) is read into database, which is created first when
// create is set.
func (m *Manager) Import(serviceID, database, filePath string, create bool) (model.Job, error) {
	return model.Job{}, ErrNotImplemented
}

// Export starts a db-export job writing database to filePath (gzip when it
// ends in .gz); an existing file is refused unless overwrite.
func (m *Manager) Export(serviceID, database, filePath string, overwrite bool) (model.Job, error) {
	return model.Job{}, ErrNotImplemented
}

// SetPassword changes the administrator password ("" removes it) and stores it
// in the service (the server must run).
func (m *Manager) SetPassword(ctx context.Context, serviceID, password string) (model.Service, error) {
	return model.Service{}, ErrNotImplemented
}

// Reset deletes the service's data folder (the service must be stopped) and
// clears its password; the next start sets the database up again.
func (m *Manager) Reset(serviceID string) (model.Service, error) {
	return model.Service{}, ErrNotImplemented
}

// ListRemote: see ServerCopier.
func (m *Manager) ListRemote(ctx context.Context, src Target) ([]Database, error) {
	return nil, ErrNotImplemented
}

// CopyFromServer: see ServerCopier.
func (m *Manager) CopyFromServer(ctx context.Context, src Target, dbs []string, dstServiceID string, progress func(int, string)) error {
	return ErrNotImplemented
}

// DropDatabases: see ServerCopier.
func (m *Manager) DropDatabases(ctx context.Context, serviceID string, dbs []string) error {
	return ErrNotImplemented
}

var _ ServerCopier = (*Manager)(nil)
