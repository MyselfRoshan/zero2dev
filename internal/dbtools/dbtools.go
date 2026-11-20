// Package dbtools works on the data of database services: list, create
// databases and users, import and export dumps, set the administrator
// password, reset the data folder, and copy databases from another server.
// It runs the database's own client programs (found next to the server
// binary) through an injectable Runner, so tests never need real database
// binaries. Passwords travel in the environment (MYSQL_PWD, PGPASSWORD,
// REDISCLI_AUTH), never on a command line.
//
// Contract: docs/API.md, "Amendment: data, databases and environment
// snapshots (0.0.5)", section "Databases".
package dbtools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/jobs"
	"zero2dev/internal/launch"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// ErrNotImplemented: the API answers 501 when no Manager is configured.
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
	Run  Runner // nil: DefaultRunner
	Home string

	mu     sync.Mutex
	active map[string]string // service id -> id of the import/export job working on it
}

// New returns a Manager that runs programs with the default Runner.
func New(st *store.Store, sup Supervisor, j *jobs.Registry, home string) *Manager {
	return &Manager{St: st, Sup: sup, Jobs: j, Run: DefaultRunner, Home: home}
}

// ---------- helpers ----------

func (m *Manager) log(id, text string) {
	if id != "" && m.Sup != nil {
		m.Sup.Log(id, "system", text)
	}
}

// exec runs c, first logging what runs (to logID's service log; "" logs
// nothing) as "<what>: <program> <args>". Output with no Stdout of its own goes
// to the log. secrets (passwords in stdin) are replaced by ******** in the
// error, which is logged too.
func (m *Manager) exec(ctx context.Context, logID, what string, c Cmd, secrets ...string) error {
	run := m.Run
	if run == nil {
		run = DefaultRunner
	}
	if logID != "" {
		m.log(logID, what+": "+progName(c.Bin)+" "+strings.Join(c.Args, " "))
	}
	var ll *lineLogger
	if c.Stdout == nil {
		if logID == "" {
			c.Stdout = io.Discard
		} else {
			ll = &lineLogger{emit: func(t string) { m.log(logID, "  "+t) }}
			c.Stdout = ll
		}
	}
	err := run(ctx, c)
	if ll != nil {
		ll.flush()
	}
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, s := range secrets {
		if s != "" {
			msg = strings.ReplaceAll(msg, s, "********")
		}
	}
	if logID != "" {
		m.log(logID, what+" failed: "+msg)
	}
	var ae *apperr.Error
	if errors.As(err, &ae) && msg == err.Error() {
		return err
	}
	return errors.New(msg)
}

// service returns the stored service (404 when unknown).
func (m *Manager) service(id string) (model.Service, error) {
	for _, s := range m.St.Snapshot().Services {
		if s.ID == id {
			return s, nil
		}
	}
	return model.Service{}, apperr.NotFound("service %s not found", id)
}

// dbService returns a database service (400 for any other kind).
func (m *Manager) dbService(id string) (model.Service, error) {
	svc, err := m.service(id)
	if err != nil {
		return svc, err
	}
	if family(svc.Kind) == "" {
		return svc, apperr.BadRequest("%s is not a database", svc.Name)
	}
	return svc, nil
}

// sqlService is dbService refusing Redis (no databases, users or dumps).
func (m *Manager) sqlService(id, what string) (model.Service, error) {
	svc, err := m.dbService(id)
	if err == nil && svc.Kind == "redis" {
		err = apperr.BadRequest("%s is Redis, which has no %s", svc.Name, what)
	}
	return svc, err
}

func (m *Manager) running(svc model.Service) bool {
	return m.Sup.State(svc.ID) == model.StateRunning
}

func (m *Manager) needRunning(svc model.Service) error {
	if !m.running(svc) {
		return apperr.Conflict("start %s first", svc.Name)
	}
	return nil
}

