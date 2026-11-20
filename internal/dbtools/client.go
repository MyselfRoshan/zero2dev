package dbtools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"zero2dev/internal/apperr"
	"zero2dev/internal/launch"
	"zero2dev/internal/model"
)

// queryTimeout bounds the short client runs (listing, CREATE, ALTER USER ...).
// Dumps and imports have no limit.
const queryTimeout = time.Minute

var identRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// systemDatabases cannot be created, imported into, exported or dropped
// through dbtools, and are left out of every listing.
var systemDatabases = map[string]bool{
	"mysql": true, "information_schema": true, "performance_schema": true, "sys": true,
	"postgres": true, "template0": true, "template1": true,
}

// IsSystemDatabase reports whether name is one of the servers' own databases.
func IsSystemDatabase(name string) bool { return systemDatabases[strings.ToLower(name)] }

// checkName validates a database (what "database") or user (what "user") name.
func checkName(what, name string) error {
	if !identRe.MatchString(name) {
		return apperr.BadRequest("%s name must be 1-64 letters, digits or _", what)
	}
	return nil
}

// checkDatabase validates a database name and refuses the system databases.
func checkDatabase(name string) error {
	if err := checkName("database", name); err != nil {
		return err
	}
	if IsSystemDatabase(name) {
		return apperr.BadRequest("%s is a system database; Zero2Dev does not change it", name)
	}
	return nil
}

// maxPassword is the longest password accepted (in characters).
const maxPassword = 128

// checkPassword: one line of at most maxPassword characters; required refuses "".
func checkPassword(pw string, required bool) error {
	bad := !utf8.ValidString(pw) || strings.ContainsAny(pw, "\x00\r\n") || utf8.RuneCountInString(pw) > maxPassword
	if required && pw == "" || bad {
		if required {
			return apperr.BadRequest("password must be 1-%d characters on one line", maxPassword)
		}
		return apperr.BadRequest("password must be at most %d characters on one line", maxPassword)
	}
	return nil
}

// family groups the kinds that share client programs and SQL: "mysql"
// (MariaDB and MySQL), "postgres" or "redis"; "" for any other kind.
func family(kind string) string {
	switch kind {
	case "mariadb", "mysql":
		return "mysql"
	case "postgres", "redis":
		return kind
	}
	return ""
}

// mysqlString quotes s as a MariaDB/MySQL string literal (default sql_mode:
// backslash is an escape character).
func mysqlString(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(s) + "'"
}

// pgString quotes s as a PostgreSQL string literal (standard_conforming_strings
// is on: backslash is an ordinary character).
func pgString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// quoteIdent quotes a name that passed checkName (letters, digits, _ only).
func quoteIdent(kind, name string) string {
	if family(kind) == "postgres" {
		return `"` + name + `"`
	}
	return "`" + name + "`"
}

// clientNames are the file names (without .exe) tried, in order, for the
// interactive client (dump false) or the dump program (dump true).
func clientNames(kind string, dump bool) []string {
	switch kind {
	case "mariadb":
		if dump {
			return []string{"mariadb-dump", "mysqldump"}
		}
		return []string{"mariadb", "mysql"}
	case "mysql":
		if dump {
			return []string{"mysqldump", "mariadb-dump"}
		}
		return []string{"mysql", "mariadb"}
	case "postgres":
		if dump {
			return []string{"pg_dump"}
		}
		return []string{"psql"}
	case "redis":
		if !dump {
			return []string{"redis-cli"}
		}
	}
	return nil
}

