package envreg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// The user's choices (model.EnvConfig) turned into what goes into the user
// environment, and what is there now.

const maxItems = 50

var (
	varNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	itemIDRE  = regexp.MustCompile(`^env[pv]_[A-Za-z0-9_]{1,40}$`)
	absWinRE  = regexp.MustCompile(`^([A-Za-z]:[\\/]|\\\\[^\\])`)
)

// reserved are variables Windows or Zero2Dev's services depend on; they are never set
// from here. PATH has its own list of folders.
var reserved = map[string]bool{
	"PATH": true, "PATHEXT": true, "COMSPEC": true, "SYSTEMROOT": true, "SYSTEMDRIVE": true, "WINDIR": true,
	"TEMP": true, "TMP": true, "USERNAME": true, "USERDOMAIN": true, "USERPROFILE": true, "APPDATA": true,
	"LOCALAPPDATA": true, "PROGRAMDATA": true, "PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "PROGRAMW6432": true,
	"COMMONPROGRAMFILES": true, "HOMEDRIVE": true, "HOMEPATH": true, "COMPUTERNAME": true, "OS": true,
	"NUMBER_OF_PROCESSORS": true, "PROCESSOR_ARCHITECTURE": true, "PROCESSOR_IDENTIFIER": true, "PSMODULEPATH": true,
	"ZERO2DEV_HOME": true, "ZERO2DEV_PORT": true,
}

// Kinds a variable can follow the default runtime of.
func supportedKind(k string) bool { return slices.Contains(kindOrder, k) }

func runtimeByID(rts []model.Runtime, id string) (model.Runtime, bool) {
	for _, r := range rts {
		if r.ID == id {
			return r, true
		}
	}
	return model.Runtime{}, false
}

// runtimeValue is what a variable of a runtime holds: its install folder for Java and
// Go (JAVA_HOME, GOROOT style), the folder of its program for the rest.
func runtimeValue(rt model.Runtime) string {
	if _, dir := model.RuntimeHome(rt); dir != "" {
		return dir
	}
	return filepath.Dir(rt.Binary)
}

