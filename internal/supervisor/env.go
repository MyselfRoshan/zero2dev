package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// Environ is the full environment a service starts with: the daemon's own
// variables with the runtime on PATH, then (later wins) the runtime home
// (JAVA_HOME / GOROOT), the kind's own variables, the database variables, the
// .env file, the user's Env and PORT (see serviceVars). It is what "Open
// terminal" gives the console it opens. Problems that Start would refuse (no
// runtime installed...) are tolerated: the environment is built as far as it can be.
func (s *Supervisor) Environ(svc model.Service) ([]string, error) {
	r, err := s.resolveWith(svc, s.st.Snapshot(), true)
	if err != nil {
		return nil, err
	}
	return r.env, nil
}

// serviceVars lists the variables Zero2Dev adds on top of the daemon's
// environment, in this order (a later one overrides an earlier one, because
// the process environment keeps the last value of a name):
//
//  1. the runtime home: JAVA_HOME (java) or GOROOT (go), only when the service
//     uses a Zero2Dev runtime (rt != nil)
//  2. the kind's own variables from its settings (plan, e.g. NODE_ENV)
//     2b. the portable folders switched on in Settings.Portable (see portableVars)
//  3. the database variables of Service.DatabaseID
//  4. the .env file (Service.EnvFile, relative to the folder)
//  5. the user's own Env ({port} is replaced)
//  6. PORT
//
// (resolve adds Caddy's XDG_*/TMP folders after these.) The PATH entry of the
// runtime is put in front by resolve, before all of them.
//
// added is what the process gets; shown is the same list for the preview, with
// the values of secret-looking .env variables hidden. warnings are for the
// preview and the start log, info lines for the start log only. A .env path
// that leaves the folder is an error unless lenient.
//
// serviceVars itself cannot add step 2b: it does not know the data folder.
// resolve gets it by calling s.serviceVarsWithHome instead (the same with
// s.home).
func serviceVars(svc model.Service, d store.Data, rt *model.Runtime, planEnv []string, lenient bool) (added, shown, warnings, info []string, err error) {
	return serviceVarsHome("", svc, d, rt, planEnv, lenient)
}

// serviceVarsWithHome is serviceVars with the portable folders of step 2b.
func (s *Supervisor) serviceVarsWithHome(svc model.Service, d store.Data, rt *model.Runtime, planEnv []string, lenient bool) (added, shown, warnings, info []string, err error) {
	return serviceVarsHome(s.home, svc, d, rt, planEnv, lenient)
}

// serviceVarsHome is serviceVars for the data folder home ("": no portable folders).
func serviceVarsHome(home string, svc model.Service, d store.Data, rt *model.Runtime, planEnv []string, lenient bool) (added, shown, warnings, info []string, err error) {
	add := func(kv string) { added, shown = append(added, kv), append(shown, kv) }
	if rt != nil {
		if name, dir := model.RuntimeHome(*rt); name != "" {
			add(name + "=" + dir)
		}
	}
	for _, e := range planEnv {
		add(e)
	}
	if home != "" {
		pAdded, pShown := portableVarsGo(home, svc.Kind, d.Settings.Portable, rt, os.Environ(), goIsolated(svc))
		added, shown = append(added, pAdded...), append(shown, pShown...)
	}
	dbVars, w := databaseEnv(svc, d)
	if w != "" {
		warnings = append(warnings, w)
	}
	for _, e := range dbVars {
		add(e)
	}
	fileVars, fw, ferr := loadEnvFile(svc.Cwd, svc.EnvFile)
	switch {
	case ferr != nil && !lenient:
		return nil, nil, nil, nil, ferr
	case ferr != nil:
		warnings = append(warnings, ferr.Error())
	}
	warnings = append(warnings, fw...)
	for _, v := range fileVars {
		added = append(added, v.Key+"="+v.Value)
		shown = append(shown, v.Key+"="+displayValue(v.Key, v.Value))
	}
	if len(fileVars) > 0 {
		info = append(info, fmt.Sprintf("loaded %d variables from %s", len(fileVars), strings.TrimSpace(svc.EnvFile)))
	}
	port := strconv.Itoa(svc.Port)
	keys := make([]string, 0, len(svc.Env))
	for k := range svc.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys { // the user's own variables win over everything above
		add(k + "=" + strings.ReplaceAll(svc.Env[k], "{port}", port))
	}
	if svc.Port > 0 {
		add("PORT=" + port)
	}
	return added, shown, warnings, info, nil
}

// secretWords: a variable whose name contains one of these has its value
// hidden in the preview (the process still gets it).
var secretWords = []string{"PASS", "SECRET", "TOKEN", "KEY", "CREDENTIAL", "AUTH", "PRIVATE", "SIGNATURE", "COOKIE", "SESSION"}

// displayValue is the value shown in the preview for a variable from the .env file.
func displayValue(key, value string) string {
	up := strings.ToUpper(key)
	for _, w := range secretWords {
		if strings.Contains(up, w) {
			if value == "" {
				return ""
			}
			return "(hidden)"
		}
	}
	// A connection URL may carry user:password@host.
	if _, rest, ok := strings.Cut(value, "://"); ok {
		auth, _, _ := strings.Cut(rest, "/")
		if cred, _, ok := strings.Cut(auth, "@"); ok && strings.Contains(cred, ":") {
			return "(hidden)"
		}
	}
	return value
}

