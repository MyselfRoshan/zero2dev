// Package frameworks is the list of frameworks New project can start from, shipped with Zero2Dev
// as data (frameworks.json, embedded; <home>\frameworks.json replaces it) so new ones are added
// without code. Each entry says what it needs, which generator makes the project, and what to
// create around it (services, site, .env).
//
// Placeholders, in strings of an entry (all values are checked before they are used):
//
//	{name}        the project name as typed        {slug}   name as a folder/host name: "my-shop"
//	{dir}         the project folder (absolute)    {parent} the folder that holds {dir}
//	{port}        the port of the main service     {domain} "my-shop.localhost"
//	{db}          the database name                {dbUser} {dbPassword} {dbHost} {dbPort}
//	{dbUrl}       a connection URL built from the four above for the database kind
//	{appKey}      32 random bytes, base64          {random}  16 random hex characters
//	{venv}        the Python virtual environment   {home}    the Zero2Dev data folder
//
// {appKey} and {random} give a new value at every use. The database placeholders are empty when
// the project has no database, and an env line that uses one is then left out.
//
// Contract (docs/API.md, "Amendment: languages, tasks and new projects (0.0.6)").
package frameworks

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"zero2dev/internal/model"
)

//go:embed frameworks.json
var builtin []byte

// Languages, as New project groups the frameworks.
var Languages = []string{"php", "javascript", "python", "go", "java", "static"}

// Step types.
const (
	StepRun      = "run"      // run a program (npm create ..., composer create-project ...) with the runtime of Needs
	StepDownload = "download" // download a file or archive with a checksum and unpack it into the project folder
	StepFiles    = "files"    // write starter files
)

// Framework is one entry of the list.
type Framework struct {
	ID       string `json:"id"`       // "laravel", lower case letters, digits and -
	Label    string `json:"label"`    // "Laravel"
	Language string `json:"language"` // one of Languages
	Summary  string `json:"summary"`  // one line
	Homepage string `json:"homepage,omitempty"`

	// Runtime is the language runtime the generator and the services use.
	Runtime Runtime `json:"runtime"`
	// Composer: the generator needs the installed Composer (PHP frameworks).
	Composer bool `json:"composer,omitempty"`
	// Database is what the project can use; nil: no database.
	Database *Database `json:"database,omitempty"`

	Steps    []Step       `json:"steps"`
	Services []ServiceTpl `json:"services"`
	Site     *SiteTpl     `json:"site,omitempty"`
	Env      []EnvLine    `json:"env,omitempty"`
	// EnvFile is the file the Env lines go to, relative to the project folder; default ".env".
	EnvFile string `json:"envFile,omitempty"`
}

// Runtime is a runtime kind and the versions the framework works with.
type Runtime struct {
	Kind string `json:"kind"` // node | php | python | go | java | "" (static: none)
	// Min and Max are inclusive version bounds, "8.2" or "20.0.0"; empty means no bound. Zero2Dev
	// installs the newest catalog version inside them when none is installed.
	Min string `json:"min,omitempty"`
	Max string `json:"max,omitempty"`
}

// Database says which servers a project can use and which one is offered first.
type Database struct {
	Kinds    []string `json:"kinds"`    // mariadb | mysql | postgres
	Default  string   `json:"default"`  // one of Kinds
	Optional bool     `json:"optional"` // the project works without one (the form offers "none")
}

// Step is one action of the generator.
type Step struct {
	Type  string `json:"type"`
	Label string `json:"label"` // shown while it runs: "Creating the Laravel project"

	// run
	Program string   `json:"program,omitempty"` // composer | npm | npx | python | go | java ... (the runtime's own tools)
	Args    []string `json:"args,omitempty"`
	// In is where the program runs: "parent" (the folder that will hold the project, the default
	// for generators that create their own folder) or "project" (the project folder, created first).
	In string `json:"in,omitempty"`
	// Timeout in seconds; 0 is 15 minutes.
	Timeout int `json:"timeout,omitempty"`
	// Env are extra environment variables of the program (CI=1, npm_config_yes=true ...), on top
	// of the runtime's environment. Values may use placeholders.
	Env map[string]string `json:"env,omitempty"`

	// download
	URL      string `json:"url,omitempty"`
	Checksum string `json:"checksum,omitempty"` // "sha256:<hex>" or "sha1:<hex>"; or a https URL of a file holding it
	Strip    int    `json:"strip,omitempty"`    // leading folders to strip when unpacking
	// Unverified: the publisher gives no checksum (start.spring.io). Reason says why and is shown
	// in the plan as a warning; a download step needs a Checksum, or Unverified with a Reason.
	Unverified bool   `json:"unverified,omitempty"`
	Reason     string `json:"reason,omitempty"`

	// files: path relative to the project folder -> content (placeholders allowed)
	Files map[string]string `json:"files,omitempty"`
}