// exe adds .exe on Windows.
func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// findClient looks for the first of names in dir, in dir\bin and in the
// sibling ..\bin folder.
func findClient(dir string, names []string) string {
	if dir == "" {
		return ""
	}
	dirs := []string{dir, filepath.Join(dir, "bin"), filepath.Join(filepath.Dir(dir), "bin")}
	for _, n := range names {
		for _, d := range dirs {
			p := filepath.Join(d, exe(n))
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// program finds the client (dump false) or dump program (dump true) for t.
// t.ClientDir is searched first; when it is empty, the folder of the default
// runtime of t.Kind (for MariaDB/MySQL also the other of the two), then
// fallback (the destination service's folder for a copy).
func (m *Manager) program(t Target, dump bool, fallback string) (string, error) {
	names := clientNames(t.Kind, dump)
	dirs := []string{t.ClientDir}
	if t.ClientDir == "" {
		kinds := []string{t.Kind}
		switch t.Kind {
		case "mariadb":
			kinds = append(kinds, "mysql")
		case "mysql":
			kinds = append(kinds, "mariadb")
		}
		for _, k := range kinds {
			if _, bin, err := m.Sup.RuntimeFor(model.Service{Kind: k}); err == nil && bin != "" {
				dirs = append(dirs, filepath.Dir(bin))
			}
		}
	}
	if fallback != "" {
		dirs = append(dirs, fallback)
	}
	for _, d := range dirs {
		if p := findClient(d, names); p != "" {
			return p, nil
		}
	}
	where := "the server's folder"
	for _, d := range dirs {
		if d != "" {
			where = d
			break
		}
	}
	return "", apperr.Conflict("the %s program %s was not found in %s; this %s build does not include its client programs",
		t.Kind, exe(names[0]), where, t.Kind)
}

// normalize fills in the defaults of a target: host 127.0.0.1, the
// administrator user.
func normalize(t Target) Target {
	if t.Host == "" {
		t.Host = "127.0.0.1"
	}
	if t.User == "" {
		t.User = launch.DBUser(t.Kind)
	}
	return t
}

// connArgs are the connection options of the client programs of t. Every
// option is one argument with "=", so no value can be read as another option.
// No password: it goes in the environment (passwordEnv).
func connArgs(t Target) []string {
	port := strconv.Itoa(t.Port)
	switch family(t.Kind) {
	case "mysql":
		return []string{"--no-defaults", "--host=" + t.Host, "--port=" + port, "--protocol=tcp", "--user=" + t.User, "--default-character-set=utf8mb4"}
	case "postgres":
		return []string{"--host=" + t.Host, "--port=" + port, "--username=" + t.User, "--no-password"}
	case "redis":
		return []string{"-h", t.Host, "-p", port}
	}
	return nil
}

// passwordEnv carries t's password to its client programs: MYSQL_PWD (always
// set, so a MYSQL_PWD of the daemon's own environment never leaks in),
// PGPASSWORD and REDISCLI_AUTH (only when there is one).
func passwordEnv(t Target) []string {
	switch family(t.Kind) {
	case "mysql":
		return []string{"MYSQL_PWD=" + t.Password}
	case "postgres":
		env := []string{"PGCLIENTENCODING=UTF8"}
		if t.Password != "" {
			env = append(env, "PGPASSWORD="+t.Password)
		}
		return env
	case "redis":
		if t.Password != "" {
			return []string{"REDISCLI_AUTH=" + t.Password}
		}
	}
	return nil
}

// sqlArgs are the client arguments that run the SQL read from stdin in db
// ("" = none for MariaDB/MySQL, postgres for PostgreSQL) and print the rows
// tab-separated without headers.
func sqlArgs(t Target, db string) []string {
	args := connArgs(t)
	switch family(t.Kind) {
	case "mysql":
		args = append(args, "--batch", "--skip-column-names")
		if db != "" {
			args = append(args, "--database="+db)
		}
	case "postgres":
		if db == "" {
			db = "postgres"
		}
		args = append(args, "--no-psqlrc", "--quiet", "--tuples-only", "--no-align", "--field-separator=\t", "--set=ON_ERROR_STOP=1", "--dbname="+db)
	}
	return args
}

// importArgs read a dump from stdin into db.
func importArgs(t Target, db string) []string {
	args := connArgs(t)
	switch family(t.Kind) {
	case "mysql":
		return append(args, "--max-allowed-packet=1073741824", "--database="+db)
	case "postgres":
		return append(args, "--no-psqlrc", "--quiet", "--set=ON_ERROR_STOP=1", "--dbname="+db)
	}
	return args
}

// dumpArgs write db as SQL to stdout.
func dumpArgs(t Target, db string) []string {
	args := connArgs(t)
	switch family(t.Kind) {
	case "mysql":
		return append(args, "--single-transaction", "--routines", "--triggers", "--max-allowed-packet=1073741824", db)
	case "postgres":
		return append(args, "--no-owner", "--format=plain", "--encoding=UTF8", "--dbname="+db)
	}
	return args
}

// sql runs statements (read by the client from stdin) on t and returns what
// it printed. logID "" logs nothing (listings); secrets are hidden in errors.
func (m *Manager) sql(ctx context.Context, t Target, logID, what, db, statements string, secrets ...string) (string, error) {
	bin, err := m.program(t, false, "")
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	err = m.exec(ctx, logID, what, Cmd{
		Bin: bin, Args: sqlArgs(t, db), Env: passwordEnv(t),
		Stdin: strings.NewReader(statements), Stdout: &out, Timeout: queryTimeout,
	}, secrets...)
	return out.String(), err
}

// rows splits client output into tab-separated fields, one slice per line.
func rows(out string) [][]string {
	var res [][]string
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		res = append(res, strings.Split(line, "\t"))
	}
	return res
}

// listDatabases lists t's databases with their sizes, system ones left out.
func (m *Manager) listDatabases(ctx context.Context, t Target) ([]Database, error) {
	var q string
	switch family(t.Kind) {
	case "mysql":
		q = "SELECT s.SCHEMA_NAME, COALESCE(SUM(t.DATA_LENGTH + t.INDEX_LENGTH), 0) FROM information_schema.SCHEMATA s " +
			"LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = s.SCHEMA_NAME GROUP BY s.SCHEMA_NAME ORDER BY s.SCHEMA_NAME;\n"
	case "postgres":
		q = "SELECT datname, pg_database_size(datname) FROM pg_database WHERE datallowconn AND NOT datistemplate ORDER BY datname;\n"
	default:
		return []Database{}, nil
	}
	out, err := m.sql(ctx, t, "", "", "", q)
	if err != nil {
		return nil, err
	}
	dbs := []Database{}
	for _, f := range rows(out) {
		name := strings.TrimSpace(f[0])
		if name == "" || IsSystemDatabase(name) {
			continue
		}
		d := Database{Name: name}
		if len(f) > 1 {
			if n, err := strconv.ParseFloat(strings.TrimSpace(f[1]), 64); err == nil && n > 0 {
				d.SizeBytes = int64(n)
			}
		}
		dbs = append(dbs, d)
	}
	return dbs, nil
}

// listUsers lists the login accounts of t (MariaDB/MySQL: the internal
// mysql.* / mariadb.* accounts and anonymous ones left out).
func (m *Manager) listUsers(ctx context.Context, t Target) ([]string, error) {
	var q string
	switch family(t.Kind) {
	case "mysql":
		q = "SELECT DISTINCT User FROM mysql.user ORDER BY User;\n"
	case "postgres":
		q = "SELECT rolname FROM pg_roles WHERE rolcanlogin ORDER BY rolname;\n"
	default:
		return []string{}, nil
	}
	out, err := m.sql(ctx, t, "", "", "", q)
	if err != nil {
		return nil, err
	}
	users := []string{}
	for _, f := range rows(out) {
		u := strings.TrimSpace(f[0])
		if u == "" || u == "PUBLIC" || strings.HasPrefix(u, "mysql.") || strings.HasPrefix(u, "mariadb.") {
			continue
		}
		users = append(users, u)
	}
	return users, nil
}

// hasName reports whether name is in list (MariaDB/MySQL on Windows compare
// names without case).
func hasName(kind string, list []string, name string) bool {
	for _, n := range list {
		if n == name || family(kind) == "mysql" && strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

func databaseNames(dbs []Database) []string {
	out := make([]string, len(dbs))
	for i, d := range dbs {
		out[i] = d.Name
	}
	return out
}
