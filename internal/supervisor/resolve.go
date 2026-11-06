package supervisor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/fsutil"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// Default program names looked up on PATH per kind.
var progName = map[string]string{"node": "node", "php": "php", "go": "go", "java": "java", "caddy": "caddy", "nginx": "nginx",
	"mariadb": "mariadbd", "mysql": "mysqld", "postgres": "postgres", "redis": "redis-server"}

// resolved is everything needed to spawn a service.
type resolved struct {
	bin      string
	args     []string
	env      []string
	added    []string // the part of env Zero2Dev adds (for the preview)
	workdir  string
	dataDir  string  // the service's data folder ("" when none)
	db       *dbPlan // database services: data folder, first-start setup, clean stop
	dirs     []string
	files    []kinds.File
	warnings []string
}

// Preview is the command line a service runs, as shown by the UI.
type Preview struct {
	Program  string   `json:"program"`
	Args     []string `json:"args"`
	Env      []string `json:"env"` // the variables Zero2Dev adds, KEY=VALUE
	Workdir  string   `json:"workdir"`
	DataDir  string   `json:"dataDir"`
	Files    []string `json:"files"`
	Warnings []string `json:"warnings"`
}

// Preview resolves svc without starting it and without writing anything.
// Problems (no runtime installed, a missing program) become warnings.
func (s *Supervisor) Preview(svc model.Service) Preview {
	r, err := s.resolveWith(svc, s.st.Snapshot(), true)
	if err != nil {
		return Preview{Args: []string{}, Env: []string{}, Files: []string{}, Warnings: []string{err.Error()}}
	}
	files := []string{}
	for _, f := range r.files {
		files = append(files, f.Path)
	}
	return Preview{Program: r.bin, Args: nonNil(r.args), Env: nonNil(r.added), Workdir: r.workdir, DataDir: r.dataDir, Files: files, Warnings: nonNil(r.warnings)}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// resolve turns a Service into a concrete command line following the
// "Command resolution" rules of docs/API.md. No shell is ever involved.
// It only reads: folders and files are created by Start.
func (s *Supervisor) resolve(svc model.Service, d store.Data) (*resolved, error) {
	return s.resolveWith(svc, d, false)
}

func (s *Supervisor) resolveWith(svc model.Service, d store.Data, lenient bool) (*resolved, error) {
	r := &resolved{}
	bin := strings.TrimSpace(svc.Command)
	extraPath, version, runtimeBin := "", "", ""
	if bin == "" {
		if svc.Kind == "custom" || svc.Kind == "" {
			if !lenient {
				return nil, apperr.BadRequest("command is required for custom services")
			}
			r.warnings = append(r.warnings, "no program set")
		} else {
			rt, path, err := findRuntime(svc, d)
			switch {
			case err == nil && rt != nil:
				bin, extraPath, version = rt.Binary, filepath.Dir(rt.Binary), rt.Version
			case err == nil:
				bin = path
			case lenient:
				bin = progName[svc.Kind]
				if bin == "" {
					bin = svc.Kind
				}
				r.warnings = append(r.warnings, err.Error())
			default:
				return nil, err
			}
			runtimeBin = bin
		}
	} else if !filepath.IsAbs(bin) && strings.ContainsAny(bin, `/\`) {
		bin = filepath.Join(svc.Cwd, bin) // relative paths are relative to cwd
	}

	prof, _ := kinds.Lookup(svc.Kind)
	if prof != nil {
		switch prof.DataDir {
		case "service":
			r.dataDir = s.DataDir(svc.Kind, svc.ID)
		case "shared":
			r.dataDir = filepath.Join(s.home, model.DataDirName(svc.Kind))
		}
	}
	ctx := kinds.Ctx{Home: s.home, Data: r.dataDir, Bin: bin, Cwd: svc.Cwd, Port: strconv.Itoa(svc.Port),
		Version: version, Windows: runtime.GOOS == "windows", ServiceID: svc.ID}
	if !svc.CustomArgs {
		if err := kinds.Validate(svc.Kind, svc.Settings); err != nil {
			if !lenient {
				return nil, apperr.BadRequest("%v", err)
			}
			r.warnings = append(r.warnings, err.Error())
		}
	}
	var plan *kinds.Plan
	if svc.CustomArgs {
		plan = kinds.BuildCustom(svc.Kind, svc.Args, ctx)
	} else {
		plan = kinds.Build(svc.Kind, svc.Settings, svc.Args, ctx)
	}
	r.warnings = append(r.warnings, plan.Warnings...)
	if plan.Program != "" && strings.TrimSpace(svc.Command) == "" {
		// e.g. npm next to node.exe: the same Node version runs the script.
		if p := siblingProgram(runtimeBin, plan.Program); p != "" {
			bin = p
		} else {
			bin = plan.Program
		}
	}

	if filepath.IsAbs(bin) {
		if st, err := os.Stat(bin); err != nil || st.IsDir() {
			if !lenient {
				return nil, apperr.Conflict("command %s not found", bin)
			}
			r.warnings = append(r.warnings, "program "+bin+" not found")
		}
	} else if bin != "" {
		if p, err := exec.LookPath(bin); err == nil {
			bin = p
		} else if !lenient {
			return nil, apperr.Conflict("command %q not found on PATH", bin)
		} else {
			r.warnings = append(r.warnings, fmt.Sprintf("program %q not found on PATH", bin))
		}
	}
	r.bin, r.args = bin, plan.Args

	port := strconv.Itoa(svc.Port)
	add := func(kv string) { r.added = append(r.added, kv) }
	for _, e := range plan.Env {
		add(e)
	}
	keys := make([]string, 0, len(svc.Env))
	for k := range svc.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys { // the user's own variables win over the settings
		add(k + "=" + strings.ReplaceAll(svc.Env[k], "{port}", port))
	}
	if svc.Port > 0 {
		add("PORT=" + port)
	}
	if isCaddy(svc, bin) {
		// Caddy writes certificates and config below XDG_* (on every OS,
		// else %AppData%\Caddy). Keep all of it inside the data folder.
		// Added last so it overrides any user-provided value.
		data, cfg, tmp := filepath.Join(s.home, "caddy", "data"), filepath.Join(s.home, "caddy", "config"), filepath.Join(s.home, "tmp")
		r.dirs = append(r.dirs, data, cfg, tmp)
		for _, kv := range []string{"XDG_DATA_HOME=" + data, "XDG_CONFIG_HOME=" + cfg, "TMP=" + tmp, "TEMP=" + tmp, "TMPDIR=" + tmp} {
			add(kv)
		}
	}
	env := os.Environ()
	if extraPath != "" {
		env = prependPath(env, extraPath)
	}
	r.env = append(env, r.added...)
	r.dirs = append(r.dirs, plan.Dirs...)
	r.files = plan.Files

	switch {
	case svc.Cwd != "":
		r.workdir = svc.Cwd
	case r.dataDir != "" && prof != nil && prof.DataDir == "service":
		r.workdir = r.dataDir
	case filepath.IsAbs(bin):
		r.workdir = filepath.Dir(bin)
	default:
		r.workdir = s.home
	}
	r.db = s.planDB(svc, bin, svc.CustomArgs)
	return r, nil
}

// siblingProgram finds name (name.cmd / name.exe on Windows) next to the runtime binary.
func siblingProgram(runtimeBin, name string) string {
	if runtimeBin == "" || !filepath.IsAbs(runtimeBin) {
		return ""
	}
	dir := filepath.Dir(runtimeBin)
	cands := []string{name}
	if runtime.GOOS == "windows" {
		cands = []string{name + ".cmd", name + ".exe", name + ".bat"}
	}
	for _, c := range cands {
		p := filepath.Join(dir, c)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// isCaddy: kind caddy, or the resolved binary is named caddy / caddy.exe.
func isCaddy(svc model.Service, bin string) bool {
	if svc.Kind == "caddy" {
		return true
	}
	b := strings.ToLower(filepath.Base(bin))
	return b == "caddy" || b == "caddy.exe"
}

// findRuntime returns the runtime a service uses (its own, else the default
// of its kind, else the first of its kind), or a program found on PATH.
func findRuntime(svc model.Service, d store.Data) (*model.Runtime, string, error) {
	if svc.RuntimeID != "" {
		for i, r := range d.Runtimes {
			if r.ID == svc.RuntimeID {
				return &d.Runtimes[i], "", nil
			}
		}
		return nil, "", apperr.Conflict("runtime %s not found", svc.RuntimeID)
	}
	first := -1
	for i, r := range d.Runtimes {
		if r.Kind != svc.Kind {
			continue
		}
		if r.Default { // the default of the kind wins
			return &d.Runtimes[i], "", nil
		}
		if first < 0 {
			first = i
		}
	}
	if first >= 0 {
		return &d.Runtimes[first], "", nil
	}
	if name := progName[svc.Kind]; name != "" {
		if p, e := exec.LookPath(name); e == nil {
			return nil, p, nil
		}
	}
	return nil, "", apperr.Conflict("no %s runtime registered and none found on PATH", svc.Kind)
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

// prepare creates the folders and writes the files (php.ini) a service needs,
// and reports the settings that were left out.
func (s *Supervisor) prepare(id string, r *resolved) error {
	for _, w := range r.warnings {
		s.logLine(id, "system", "note: "+w)
	}
	for _, d := range r.dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("creating %s: %v", d, err)
		}
	}
	for _, f := range r.files {
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			return err
		}
		if err := fsutil.WriteFileAtomic(f.Path, f.Content, 0o644); err != nil {
			return fmt.Errorf("writing %s: %v", f.Path, err)
		}
		s.logLine(id, "system", "wrote "+f.Path)
	}
	return nil
}