// ServiceTpl is a service New project creates.
type ServiceTpl struct {
	Name     string            `json:"name"` // "{name}"
	Kind     string            `json:"kind"`
	Settings map[string]string `json:"settings,omitempty"`
	Command  string            `json:"command,omitempty"` // custom kind
	Args     []string          `json:"args,omitempty"`
	Cwd      string            `json:"cwd,omitempty"` // relative to the project folder; "" is the folder itself
	Port     int               `json:"port,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	// UsesDatabase links the service to the project's database service (it starts after it and
	// gets its connection variables).
	UsesDatabase bool `json:"usesDatabase,omitempty"`
}

// SiteTpl is the local site New project creates.
type SiteTpl struct {
	Type  string `json:"type"`           // proxy | php | static
	Root  string `json:"root,omitempty"` // php, static: relative to the project folder ("public")
	HTTPS bool   `json:"https"`
}

// EnvLine is a line written to the .env file.
type EnvLine struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	// IfMissing: only when the file has no such key yet (the generator's own file wins).
	IfMissing bool `json:"ifMissing,omitempty"`
}

// File is the shape of frameworks.json.
type File struct {
	Format     int         `json:"format"` // 1
	Frameworks []Framework `json:"frameworks"`
}

// ErrInvalid is wrapped by Parse errors.
var ErrInvalid = errors.New("invalid framework list")

var idRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// Parse reads and checks a frameworks list: every id is unique and well formed, languages, runtime
// kinds, step types and database kinds are known, a run step names a program, a download step has a
// https URL and a checksum, and a framework with services has at least one.
func Parse(raw []byte) ([]Framework, error) {
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if f.Format != 1 {
		return nil, fmt.Errorf("%w: format must be 1", ErrInvalid)
	}
	seen := map[string]bool{}
	for i := range f.Frameworks {
		fw := &f.Frameworks[i]
		if err := validate(fw); err != nil {
			return nil, fmt.Errorf("%w: %q: %v", ErrInvalid, fw.ID, err)
		}
		if seen[fw.ID] {
			return nil, fmt.Errorf("%w: id %q is listed twice", ErrInvalid, fw.ID)
		}
		seen[fw.ID] = true
	}
	return f.Frameworks, nil
}

func validate(fw *Framework) error {
	if !idRE.MatchString(fw.ID) {
		return errors.New("id must be lower case letters, digits and -")
	}
	if strings.TrimSpace(fw.Label) == "" {
		return errors.New("label is required")
	}
	if !model.In(fw.Language, Languages) {
		return fmt.Errorf("language must be one of %s", strings.Join(Languages, ", "))
	}
	if k := fw.Runtime.Kind; k != "" && !model.In(k, model.RuntimeKinds) {
		return fmt.Errorf("runtime kind %q is unknown", k)
	}
	if fw.Composer && fw.Runtime.Kind != "php" {
		return errors.New("composer needs the php runtime")
	}
	if d := fw.Database; d != nil {
		if len(d.Kinds) == 0 || !model.In(d.Default, d.Kinds) {
			return errors.New("database needs kinds and a default that is one of them")
		}
		for _, k := range d.Kinds {
			if k != "mariadb" && k != "mysql" && k != "postgres" {
				return fmt.Errorf("database kind %q must be mariadb, mysql or postgres", k)
			}
		}
	}
	if len(fw.Steps) == 0 {
		return errors.New("at least one step is required")
	}
	for i, s := range fw.Steps {
		switch s.Type {
		case StepRun:
			if err := checkProgram(s.Program); err != nil {
				return fmt.Errorf("step %d: %v", i+1, err)
			}
			if s.In != "" && s.In != "parent" && s.In != "project" {
				return fmt.Errorf("step %d: in must be parent or project", i+1)
			}
			if s.Timeout < 0 || s.Timeout > 3600 {
				return fmt.Errorf("step %d: timeout must be 0-3600 seconds", i+1)
			}
			for k := range s.Env {
				if k == "" || strings.ContainsAny(k, "=\x00") {
					return fmt.Errorf("step %d: env name %q is not valid", i+1, k)
				}
			}
		case StepDownload:
			if !strings.HasPrefix(s.URL, "https://") {
				return fmt.Errorf("step %d: a download step needs a https url", i+1)
			}
			switch {
			case s.Unverified && strings.TrimSpace(s.Reason) == "":
				return fmt.Errorf("step %d: an unverified download needs a reason", i+1)
			case !s.Unverified && s.Checksum == "":
				return fmt.Errorf("step %d: a download step needs a checksum (or unverified with a reason)", i+1)
			case s.Unverified && s.Checksum != "":
				return fmt.Errorf("step %d: a download is either verified or unverified", i+1)
			case s.Checksum != "" && !validChecksum(s.Checksum) && !PlaceholderRE.MatchString(s.Checksum):
				return fmt.Errorf("step %d: checksum must be sha256:<hex>, sha1:<hex> or a https URL", i+1)
			case s.Strip < 0 || s.Strip > 8:
				return fmt.Errorf("step %d: strip must be 0-8", i+1)
			}
		case StepFiles:
			if len(s.Files) == 0 {
				return fmt.Errorf("step %d: a files step needs files", i+1)
			}
			for p := range s.Files {
				if filepath.IsAbs(p) || strings.Contains(p, "..") || strings.ContainsAny(p, `:\`) {
					return fmt.Errorf("step %d: file path %q must be relative, with / as separator", i+1, p)
				}
			}
		default:
			return fmt.Errorf("step %d: type must be run, download or files", i+1)
		}
	}
	if len(fw.Services) == 0 && fw.Site == nil {
		return errors.New("a service or a site is required")
	}
	for _, sv := range fw.Services {
		if !model.In(sv.Kind, model.ServiceKinds) {
			return fmt.Errorf("service kind %q is unknown", sv.Kind)
		}
		if sv.UsesDatabase && fw.Database == nil {
			return errors.New("a service uses the database but the framework has none")
		}
	}
	if s := fw.Site; s != nil {
		if s.Type != "proxy" && s.Type != "php" && s.Type != "static" {
			return errors.New("site type must be proxy, php or static")
		}
		if !relative(s.Root) {
			return fmt.Errorf("site root %q must be relative", s.Root)
		}
		if s.Type == "php" && !slices.ContainsFunc(fw.Services, func(t ServiceTpl) bool { return t.Kind == "php" }) {
			return errors.New("a php site needs a php service")
		}
		if s.Type != "static" && !slices.ContainsFunc(fw.Services, func(t ServiceTpl) bool { return t.Port > 0 }) {
			return errors.New("a proxy or php site needs a service with a port")
		}
	}
	for _, sv := range fw.Services {
		if !relative(sv.Cwd) {
			return fmt.Errorf("service cwd %q must be relative", sv.Cwd)
		}
	}
	for _, e := range fw.Env {
		if e.Key == "" || strings.ContainsAny(e.Key, "= \x00") {
			return fmt.Errorf("env key %q is not valid", e.Key)
		}
	}
	if !relative(fw.EnvFile) {
		return fmt.Errorf("envFile %q must be relative", fw.EnvFile)
	}
	return checkPlaceholders(fw)
}

// checkProgram: a run step names a program as a plain name, or as ./name for a file in the
// folder the step runs in (a project's own wrapper such as ./mvnw.cmd).
func checkProgram(p string) error {
	switch {
	case p == "":
		return errors.New("a run step needs a program")
	case strings.ContainsAny(p, "\x00\r\n \\") || strings.Contains(p, ".."):
		return fmt.Errorf("program %q is not valid", p)
	}
	if rest, ok := strings.CutPrefix(p, "./"); ok {
		if rest == "" || strings.Contains(rest, "/") {
			return fmt.Errorf("program %q must be ./name", p)
		}
	} else if strings.Contains(p, "/") {
		return fmt.Errorf("program %q must be a name or ./name", p)
	}
	return nil
}

func relative(p string) bool {
	return !filepath.IsAbs(p) && !strings.HasPrefix(p, "/") && !strings.ContainsAny(p, ":\\\x00") && !slices.Contains(strings.Split(p, "/"), "..")
}

var hexRE = regexp.MustCompile(`^[0-9a-fA-F]+$`)

func validChecksum(c string) bool {
	if strings.HasPrefix(c, "https://") {
		return true
	}
	algo, v, ok := strings.Cut(c, ":")
	return ok && ((algo == "sha256" && len(v) == 64) || (algo == "sha1" && len(v) == 40)) && hexRE.MatchString(v)
}

// Placeholders are the names that may be used between braces in a framework's strings.
var Placeholders = []string{"name", "slug", "dir", "parent", "port", "domain", "db", "dbUser", "dbPassword",
	"dbHost", "dbPort", "dbUrl", "appKey", "random", "venv", "home"}

// PlaceholderRE finds {name} references.
var PlaceholderRE = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9]*)\}`)

