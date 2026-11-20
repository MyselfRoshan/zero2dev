package dbtools

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"

	"zero2dev/internal/apperr"
)

// checkRemote validates and completes a Target naming another server.
func checkRemote(src Target) (Target, error) {
	if f := family(src.Kind); f != "mysql" && f != "postgres" {
		return src, apperr.BadRequest("databases can only be copied from MariaDB, MySQL or PostgreSQL, not %q", src.Kind)
	}
	if src.Port < 1 || src.Port > 65535 {
		return src, apperr.BadRequest("port must be 1-65535")
	}
	if strings.ContainsAny(src.Host+src.User+src.Password, "\x00\r\n") || len(src.Host) > 253 || len(src.User) > 64 {
		return src, apperr.BadRequest("host, user and password must be one line each")
	}
	src = normalize(src)
	return src, nil
}

// ListRemote: see ServerCopier.
func (m *Manager) ListRemote(ctx context.Context, src Target) ([]Database, error) {
	src, err := checkRemote(src)
	if err != nil {
		return nil, err
	}
	if _, err := m.program(src, false, ""); err != nil {
		return nil, err
	}
	dbs, err := m.listDatabases(ctx, src)
	if err != nil {
		return nil, apperr.Conflict("cannot list the databases of %s:%d: %v", src.Host, src.Port, err)
	}
	return dbs, nil
}

// CopyFromServer: see ServerCopier. Each database is dumped from src and
// piped straight into the import (no temporary file). The databases must not
// exist in the service yet. When a copy fails, the databases this call
// created are dropped again, so nothing is left half copied.
func (m *Manager) CopyFromServer(ctx context.Context, src Target, dbs []string, dstServiceID string, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	src, err := checkRemote(src)
	if err != nil {
		return err
	}
	dst, err := m.sqlService(dstServiceID, "databases to copy into")
	if err != nil {
		return err
	}
	if family(dst.Kind) != family(src.Kind) {
		return apperr.BadRequest("%s databases cannot be copied into %s, a %s server", src.Kind, dst.Name, dst.Kind)
	}
	if len(dbs) == 0 {
		return apperr.BadRequest("choose at least one database")
	}
	for _, d := range dbs {
		if err := checkDatabase(d); err != nil {
			return err
		}
	}
	if err := m.needRunning(dst); err != nil {
		return err
	}
	dt, err := m.local(dst)
	if err != nil {
		return err
	}
	importBin, err := m.program(dt, false, "")
	if err != nil {
		return err
	}
	dumpBin, err := m.program(src, true, dt.ClientDir)
	if err != nil {
		return err
	}
	have, err := m.listDatabases(ctx, dt)
	if err != nil {
		return err
	}
	for _, d := range dbs {
		if hasName(dt.Kind, databaseNames(have), d) {
			return apperr.Conflict("database %s already exists in %s", d, dst.Name)
		}
	}
	var created []string
	fail := func(err error) error {
		if len(created) > 0 {
			m.log(dst.ID, "copy failed; dropping the databases it created: "+strings.Join(created, ", "))
			_ = m.drop(context.Background(), dt, dst.ID, created)
		}
		return err
	}
	from := fmt.Sprintf("%s:%d", src.Host, src.Port)
	for i, d := range dbs {
		progress(i*100/len(dbs), "Copying "+d)
		if err := m.createDB(ctx, dt, dst.ID, d); err != nil {
			return fail(err)
		}
		created = append(created, d)
		if err := m.pipe(ctx, dst.ID, from, d,
			Cmd{Bin: dumpBin, Args: dumpArgs(src, d), Env: passwordEnv(src)},
			Cmd{Bin: importBin, Args: importArgs(dt, d), Env: passwordEnv(dt), Stdout: io.Discard},
		); err != nil {
			return fail(err)
		}
		m.log(dst.ID, "copied "+d+" from "+from)
	}
	progress(100, "Copied")
	return nil
}

// pipe runs dump and imp at the same time with dump's output as imp's input.
// The error is the one of the side that failed first.
func (m *Manager) pipe(ctx context.Context, logID, from, db string, dump, imp Cmd) error {
	pr, pw := io.Pipe()
	dump.Stdout, imp.Stdin = pw, pr
	var first atomic.Int32 // 1: the dump failed first, 2: the import
	impErr := make(chan error, 1)
	go func() {
		err := m.exec(ctx, logID, "copy: import into "+db, imp)
		if err != nil {
			first.CompareAndSwap(0, 2)
		}
		pr.CloseWithError(fmt.Errorf("the import stopped"))
		impErr <- err
	}()
	derr := m.exec(ctx, logID, "copy: dump "+db+" from "+from, dump, dump.passwords()...)
	if derr != nil {
		first.CompareAndSwap(0, 1)
		pw.CloseWithError(derr)
	} else {
		pw.Close()
	}
	ierr := <-impErr
	switch {
	case derr != nil && (first.Load() == 1 || ierr == nil):
		return fmt.Errorf("dumping %s from %s failed: %v", db, from, derr)
	case ierr != nil:
		return fmt.Errorf("importing %s failed: %v", db, ierr)
	}
	return nil
}

// passwords are the values of the password variables of c, to hide in errors.
func (c Cmd) passwords() []string {
	var out []string
	for _, kv := range c.Env {
		k, v, _ := strings.Cut(kv, "=")
		if v != "" && (k == "MYSQL_PWD" || k == "PGPASSWORD" || k == "REDISCLI_AUTH") {
			out = append(out, v)
		}
	}
	return out
}

// DropDatabases: see ServerCopier.
func (m *Manager) DropDatabases(ctx context.Context, serviceID string, dbs []string) error {
	svc, err := m.sqlService(serviceID, "databases to drop")
	if err != nil {
		return err
	}
	for _, d := range dbs {
		if err := checkDatabase(d); err != nil {
			return err
		}
	}
	if len(dbs) == 0 {
		return nil
	}
	if err := m.needRunning(svc); err != nil {
		return err
	}
	t, err := m.local(svc)
	if err != nil {
		return err
	}
	return m.drop(ctx, t, svc.ID, dbs)
}

func (m *Manager) drop(ctx context.Context, t Target, logID string, dbs []string) error {
	var b strings.Builder
	for _, d := range dbs {
		fmt.Fprintf(&b, "DROP DATABASE IF EXISTS %s;\n", quoteIdent(t.Kind, d))
	}
	_, err := m.sql(ctx, t, logID, "drop "+strings.Join(dbs, ", "), "", b.String())
	return err
}
