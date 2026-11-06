package kinds

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Ctx is what Build needs to know about the service and its runtime.
type Ctx struct {
	Home      string // the data folder
	Data      string // this service's data folder ("" when the kind has none)
	Bin       string // the runtime binary ("" when unknown)
	Cwd       string // the project folder
	Port      string
	Version   string // the runtime version ("" when unknown)
	Windows   bool
	ServiceID string
}

// File is a file the service needs written before it starts.
type File struct {
	Path    string
	Content []byte
}

// Plan is the command line and side files of a service.
type Plan struct {
	Program  string   // run this program (looked up next to the runtime) instead of the runtime binary
	Args     []string //
	Env      []string // KEY=VALUE, added after the user's variables (Zero2Dev's win)
	Files    []File
	Dirs     []string // folders to create before the start
	Warnings []string
}

var settingRef = regexp.MustCompile(`\{s\.([A-Za-z0-9_]+)\}`)

func (c Ctx) expand(s, v string, eff map[string]string, extra map[string]string) string {
	s = settingRef.ReplaceAllStringFunc(s, func(m string) string { return eff[m[3:len(m)-1]] })
	r := []string{
		"{v}", v,
		"{port}", c.Port,
		"{home}", filepath.ToSlash(c.Home),
		"{data}", c.Data,
		"{dataSlash}", filepath.ToSlash(c.Data),
		"{cwd}", c.Cwd,
		"{binDir}", filepath.ToSlash(filepath.Dir(c.Bin)),
	}
	for k, val := range extra {
		r = append(r, "{"+k+"}", val)
	}
	return strings.NewReplacer(r...).Replace(s)
}

// ExpandPlain replaces {port}, {home} and {data} in user-written arguments.
func (c Ctx) ExpandPlain(args []string) []string {
	out := make([]string, len(args))
	r := strings.NewReplacer("{port}", c.Port, "{home}", filepath.ToSlash(c.Home), "{data}", c.Data)
	for i, a := range args {
		out[i] = r.Replace(a)
	}
	return out
}

// Build turns a service's settings into its command line. extra are the
// user's own arguments, added last.
func Build(kind string, settings map[string]string, extra []string, c Ctx) *Plan {
	pl := &Plan{}
	p, ok := Lookup(kind)
	if !ok {
		pl.Args = c.ExpandPlain(extra)
		return pl
	}
	eff := Effective(kind, settings)
	vars := map[string]string{}
	if kind == "php" {
		vars["docroot"] = filepath.Clean(filepath.Join(c.Cwd, filepath.FromSlash(eff["docroot"])))
		vars["ini"] = PHPIniPath(c.Data)
	}
	for _, f := range p.Fixed {
		if f.Windows && !c.Windows {
			continue
		}
		for _, a := range f.Args {
			pl.Args = append(pl.Args, c.expand(a, "", eff, vars))
		}
	}
	var ini [][2]string
	var tail []string
	for i := range p.Options {
		o := &p.Options[i]
		if !p.Visible(o, eff) {
			continue
		}
		v := eff[o.Key]
		args, env, prog, iv := emitOption(o, v)
		if (len(args) > 0 || len(env) > 0) && o.MinVersion != "" && !VersionAtLeast(c.Version, o.MinVersion) {
			pl.Warnings = append(pl.Warnings, o.Label+" needs "+p.Label+" "+o.MinVersion+" or newer (this is "+c.Version+"); left out")
			continue
		}
		for i := range args {
			args[i] = c.expand(args[i], v, eff, vars)
		}
		if o.At == "end" {
			tail = append(tail, args...)
		} else {
			pl.Args = append(pl.Args, args...)
		}
		for _, e := range env {
			pl.Env = append(pl.Env, c.expand(e, v, eff, vars))
		}
		if prog != "" {
			pl.Program = prog
		}
		if o.Emit.Ini != "" && iv != "" {
			ini = append(ini, [2]string{o.Emit.Ini, iv})
		}
	}
	pl.Args = append(pl.Args, tail...)
	if len(extra) > 0 {
		if pl.Program == "npm" {
			pl.Args = append(pl.Args, "--") // npm passes what follows to the script
		}
		pl.Args = append(pl.Args, c.ExpandPlain(extra)...)
	}
	for _, e := range p.Env {
		pl.Env = append(pl.Env, e.Name+"="+c.expand(e.Value, "", eff, vars))
	}
	if kind == "php" && c.Data != "" {
		phpPlan(pl, eff, ini, c)
	}
	if kind == "go" && eff["isolate"] == "true" {
		pl.Dirs = append(pl.Dirs, filepath.Join(c.Home, "go"))
	}
	return pl
}

// emitOption returns what one option with value v writes. env entries are
// KEY=VALUE templates; iv is the php.ini value.
func emitOption(o *Option, v string) (args, env []string, prog, iv string) {
	e := o.Emit
	switch o.Type {
	case "bool":
		if v == "true" {
			args = append(args, e.Arg...)
			for k, val := range e.EnvSet {
				env = append(env, k+"="+val)
			}
			iv = "On"
		} else {
			args = append(args, e.ArgOff...)
			iv = "Off"
		}
	case "select":
		if ch := o.choice(v); ch != nil && ch.Args != nil {
			args = append(args, ch.Args...)
		} else if v != "" {
			args = append(args, e.Arg...)
		}
		iv = v
	default:
		if v != "" {
			args = append(args, e.Arg...)
		}
		iv = v
	}
	if e.Env != "" && v != "" {
		env = append(env, e.Env+"="+v)
	}
	if e.Program != nil {
		prog = e.Program[v]
	}
	slices.Sort(env) // EnvSet is a map: keep the order stable
	return args, env, prog, iv
}

// BuildCustom is the plan of a service whose arguments the user wrote
// themselves: their arguments as they are, plus the variables Zero2Dev always
// sets for the kind (such as npm's cache folder).
func BuildCustom(kind string, args []string, c Ctx) *Plan {
	pl := &Plan{Args: c.ExpandPlain(args)}
	if p, ok := Lookup(kind); ok {
		eff := Defaults(kind)
		for _, e := range p.Env {
			pl.Env = append(pl.Env, e.Name+"="+c.expand(e.Value, "", eff, nil))
		}
	}
	return pl
}