// Validate checks the config the user sent and returns it cleaned up: names and
// folders trimmed, ids made for new items, duplicates refused. A runtime that was
// removed meanwhile is only an error on an item that is switched on.
func Validate(cfg model.EnvConfig, runtimes []model.Runtime) (model.EnvConfig, error) {
	if len(cfg.Path) > maxItems || len(cfg.Vars) > maxItems {
		return cfg, fmt.Errorf("at most %d folders and %d variables", maxItems, maxItems)
	}
	out := model.EnvConfig{Seeded: true, Path: []model.EnvPath{}, Vars: []model.EnvVar{}}
	ids := map[string]bool{}
	newID := func(id, prefix string) (string, error) {
		if id == "" {
			for {
				id = model.NewID(prefix)
				if !ids[id] {
					break
				}
			}
		} else if !itemIDRE.MatchString(id) || !strings.HasPrefix(id, prefix) {
			return "", fmt.Errorf("%q is not a valid item id", id)
		}
		if ids[id] {
			return "", fmt.Errorf("the id %q is used twice", id)
		}
		ids[id] = true
		return id, nil
	}

	seenPath := map[string]bool{}
	shimsSeen := false
	for i, p := range cfg.Path {
		p.Source = strings.ToLower(strings.TrimSpace(p.Source))
		p.Dir, p.RuntimeID = strings.TrimSpace(p.Dir), strings.TrimSpace(p.RuntimeID)
		var key string
		switch p.Source {
		case model.EnvShims:
			if shimsSeen {
				return cfg, errors.New("the shims folder is listed twice")
			}
			shimsSeen = true
			p.Dir, p.RuntimeID, key = "", "", "shims"
		case model.EnvRuntime:
			if p.RuntimeID == "" {
				return cfg, fmt.Errorf("folder %d: choose a runtime", i+1)
			}
			if _, ok := runtimeByID(runtimes, p.RuntimeID); !ok && p.Enabled {
				return cfg, fmt.Errorf("folder %d: the runtime %s is not installed any more", i+1, p.RuntimeID)
			}
			p.Dir, key = "", "runtime:"+p.RuntimeID
		case model.EnvCustom:
			if !absWinRE.MatchString(p.Dir) && !filepath.IsAbs(p.Dir) {
				return cfg, fmt.Errorf("folder %d: %q is not a full path like C:\\tools\\bin", i+1, p.Dir)
			}
			if err := checkDir(p.Dir); err != nil {
				return cfg, fmt.Errorf("folder %d: %w", i+1, err)
			}
			p.RuntimeID, key = "", "dir:"+normEntry(p.Dir)
		default:
			return cfg, fmt.Errorf("folder %d: the source must be shims, runtime or custom", i+1)
		}
		if seenPath[key] {
			return cfg, fmt.Errorf("folder %d is listed twice", i+1)
		}
		seenPath[key] = true
		id, err := newID(p.ID, "envp_")
		if err != nil {
			return cfg, err
		}
		p.ID = id
		out.Path = append(out.Path, p)
	}

	seenName := map[string]bool{}
	for i, v := range cfg.Vars {
		v.Name = strings.TrimSpace(v.Name)
		v.Source = strings.ToLower(strings.TrimSpace(v.Source))
		v.Kind, v.RuntimeID = strings.ToLower(strings.TrimSpace(v.Kind)), strings.TrimSpace(v.RuntimeID)
		if !varNameRE.MatchString(v.Name) {
			return cfg, fmt.Errorf("variable %d: %q is not a valid name (letters, digits and _, not starting with a digit)", i+1, v.Name)
		}
		up := strings.ToUpper(v.Name)
		if reserved[up] {
			return cfg, fmt.Errorf("variable %d: %s is used by Windows or Zero2Dev and cannot be set here", i+1, v.Name)
		}
		if seenName[up] {
			return cfg, fmt.Errorf("the variable %s is listed twice", v.Name)
		}
		seenName[up] = true
		switch v.Source {
		case model.EnvDefault:
			if !supportedKind(v.Kind) {
				return cfg, fmt.Errorf("variable %s: choose which runtime kind it follows", v.Name)
			}
			v.RuntimeID, v.Value = "", ""
		case model.EnvRuntime:
			if v.RuntimeID == "" {
				return cfg, fmt.Errorf("variable %s: choose a runtime", v.Name)
			}
			if _, ok := runtimeByID(runtimes, v.RuntimeID); !ok && v.Enabled {
				return cfg, fmt.Errorf("variable %s: the runtime %s is not installed any more", v.Name, v.RuntimeID)
			}
			v.Kind, v.Value = "", ""
		case model.EnvCustom:
			if strings.ContainsRune(v.Value, 0) || len(v.Value) > 2000 {
				return cfg, fmt.Errorf("variable %s: the value is too long or has an invalid character", v.Name)
			}
			if strings.TrimSpace(v.Value) == "" && v.Enabled {
				return cfg, fmt.Errorf("variable %s: enter a value", v.Name)
			}
			v.Kind, v.RuntimeID = "", ""
		default:
			return cfg, fmt.Errorf("variable %s: the source must be default, runtime or custom", v.Name)
		}
		id, err := newID(v.ID, "envv_")
		if err != nil {
			return cfg, err
		}
		v.ID = id
		out.Vars = append(out.Vars, v)
	}
	return out, nil
}

// Resolved is what one item stands for right now.
type Resolved struct {
	Value   string // the folder, or the variable value; "" when it cannot be worked out
	Problem string // why it cannot be used, for the user
}

// Plan is the config resolved against the installed runtimes.
type Plan struct {
	Desired Desired
	Path    map[string]Resolved // by item id, enabled or not
	Vars    map[string]Resolved
}