// local is the Target of a Zero2Dev database service: 127.0.0.1, its port,
// its administrator and password, the client programs next to its server.
func (m *Manager) local(svc model.Service) (Target, error) {
	if svc.Port <= 0 {
		return Target{}, apperr.Conflict("%s has no port", svc.Name)
	}
	bin := strings.TrimSpace(svc.Command)
	if bin == "" || !filepath.IsAbs(bin) {
		_, b, err := m.Sup.RuntimeFor(svc)
		if err != nil {
			return Target{}, err
		}
		bin = b
	}
	t := Target{Kind: svc.Kind, Host: "127.0.0.1", Port: svc.Port, User: launch.DBUser(svc.Kind), Password: svc.AdminPassword}
	if bin != "" {
		t.ClientDir = filepath.Dir(bin)
	}
	return t, nil
}

func bg() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), queryTimeout)
}

// hold marks the service as worked on by job jobID (Reset refuses it).
func (m *Manager) hold(serviceID, jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		m.active = map[string]string{}
	}
	m.active[serviceID] = jobID
}

func (m *Manager) unhold(serviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, serviceID)
}

func (m *Manager) activeJob(serviceID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active[serviceID]
}

// checkAbs: an absolute path (400 otherwise), cleaned.
func checkAbs(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || !filepath.IsAbs(p) {
		return "", apperr.BadRequest("filePath must be an absolute path on this PC")
	}
	return filepath.Clean(p), nil
}

func (m *Manager) setAdminPassword(id, pw string) error {
	return m.St.Update(func(d *store.Data) error {
		for i := range d.Services {
			if d.Services[i].ID == id {
				d.Services[i].AdminPassword = pw
			}
		}
		return nil
	})
}

// ---------- operations ----------

// Info describes the service's server and, while it runs, its databases and users.
func (m *Manager) Info(ctx context.Context, serviceID string) (Info, error) {
	svc, err := m.dbService(serviceID)
	if err != nil {
		return Info{}, err
	}
	sqlKind := svc.Kind != "redis"
	info := Info{
		ServiceID: svc.ID, Kind: svc.Kind, Running: m.running(svc), User: launch.DBUser(svc.Kind),
		HasPassword: svc.AdminPassword != "", Databases: []Database{}, Users: []string{},
		Supports: Supports{Databases: sqlKind, Dumps: sqlKind, Password: true, Reset: !svc.CustomArgs},
	}
	if !svc.CustomArgs {
		info.DataDir = m.Sup.DataDir(svc.Kind, svc.ID)
	}
	if !info.Running || !sqlKind {
		return info, nil
	}
	t, err := m.local(svc)
	if err != nil {
		return info, err
	}
	if info.Databases, err = m.listDatabases(ctx, t); err != nil {
		return info, err
	}
	if info.Users, err = m.listUsers(ctx, t); err != nil {
		return info, err
	}
	return info, nil
}

// CreateDatabase creates a database (the server must run).
func (m *Manager) CreateDatabase(ctx context.Context, serviceID, name string) error {
	svc, err := m.sqlService(serviceID, "databases to create")
	if err != nil {
		return err
	}
	if err := checkDatabase(name); err != nil {
		return err
	}
	if err := m.needRunning(svc); err != nil {
		return err
	}
	t, err := m.local(svc)
	if err != nil {
		return err
	}
	dbs, err := m.listDatabases(ctx, t)
	if err != nil {
		return err
	}
	if hasName(t.Kind, databaseNames(dbs), name) {
		return apperr.Conflict("database %s exists", name)
	}
	return m.createDB(ctx, t, svc.ID, name)
}

func (m *Manager) createDB(ctx context.Context, t Target, logID, name string) error {
	q := "CREATE DATABASE " + quoteIdent(t.Kind, name) + " CHARACTER SET utf8mb4;\n"
	if family(t.Kind) == "postgres" {
		q = "CREATE DATABASE " + quoteIdent(t.Kind, name) + " ENCODING 'UTF8';\n"
	}
	_, err := m.sql(ctx, t, logID, "create database "+name, "", q)
	return err
}

