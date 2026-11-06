package supervisor

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// Default program names looked up on PATH per kind.
var progName = map[string]string{"node": "node", "php": "php", "go": "go", "java": "java", "caddy": "caddy", "nginx": "nginx",
	"mariadb": "mariadbd", "mysql": "mysqld", "postgres": "postgres", "redis": "redis-server"}

// resolved is everything needed to spawn a service.
type resolved struct {
	bin  string
	args []string
	env  []string
	db   *dbPlan // database services: data folder, first-start setup, clean stop
}

// resolve turns a Service into a concrete command line following the
// "Command resolution" rules of docs/API.md. No shell is ever involved.
func (s *Supervisor) resolve(svc model.Service, d store.Data) (*resolved, error) {
	bin := strings.TrimSpace(svc.Command)
	extraPath := ""
	if bin == "" {
		if svc.Kind == "custom" || svc.Kind == "" {
			return nil, apperr.BadRequest("command is required for custom services")
		}
		var err error
		bin, extraPath, err = findRuntimeBinary(svc, d)
		if err != nil {
			return nil, err
		}
	} else if !filepath.IsAbs(bin) && strings.ContainsAny(bin, `/\`) {
		bin = filepath.Join(svc.Cwd, bin) // relative paths are relative to cwd
	}
	if filepath.IsAbs(bin) {
		if st, err := os.Stat(bin); err != nil || st.IsDir() {
			return nil, apperr.Conflict("command %s not found", bin)
		}
	} else {
		p, err := exec.LookPath(bin)
		if err != nil {
			return nil, apperr.Conflict("command %q not found on PATH", bin)
		}
		bin = p
	}

	args := svc.Args
	if len(args) == 0 {
		switch svc.Kind {
		case "php":
			args = []string{"-S", "127.0.0.1:{port}", "-t", svc.Cwd}
		case "go":
			args = []string{"run", "."}
		case "caddy":
			args = []string{"run", "--config", filepath.Join(s.home, "config", "Caddyfile"), "--adapter", "caddyfile"}
		case "nginx":
			// nginx needs its prefix (conf/, logs/, html/): default to the folder of the binary.
			args = []string{"-p", filepath.ToSlash(filepath.Dir(bin)) + "/"}
		case "node", "java":
			return nil, apperr.BadRequest("args required for %s services", svc.Kind)
		case "mariadb", "mysql", "postgres", "redis":
			args = defaultDBArgs(svc.Kind, s.DataDir(svc.ID))
		}
	}
	port := strconv.Itoa(svc.Port)
	// {home} is the data folder in slash form (nginx and most tools accept
	// forward slashes on Windows; backslashes would be escapes in nginx).
	home := filepath.ToSlash(s.home)
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(strings.ReplaceAll(a, "{port}", port), "{home}", home)
	}

	env := os.Environ()
	if extraPath != "" {
		env = prependPath(env, extraPath)
	}
	keys := make([]string, 0, len(svc.Env))
	for k := range svc.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+strings.ReplaceAll(svc.Env[k], "{port}", port))
	}
	if svc.Port > 0 {
		env = append(env, "PORT="+port)
	}
	if isCaddy(svc, bin) {
		// Caddy writes certificates and config below XDG_* (on every OS,
		// else %AppData%\Caddy). Keep all of it inside the data folder.
		// Appended last so it overrides any user-provided value.
		data, cfg, tmp := filepath.Join(s.home, "caddy", "data"), filepath.Join(s.home, "caddy", "config"), filepath.Join(s.home, "tmp")
		for _, d := range []string{data, cfg, tmp} {
			_ = os.MkdirAll(d, 0o755)
		}
		env = append(env, "XDG_DATA_HOME="+data, "XDG_CONFIG_HOME="+cfg, "TMP="+tmp, "TEMP="+tmp, "TMPDIR="+tmp)
	}
	return &resolved{bin: bin, args: out, env: env, db: s.planDB(svc, bin, len(svc.Args) > 0)}, nil
}

// isCaddy: kind caddy, or the resolved binary is named caddy / caddy.exe.
func isCaddy(svc model.Service, bin string) bool {
	if svc.Kind == "caddy" {
		return true
	}
	b := strings.ToLower(filepath.Base(bin))
	return b == "caddy" || b == "caddy.exe"
}

func findRuntimeBinary(svc model.Service, d store.Data) (bin, dir string, err error) {
	if svc.RuntimeID != "" {
		for _, r := range d.Runtimes {
			if r.ID == svc.RuntimeID {
				return r.Binary, filepath.Dir(r.Binary), nil
			}
		}
		return "", "", apperr.Conflict("runtime %s not found", svc.RuntimeID)
	}
	first := -1
	for i, r := range d.Runtimes {
		if r.Kind != svc.Kind {
			continue
		}
		if r.Default { // the default of the kind wins
			return r.Binary, filepath.Dir(r.Binary), nil
		}
		if first < 0 {
			first = i
		}
	}
	if first >= 0 {
		r := d.Runtimes[first]
		return r.Binary, filepath.Dir(r.Binary), nil
	}
	if name := progName[svc.Kind]; name != "" {
		if p, e := exec.LookPath(name); e == nil {
			return p, "", nil
		}
	}
	return "", "", apperr.Conflict("no %s runtime registered and none found on PATH", svc.Kind)
}

// prependPath puts dir at the front of PATH in env (case-insensitive key
// match so it also works for "Path" on Windows).
func prependPath(env []string, dir string) []string {
	for i, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") {
			env[i] = k + "=" + dir + string(os.PathListSeparator) + v
			return env
		}
	}
	return append(env, "PATH="+dir)
}

// portFree reports whether 127.0.0.1:port can be bound and nothing answers.
func portFree(port int) bool {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if c, err := net.DialTimeout("tcp", addr, 200*1e6); err == nil {
		c.Close()
		return false
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	l.Close()
	return true
}