// portableRule is one entry of a kind's "portable" list in profiles.json:
// what a service of that kind gets while Settings.Portable.<setting> is on
// (Import switches it on, its revert off). {home} is the data folder.
type portableRule struct {
	Setting string           `json:"setting"` // npmGlobal | composer | goCache
	Env     []kinds.FixedEnv `json:"env"`
	Path    string           `json:"path,omitempty"` // a folder put first on PATH
	Help    string           `json:"help"`
}

// portableRules maps a kind to its rules, read once from profiles.json.
var portableRules = func() map[string][]portableRule {
	var list []struct {
		Kind     string         `json:"kind"`
		Portable []portableRule `json:"portable"`
	}
	if err := json.Unmarshal(kinds.JSON(), &list); err != nil {
		panic("supervisor: profiles.json: " + err.Error())
	}
	out := map[string][]portableRule{}
	for _, k := range list {
		if len(k.Portable) > 0 {
			out[k.Kind] = k.Portable
		}
	}
	return out
}()

func portableOn(p model.Portable, setting string) bool {
	switch setting {
	case "npmGlobal":
		return p.NpmGlobal
	case "composer":
		return p.Composer
	case "goCache":
		return p.GoCache
	}
	return false
}

// portableVars is what Settings.Portable adds for a service of kind: the
// variables of its rules that are on, and a PATH with their folders first.
// The PATH entry is the whole value (the folders, the runtime's folder that
// resolve puts first, then the daemon's PATH from environ) because the last
// value of a name wins; the preview shows it as "<folders>;%PATH%".
func portableVars(home, kind string, p model.Portable, rt *model.Runtime, environ []string) (added, shown []string) {
	return portableVarsGo(home, kind, p, rt, environ, true)
}

// goIsolated tells whether a Go service keeps Go's downloads in the data folder:
// the "isolate" setting, on unless it says false.
func goIsolated(svc model.Service) bool { return svc.Settings["isolate"] != "false" }

// defaultGoBin is where `go install` puts programs when GOPATH is not Zero2Dev's: the
// bin folder of Go's own default GOPATH (the GOPATH variable of environ, else %USERPROFILE%\go).
func defaultGoBin(environ []string) string {
	gopath := ""
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "GOPATH") {
			gopath = v
		}
	}
	userHome, _ := os.UserHomeDir()
	return model.DefaultGoBin(gopath, userHome)
}

// portableVarsGo is portableVars; isolated is the Go service's setting (see goIsolated).
func portableVarsGo(home, kind string, p model.Portable, rt *model.Runtime, environ []string, isolated bool) (added, shown []string) {
	expand := func(v string) string {
		return filepath.FromSlash(strings.ReplaceAll(v, "{home}", filepath.ToSlash(home)))
	}
	var dirs []string
	for _, r := range portableRules[kind] {
		if !portableOn(p, r.Setting) {
			continue
		}
		for _, e := range r.Env {
			kv := e.Name + "=" + expand(e.Value)
			added, shown = append(added, kv), append(shown, kv)
		}
		if r.Path != "" {
			dirs = append(dirs, expand(r.Path))
		}
	}
	// Go's own tools (air, dlv, gopls: what `go install` puts in <GOPATH>/bin) are on
	// PATH next to GOROOT's. A Go service that keeps Go's downloads in the data folder
	// (the default) gets GOPATH=<home>/go/path and that bin folder; with the setting off
	// GOPATH stays Go's own and its default bin folder (%USERPROFILE%\go\bin) is used.
	// A custom service (air run as a command) gets each of the two folders that exist.
	switch kind {
	case "go":
		if isolated {
			kv := "GOPATH=" + model.GoPath(home)
			added, shown = append(added, kv), append(shown, kv)
			dirs = append(dirs, model.GoToolsDir(home))
		} else if d := defaultGoBin(environ); d != "" {
			dirs = append(dirs, d)
		}
	case "custom":
		for _, d := range []string{model.GoToolsDir(home), defaultGoBin(environ)} {
			if st, err := os.Stat(d); d != "" && err == nil && st.IsDir() && !slices.Contains(dirs, d) {
				dirs = append(dirs, d)
			}
		}
	}
	if len(dirs) == 0 {
		return added, shown
	}
	name, cur := "PATH", ""
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") {
			name, cur = k, v
		}
	}
	if rt != nil && rt.Binary != "" {
		cur = joinList(filepath.Dir(rt.Binary), cur)
	}
	sep := string(os.PathListSeparator)
	rest := "$PATH"
	if runtime.GOOS == "windows" {
		rest = "%PATH%"
	}
	added = append(added, name+"="+joinList(strings.Join(dirs, sep), cur))
	shown = append(shown, name+"="+strings.Join(dirs, sep)+sep+rest)
	return added, shown
}

func joinList(a, b string) string {
	if b == "" {
		return a
	}
	if a == "" {
		return b
	}
	return a + string(os.PathListSeparator) + b
}