// CreateUser creates a user with a password and, when database is not "",
// gives it every privilege on that database.
func (m *Manager) CreateUser(ctx context.Context, serviceID, user, password, database string) error {
	svc, err := m.sqlService(serviceID, "users to create")
	if err != nil {
		return err
	}
	if err := checkName("user", user); err != nil {
		return err
	}
	if err := checkPassword(password, true); err != nil {
		return err
	}
	if database != "" {
		if err := checkDatabase(database); err != nil {
			return err
		}
	}
	if err := m.needRunning(svc); err != nil {
		return err
	}
	t, err := m.local(svc)
	if err != nil {
		return err
	}
	users, err := m.listUsers(ctx, t)
	if err != nil {
		return err
	}
	if hasName("", users, user) || strings.EqualFold(user, t.User) {
		return apperr.Conflict("user %s exists", user)
	}
	if database != "" {
		dbs, err := m.listDatabases(ctx, t)
		if err != nil {
			return err
		}
		if !hasName(t.Kind, databaseNames(dbs), database) {
			return apperr.BadRequest("database %s does not exist", database)
		}
	}
	what := "create user " + user
	if database != "" {
		what += " with all privileges on " + database
	}
	var b strings.Builder
	if family(t.Kind) == "mysql" {
		// '%' for TCP from 127.0.0.1, 'localhost' so an anonymous ''@localhost
		// account cannot shadow it.
		for _, host := range []string{"%", "localhost"} {
			acct := mysqlString(user) + "@" + mysqlString(host)
			fmt.Fprintf(&b, "CREATE USER %s IDENTIFIED BY %s;\n", acct, mysqlString(password))
			if database != "" {
				fmt.Fprintf(&b, "GRANT ALL PRIVILEGES ON %s.* TO %s;\n", quoteIdent(t.Kind, database), acct)
			}
		}
		b.WriteString("FLUSH PRIVILEGES;\n")
		_, err = m.sql(ctx, t, svc.ID, what, "", b.String(), password)
		return err
	}
	role := quoteIdent(t.Kind, user)
	fmt.Fprintf(&b, "CREATE ROLE %s LOGIN PASSWORD %s;\n", role, pgString(password))
	if database != "" {
		fmt.Fprintf(&b, "GRANT ALL PRIVILEGES ON DATABASE %s TO %s;\n", quoteIdent(t.Kind, database), role)
	}
	if _, err = m.sql(ctx, t, svc.ID, what, "", b.String(), password); err != nil || database == "" {
		return err
	}
	// PostgreSQL 15+ no longer lets every role create tables in public.
	q := fmt.Sprintf("GRANT ALL ON SCHEMA public TO %[1]s;\nGRANT ALL ON ALL TABLES IN SCHEMA public TO %[1]s;\nGRANT ALL ON ALL SEQUENCES IN SCHEMA public TO %[1]s;\n", role)
	_, err = m.sql(ctx, t, svc.ID, "grant "+user+" the public schema of "+database, database, q)
	return err
}

