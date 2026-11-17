package supervisor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

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
func serviceVars(svc model.Service, d store.Data, rt *model.Runtime, planEnv []string, lenient bool) (added, shown, warnings, info []string, err error) {
	add := func(kv string) { added, shown = append(added, kv), append(shown, kv) }
	if rt != nil {
		if name, dir := model.RuntimeHome(*rt); name != "" {
			add(name + "=" + dir)
		}
	}
	for _, e := range planEnv {
		add(e)
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