// DatabasePlaceholders are the placeholders that need the project's database.
var DatabasePlaceholders = []string{"db", "dbUser", "dbPassword", "dbHost", "dbPort", "dbUrl"}

// Strings calls fn with every string of the framework that may hold placeholders.
func (fw *Framework) Strings(fn func(where, s string)) {
	for i, st := range fw.Steps {
		w := fmt.Sprintf("step %d", i+1)
		fn(w+" label", st.Label)
		fn(w+" url", st.URL)
		fn(w+" checksum", st.Checksum)
		for _, a := range st.Args {
			fn(w+" args", a)
		}
		for k, v := range st.Env {
			fn(w+" env "+k, v)
		}
		for p, c := range st.Files {
			fn(w+" file "+p, c)
		}
	}
	for _, sv := range fw.Services {
		fn("service name", sv.Name)
		fn("service command", sv.Command)
		fn("service cwd", sv.Cwd)
		for _, a := range sv.Args {
			fn("service args", a)
		}
		for k, v := range sv.Settings {
			fn("service setting "+k, v)
		}
		for k, v := range sv.Env {
			fn("service env "+k, v)
		}
	}
	for _, e := range fw.Env {
		fn("env "+e.Key, e.Value)
	}
}

func checkPlaceholders(fw *Framework) error {
	var err error
	fw.Strings(func(where, s string) {
		for _, m := range PlaceholderRE.FindAllStringSubmatch(s, -1) {
			switch {
			case !slices.Contains(Placeholders, m[1]):
				err = errors.Join(err, fmt.Errorf("%s: unknown placeholder {%s}", where, m[1]))
			case m[1] == "venv" && fw.Runtime.Kind != "python":
				err = errors.Join(err, fmt.Errorf("%s: {venv} is only for python frameworks", where))
			case slices.Contains(DatabasePlaceholders, m[1]) && fw.Database == nil:
				err = errors.Join(err, fmt.Errorf("%s: {%s} needs a database", where, m[1]))
			}
		}
	})
	return err
}

// UsesDatabase reports whether s mentions a database placeholder.
func UsesDatabase(s string) bool {
	for _, m := range PlaceholderRE.FindAllStringSubmatch(s, -1) {
		if slices.Contains(DatabasePlaceholders, m[1]) {
			return true
		}
	}
	return false
}

// Load returns the list: <home>\frameworks.json when it exists (a damaged file is an error, not
// ignored), else the one shipped with Zero2Dev.
func Load(home string) ([]Framework, error) {
	if home != "" {
		if raw, err := os.ReadFile(filepath.Join(home, "frameworks.json")); err == nil {
			return Parse(raw)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return Parse(builtin)
}

// Get finds a framework by id.
func Get(list []Framework, id string) (Framework, bool) {
	for _, f := range list {
		if f.ID == id {
			return f, true
		}
	}
	return Framework{}, false
}