// SetPassword changes the administrator password ("" removes it) and stores it
// in the service (the server must run).
func (m *Manager) SetPassword(ctx context.Context, serviceID, password string) (model.Service, error) {
	svc, err := m.dbService(serviceID)
	if err != nil {
		return svc, err
	}
	if err := checkPassword(password, false); err != nil {
		return svc, err
	}
	if err := m.needRunning(svc); err != nil {
		return svc, err
	}
	t, err := m.local(svc)
	if err != nil {
		return svc, err
	}
	what := "set the administrator password"
	if password == "" {
		what = "remove the administrator password"
	}
	switch family(t.Kind) {
	case "mysql":
		out, err := m.sql(ctx, t, "", "", "", "SELECT Host FROM mysql.user WHERE User = 'root';\n")
		if err != nil {
			return svc, err
		}
		var b strings.Builder
		for _, f := range rows(out) {
			fmt.Fprintf(&b, "ALTER USER 'root'@%s IDENTIFIED BY %s;\n", mysqlString(strings.TrimSpace(f[0])), mysqlString(password))
		}
		if b.Len() == 0 {
			return svc, apperr.Conflict("%s has no root account", svc.Name)
		}
		b.WriteString("FLUSH PRIVILEGES;\n")
		if _, err := m.sql(ctx, t, svc.ID, what+" (ALTER USER for every root account)", "", b.String(), password); err != nil {
			return svc, err
		}
	case "postgres":
		q := "ALTER USER postgres WITH PASSWORD NULL;\n"
		if password != "" {
			q = "ALTER USER postgres WITH PASSWORD " + pgString(password) + ";\n"
		}
		if _, err := m.sql(ctx, t, svc.ID, what+" (ALTER USER postgres)", "", q, password); err != nil {
			return svc, err
		}
	case "redis":
		bin, err := m.program(t, false, "")
		if err != nil {
			return svc, err
		}
		var out strings.Builder
		// -x: the last argument (the password) is read from stdin.
		err = m.exec(ctx, svc.ID, what+" (CONFIG SET requirepass)", Cmd{
			Bin: bin, Args: append(connArgs(t), "-x", "CONFIG", "SET", "requirepass"), Env: passwordEnv(t),
			Stdin: strings.NewReader(password), Stdout: &out, Timeout: queryTimeout,
		}, password)
		if res := strings.TrimSpace(out.String()); err == nil && res != "OK" {
			if password != "" {
				res = strings.ReplaceAll(res, password, "********")
			}
			m.log(svc.ID, what+" failed: "+res)
			err = apperr.Conflict("redis-cli: %s", res)
		}
		if err != nil {
			return svc, err
		}
	}
	if err := m.setAdminPassword(svc.ID, password); err != nil {
		return svc, err
	}
	if password == "" {
		m.log(svc.ID, "the administrator password was removed")
	} else {
		m.log(svc.ID, "the administrator password was changed; the clean stop, the connection variables and Open in use it")
	}
	return m.service(svc.ID)
}

// Reset deletes the service's data folder (the service must be stopped) and
// clears its password; the next start sets the database up again.
func (m *Manager) Reset(serviceID string) (model.Service, error) {
	svc, err := m.dbService(serviceID)
	if err != nil {
		return svc, err
	}
	if svc.CustomArgs {
		return svc, apperr.Conflict("%s has its own command line, so Zero2Dev does not know its data folder; reset it yourself", svc.Name)
	}
	switch m.Sup.State(svc.ID) {
	case model.StateStarting, model.StateRunning, model.StateStopping:
		return svc, apperr.Conflict("stop %s first", svc.Name)
	}
	if j := m.activeJob(svc.ID); j != "" {
		return svc, apperr.Conflict("a job is working on %s (%s); try again when it is done", svc.Name, j).With("jobId", j)
	}
	release, err := m.Sup.Reserve([]string{svc.ID}, "resetting the database")
	if err != nil {
		return svc, err
	}
	defer release()
	dir := m.Sup.DataDir(svc.Kind, svc.ID)
	m.log(svc.ID, "reset: deleting the data folder "+dir)
	if err := os.RemoveAll(dir); err != nil {
		m.log(svc.ID, "reset failed: "+err.Error())
		return svc, apperr.Conflict("cannot delete %s: %v; is a program still using it?", dir, err)
	}
	if err := m.setAdminPassword(svc.ID, ""); err != nil {
		return svc, err
	}
	m.log(svc.ID, "reset: done; the next start sets the database up again, empty and without a password")
	return m.service(svc.ID)
}

var _ ServerCopier = (*Manager)(nil)