// anyEnabled reports whether the user switched anything on.
func anyEnabled(c model.EnvConfig) bool {
	return slices.ContainsFunc(c.Path, func(p model.EnvPath) bool { return p.Enabled }) ||
		slices.ContainsFunc(c.Vars, func(v model.EnvVar) bool { return v.Enabled })
}

// usable looks at a runtime the way an item needs it: it must be known and its
// program must be on disk.
func usable(rt model.Runtime, ok bool, kindWord string, exists func(string) bool) (model.Runtime, string) {
	if !ok {
		if kindWord != "" {
			return rt, "No " + kindWord + " runtime is installed."
		}
		return rt, "That runtime was removed."
	}
	if rt.Binary == "" || !exists(rt.Binary) {
		return rt, "The program of " + rt.Name + " is not on disk."
	}
	return rt, ""
}

// Resolve works out, for every item, the folder or value it stands for, and the
// Desired state of the ones switched on that can be used. exists tells whether a
// file is there (os.Stat in production).
func Resolve(home string, d store.Data, exists func(string) bool) Plan {
	plan := Plan{Desired: Desired{Vars: map[string]string{}}, Path: map[string]Resolved{}, Vars: map[string]Resolved{}}
	wantDir := map[string]bool{}
	for _, p := range d.Env.Path {
		var r Resolved
		switch p.Source {
		case model.EnvShims:
			r.Value = ShimsDir(home)
		case model.EnvRuntime:
			rt, ok := runtimeByID(d.Runtimes, p.RuntimeID)
			if rt, r.Problem = usable(rt, ok, "", exists); r.Problem == "" {
				r.Value = filepath.Dir(rt.Binary)
			}
		case model.EnvCustom:
			r.Value = p.Dir
		}
		plan.Path[p.ID] = r
		if p.Enabled && r.Value != "" && !wantDir[normEntry(r.Value)] {
			wantDir[normEntry(r.Value)] = true
			plan.Desired.Dirs = append(plan.Desired.Dirs, r.Value)
		}
	}
	wantName := map[string]bool{}
	for _, v := range d.Env.Vars {
		var r Resolved
		switch v.Source {
		case model.EnvDefault:
			rt, ok := defaultOf(d, v.Kind)
			if rt, r.Problem = usable(rt, ok, v.Kind, exists); r.Problem == "" {
				r.Value = runtimeValue(rt)
			}
		case model.EnvRuntime:
			rt, ok := runtimeByID(d.Runtimes, v.RuntimeID)
			if rt, r.Problem = usable(rt, ok, "", exists); r.Problem == "" {
				r.Value = runtimeValue(rt)
			}
		case model.EnvCustom:
			r.Value = v.Value
		}
		plan.Vars[v.ID] = r
		up := strings.ToUpper(v.Name)
		if v.Enabled && r.Value != "" && !wantName[up] {
			wantName[up] = true
			plan.Desired.Vars[v.Name] = r.Value
		}
	}
	return plan
}

// Report is the user environment next to the config, for the Environment page.
type Report struct {
	Supported bool         `json:"supported"` // false off Windows: nothing can be changed
	ShimsDir  string       `json:"shimsDir"`
	Error     string       `json:"error,omitempty"` // the user environment could not be read
	Path      []PathStatus `json:"path"`
	Vars      []VarStatus  `json:"vars"`
}

// PathStatus is one folder item: what it stands for and whether it is on the user Path now.
type PathStatus struct {
	ID      string `json:"id"`
	Dir     string `json:"dir"`
	Present bool   `json:"present"` // on the user Path now
	Owned   bool   `json:"owned"`   // Zero2Dev added it and takes it off again
	Problem string `json:"problem,omitempty"`
	// Commands are what the folder gives a terminal (node, npm ...), sorted; the first
	// folder on PATH that has a command wins it.
	Commands []string `json:"commands"`
}

// maxCommands caps the commands listed for a folder of the user's own.
const maxCommands = 20

