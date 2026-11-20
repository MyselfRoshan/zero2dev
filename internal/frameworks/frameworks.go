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

	// download
	URL      string `json:"url,omitempty"`
	Checksum string `json:"checksum,omitempty"` // "sha256:<hex>" or "sha1:<hex>"; or a https URL of a file holding it
	Strip    int    `json:"strip,omitempty"`    // leading folders to strip when unpacking

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
			if s.Program == "" {
				return fmt.Errorf("step %d: a run step needs a program", i+1)
			}
		case StepDownload:
			if !strings.HasPrefix(s.URL, "https://") || s.Checksum == "" {
				return fmt.Errorf("step %d: a download step needs a https url and a checksum", i+1)
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
	if s := fw.Site; s != nil && s.Type != "proxy" && s.Type != "php" && s.Type != "static" {
		return errors.New("site type must be proxy, php or static")
	}
	return nil
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
