package supervisor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// Databases run as ordinary services, plus three things a plain process does
// not need: a data folder inside the Zero2Dev folder, a one-time setup that
// creates the database files, and a stop that asks the server to shut down
// cleanly (on Windows a stop otherwise ends the process tree at once, which
// risks damaging data files).

// DBGrace is how long a database gets to shut down after its stop command.
const DBGrace = 20 * time.Second

// dbPlan is the database part of a resolved service.
type dbPlan struct {
	data               string   // data folder ("" when the user set custom args)
	initBin            string   // "" = create the folder only
	initArgs           []string //
	stopBin            string   // "" = no clean-stop command
	stopArgs           []string //
	initDesc, stopDesc string   // for the log
}

// DataDir is where a database service keeps its files: <home>/<kind>/<id>,
// e.g. data\postgres\svc_1a2b3c4d.
func (s *Supervisor) DataDir(kind, serviceID string) string {
	return filepath.Join(s.home, model.DataDirName(kind), serviceID)
}

// sibling finds the first of names next to bin (with .exe on Windows).
func sibling(bin string, names ...string) string {
	dir := filepath.Dir(bin)
	for _, n := range names {
		if runtime.GOOS == "windows" && filepath.Ext(n) == "" {
			n += ".exe"
		}
		p := filepath.Join(dir, n)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// planDB returns how to set up and stop a database service. custom is true
// when the user gave their own args: then Zero2Dev does not know the data
// folder and leaves setup to them, but still stops the server cleanly where
// that needs only the port.
func (s *Supervisor) planDB(svc model.Service, bin string, custom bool) *dbPlan {
	if !model.IsDatabase(svc.Kind) {
		return nil
	}
	p := &dbPlan{}
	if !custom {
		p.data = s.DataDir(svc.Kind, svc.ID)
	}
	port := strconv.Itoa(svc.Port)
	switch svc.Kind {
	case "mariadb":
		if p.data != "" {
			if p.initBin = sibling(bin, "mariadb-install-db", "mysql_install_db"); p.initBin != "" {
				p.initArgs, p.initDesc = []string{"--datadir=" + p.data}, "mariadb-install-db"
			}
		}
		p.stopBin = sibling(bin, "mariadb-admin", "mysqladmin")
	case "mysql":
		if p.data != "" {
			p.initBin, p.initDesc = bin, "mysqld --initialize-insecure"
			p.initArgs = []string{"--no-defaults", "--initialize-insecure", "--datadir=" + p.data}
			if runtime.GOOS == "windows" {
				p.initArgs = append(p.initArgs, "--console")
			}
		}
		p.stopBin = sibling(bin, "mysqladmin")
	case "postgres":
		if p.data != "" {
			if p.initBin = sibling(bin, "initdb"); p.initBin != "" {
				p.initArgs, p.initDesc = []string{"-D", p.data, "-U", "postgres", "-A", "trust", "-E", "UTF8"}, "initdb"
			}
			if p.stopBin = sibling(bin, "pg_ctl"); p.stopBin != "" {
				p.stopArgs = []string{"stop", "-D", p.data, "-m", "fast", "-w"}
			}
		}
	case "redis":
		p.stopBin = sibling(bin, "redis-cli")
		if p.stopBin != "" && svc.Port > 0 {
			p.stopArgs = []string{"-h", "127.0.0.1", "-p", port, "shutdown"}
		}
	}
	if (svc.Kind == "mariadb" || svc.Kind == "mysql") && p.stopBin != "" {
		if svc.Port > 0 {
			p.stopArgs = []string{"--no-defaults", "-u", "root", "--host=127.0.0.1", "--port=" + port, "--protocol=tcp", "shutdown"}
		} else {
			p.stopBin = ""
		}
	}
	if p.stopBin != "" {
		p.stopDesc = strings.TrimSuffix(filepath.Base(p.stopBin), filepath.Ext(p.stopBin))
	}
	return p
}

// needsInit: the data folder is missing or empty.
func needsInit(dir string) bool {
	ents, err := os.ReadDir(dir)
	return err != nil || len(ents) == 0
}

// initDB creates the database files on first start. A failed setup removes
// the half-made folder so the next start tries again from scratch.
func (s *Supervisor) initDB(id string, p *dbPlan, env []string) error {
	if p == nil || p.data == "" || !needsInit(p.data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.data), 0o755); err != nil {
		return err
	}
	if p.initBin == "" { // redis: an empty folder is all it needs
		return os.MkdirAll(p.data, 0o755)
	}
	if p.initDesc != "mariadb-install-db" && p.initDesc != "initdb" {
		// mysqld --initialize wants the folder absent or empty; initdb and
		// mariadb-install-db create it themselves.
		if err := os.MkdirAll(p.data, 0o755); err != nil {
			return err
		}
	}
	s.logLine(id, "system", "first start: creating the database files with "+p.initDesc+" (this takes a moment)")
	err := s.runTool(id, p.initBin, p.initArgs, env, 2*time.Minute)
	if err != nil {
		_ = os.RemoveAll(p.data)
		return fmt.Errorf("creating the database files failed (%s): %v; see the service log", p.initDesc, err)
	}
	s.logLine(id, "system", "database files created in "+p.data)
	s.clearAdminPassword(id)
	return nil
}

// clearAdminPassword forgets the stored administrator password of a MariaDB,
// MySQL or PostgreSQL service whose data files were just created: the new
// database has none (its password lives in those files). Redis keeps its
// password, which is passed on its command line.
func (s *Supervisor) clearAdminPassword(id string) {
	cleared := false
	_ = s.st.Update(func(d *store.Data) error {
		for i := range d.Services {
			if sv := &d.Services[i]; sv.ID == id && sv.AdminPassword != "" && sv.Kind != "redis" {
				sv.AdminPassword, cleared = "", true
			}
		}
		return nil
	})
	if cleared {
		s.logLine(id, "system", "the stored administrator password was cleared: the new database has none")
		s.em.Publish("services.changed", struct{}{})
	}
}

// adminPassword returns the stored administrator password and kind of a service.
func (s *Supervisor) adminPassword(id string) (pw, kind string) {
	for _, sv := range s.st.Snapshot().Services {
		if sv.ID == id {
			return sv.AdminPassword, sv.Kind
		}
	}
	return "", ""
}

// withAdminPassword adds the service's current administrator password to the
// environment of a helper program (the clean stop runs mariadb-admin /
// mysqladmin with MYSQL_PWD and redis-cli with REDISCLI_AUTH). It is read at
// the time of the call, so a password set while the server runs is used by
// its stop. Never on a command line, never logged.
func (s *Supervisor) withAdminPassword(id string, env []string) []string {
	pw, kind := s.adminPassword(id)
	if pw == "" {
		return env
	}
	out := append([]string{}, env...)
	switch kind {
	case "mariadb", "mysql":
		out = append(out, "MYSQL_PWD="+pw)
	case "postgres":
		out = append(out, "PGPASSWORD="+pw)
	case "redis":
		out = append(out, "REDISCLI_AUTH="+pw)
	}
	return out
}

// dbStartArgs are arguments Zero2Dev adds to a database server's command
// line: --requirepass for a Redis service with an administrator password.
func dbStartArgs(svc model.Service) []string {
	if svc.Kind == "redis" && svc.AdminPassword != "" {
		return []string{"--requirepass", svc.AdminPassword}
	}
	return nil
}

// Masked is how the command preview and the start log show a password.
const Masked = "********"

// maskArgs hides the value after --requirepass.
func maskArgs(args []string) []string {
	out := append([]string{}, args...)
	for i := 0; i+1 < len(out); i++ {
		if strings.EqualFold(out[i], "--requirepass") {
			out[i+1] = Masked
		}
	}
	return out
}

// maskEnv hides the database password in the variables Zero2Dev sets
// (DB_PASSWORD, and the password part of DATABASE_URL / REDIS_URL).
func maskEnv(env []string) []string {
	out := append([]string{}, env...)
	for i, kv := range out {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "DB_PASSWORD":
			if v != "" && v != "(hidden)" {
				out[i] = k + "=" + Masked
			}
		case "DATABASE_URL", "REDIS_URL":
			scheme, rest, ok := strings.Cut(v, "://")
			if !ok {
				continue
			}
			at := strings.LastIndex(rest, "@")
			if at < 0 {
				continue
			}
			if user, _, hasPw := strings.Cut(rest[:at], ":"); hasPw {
				out[i] = k + "=" + scheme + "://" + user + ":" + Masked + rest[at:]
			}
		}
	}
	return out
}

// runTool runs a helper program of a database (setup or shutdown), sending
// its output to the service log as system lines.
func (s *Supervisor) runTool(id, bin string, args, env []string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) // never a shell
	cmd.Env = s.withAdminPassword(id, env)
	cmd.Dir = filepath.Dir(bin)
	configure(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		if t := strings.TrimRight(sc.Text(), "\r"); t != "" {
			s.logLine(id, "system", "  "+t)
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return fmt.Errorf("timed out after %s", timeout)
	}
	return err
}