// folderCommands lists the commands a folder item provides, read-only:
//   - the shims folder: one per generated shim (activate scripts left out);
//   - a runtime's folder: its own program and the tools next to it that get a shim;
//   - a folder of the user's: its .exe, .cmd and .bat files (at most maxCommands).
func folderCommands(home string, d store.Data, p model.EnvPath, exists func(string) bool, readDir func(string) ([]os.DirEntry, error)) []string {
	out := []string{}
	add := func(name string) {
		name = strings.ToLower(name)
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	switch p.Source {
	case model.EnvShims:
		for _, f := range FilesAt(home, d, exists, readDir) {
			if !strings.HasPrefix(f.Name, "activate.") {
				add(strings.TrimSuffix(f.Name, ".cmd"))
			}
		}
	case model.EnvRuntime:
		rt, ok := runtimeByID(d.Runtimes, p.RuntimeID)
		if !ok || rt.Binary == "" {
			break
		}
		dir, ext := filepath.Dir(rt.Binary), filepath.Ext(rt.Binary)
		add(strings.TrimSuffix(filepath.Base(rt.Binary), ext))
		for _, cmd := range shimmed[rt.Kind] {
			for _, e := range []string{ext, ".cmd", ".bat"} {
				if exists(filepath.Join(dir, cmd+e)) {
					add(cmd)
					break
				}
			}
		}
	case model.EnvCustom:
		entries, err := readDir(p.Dir)
		if err != nil {
			break
		}
		for _, e := range entries {
			if len(out) >= maxCommands {
				break
			}
			switch ext := strings.ToLower(filepath.Ext(e.Name())); {
			case e.IsDir():
			case ext == ".exe" || ext == ".cmd" || ext == ".bat":
				add(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
			}
		}
	}
	slices.Sort(out)
	return out
}

// VarStatus is one variable item: the value it stands for and what the user has now.
type VarStatus struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Value      string `json:"value"`
	Current    string `json:"current,omitempty"`
	HasCurrent bool   `json:"hasCurrent"`
	Owned      bool   `json:"owned"` // the current value is the one Zero2Dev set
	Problem    string `json:"problem,omitempty"`
}

// Inspect reads the user environment (read-only) and sets it against the config.
func Inspect(home string, d store.Data) Report {
	plan := Resolve(home, d, fileExists)
	rep := Report{ShimsDir: ShimsDir(home), Path: []PathStatus{}, Vars: []VarStatus{}}
	reg := openReg()
	rep.Supported = reg != nil
	var userPath string
	var backup Backup
	if reg != nil {
		backup, _ = loadBackup(BackupFile(home), ShimsDir(home))
		cur, typ, ok, err := reg.Get("Path")
		switch {
		case err != nil:
			rep.Error = "Cannot read your user PATH: " + err.Error()
		case ok && isString(typ):
			userPath = cur
		}
	}
	for _, p := range d.Env.Path {
		r := plan.Path[p.ID]
		st := PathStatus{ID: p.ID, Dir: r.Value, Problem: r.Problem, Commands: folderCommands(home, d, p, fileExists, os.ReadDir)}
		if r.Value != "" {
			st.Present = hasEntry(userPath, r.Value)
			st.Owned = slices.ContainsFunc(backup.Path.Owned, func(o string) bool { return normEntry(o) == normEntry(r.Value) })
		}
		rep.Path = append(rep.Path, st)
	}
	for _, v := range d.Env.Vars {
		r := plan.Vars[v.ID]
		st := VarStatus{ID: v.ID, Name: v.Name, Value: r.Value, Problem: r.Problem}
		if reg != nil {
			if cur, typ, ok, err := reg.Get(v.Name); err == nil && ok && isString(typ) {
				st.Current, st.HasCurrent = cur, true
				if rec, has := backup.Vars[v.Name]; has && strings.EqualFold(rec, cur) {
					st.Owned = true
				}
			}
		}
		rep.Vars = append(rep.Vars, st)
	}
	return rep
}
