// Package model holds the JSON data types shared by the daemon packages.
// Field names and tags follow docs/API.md exactly.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NewID returns prefix + 8 random hex chars (e.g. svc_a1b2c3d4).
func NewID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the OS RNG failing is unrecoverable
	}
	return prefix + hex.EncodeToString(b)
}

// Service states.
const (
	StateStopped  = "stopped"
	StateStarting = "starting"
	StateRunning  = "running"
	StateStopping = "stopping"
	StateFailed   = "failed"
)

// Service is a managed process definition plus its runtime status.
// State, Pid, StartedAt and LastError are filled in by the supervisor.
type Service struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Settings   map[string]string `json:"settings"`   // per-kind settings (internal/kinds); missing keys use the defaults
	CustomArgs bool              `json:"customArgs"` // Args are the whole argument list instead of extra arguments
	Cwd        string            `json:"cwd"`
	Env        map[string]string `json:"env"`
	Port       int               `json:"port"`
	HealthPath string            `json:"healthPath"`
	RuntimeID  string            `json:"runtimeId"`
	Autostart  bool              `json:"autostart"`
	// DependsOn are the services that must be running before this one starts
	// (and stop after it). DatabaseID is a database service this one uses; it is
	// an implicit dependency too (see Deps).
	DependsOn  []string `json:"dependsOn"`
	DatabaseID string   `json:"databaseId"`
	// EnvFile is a .env file, relative to Cwd, whose variables are added to the
	// service's environment (any kind).
	EnvFile string `json:"envFile"`
	// RestartOnCrash restarts the service after it exits unexpectedly, at most
	// RestartLimit times in a row (1-20, default DefaultRestartLimit).
	RestartOnCrash bool `json:"restartOnCrash"`
	RestartLimit   int  `json:"restartLimit"`
	// AdminPassword is the password of a database service's administrator
	// (root, postgres, or Redis' requirepass), "" for none. It is not part of
	// ServiceInput: only PUT /api/services/{id}/database/password changes it
	// (and a reset clears it), so it always matches what the data folder holds.
	AdminPassword string `json:"adminPassword"`
	// Restarts is how many crash restarts happened since the last manual start
	// (filled in by the supervisor, never stored or accepted as input).
	Restarts  int        `json:"restarts"`
	State     string     `json:"state"`
	Pid       int        `json:"pid"`
	StartedAt *time.Time `json:"startedAt"`
	LastError string     `json:"lastError"`
	// CPU and MemBytes are the last sample of a running service, summed over its whole process
	// tree (0.0.6, internal/supervisor sampler): CPU is the percent of the whole machine (0-100),
	// MemBytes the working set. Both are 0 while the service is stopped or before the first sample;
	// never stored or accepted as input.
	CPU      float64 `json:"cpu"`
	MemBytes uint64  `json:"memBytes"`
}

// Usage is one entry of the service.usage event (events carry a batch of them).
type Usage struct {
	ID       string  `json:"id"`
	CPU      float64 `json:"cpu"`
	MemBytes uint64  `json:"memBytes"`
}

// Restart-on-crash limits.
const (
	DefaultRestartLimit = 5
	MaxRestartLimit     = 20
)

// Deps returns the ids of the services sv needs running: DependsOn plus
// DatabaseID, without duplicates, in that order.
func Deps(sv Service) []string {
	out := make([]string, 0, len(sv.DependsOn)+1)
	seen := map[string]bool{}
	for _, id := range append(append([]string{}, sv.DependsOn...), sv.DatabaseID) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// GoPath is Zero2Dev's GOPATH: <home>/go/path. A Go service that keeps Go's
// downloads in the data folder (the default setting) uses it, so the Go tools
// installed with `go install` (air, dlv, gopls) and the module cache live there
// and move with the data folder.
func GoPath(home string) string { return filepath.Join(home, "go", "path") }

// GoToolsDir is where `go install` puts programs under Zero2Dev's GOPATH: <GOPATH>/bin.
func GoToolsDir(home string) string { return filepath.Join(GoPath(home), "bin") }

// DefaultGoBin is where `go install` puts programs when GOPATH is not Zero2Dev's:
// the bin folder of the first GOPATH entry (gopath, the GOPATH variable) or, when
// it is empty, Go's own default %USERPROFILE%\go (userHome). "" when neither is known.
func DefaultGoBin(gopath, userHome string) string {
	first := strings.TrimSpace(strings.SplitN(gopath, string(os.PathListSeparator), 2)[0])
	switch {
	case first != "":
		return filepath.Join(first, "bin")
	case userHome != "":
		return filepath.Join(userHome, "go", "bin")
	}
	return ""
}

// RuntimeHome is the environment variable that points at a runtime's install
// folder (JAVA_HOME for java, GOROOT for go) and its value: the folder two
// levels above the binary (<home>/bin/java -> <home>). name is "" for every
// other kind.
func RuntimeHome(rt Runtime) (name, dir string) {
	switch rt.Kind {
	case "java":
		name = "JAVA_HOME"
	case "go":
		name = "GOROOT"
	default:
		return "", ""
	}
	if rt.Binary == "" {
		return "", ""
	}
	return name, filepath.Dir(filepath.Dir(rt.Binary))
}

// ServiceInput is the body of create/update.
type ServiceInput struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Settings   map[string]string `json:"settings"` // absent (null) with args: a pre-settings client, args are the whole list
	CustomArgs bool              `json:"customArgs"`
	Cwd        string            `json:"cwd"`
	Env        map[string]string `json:"env"`
	Port       int               `json:"port"`
	HealthPath string            `json:"healthPath"`
	RuntimeID  string            `json:"runtimeId"`
	Autostart  bool              `json:"autostart"`
	// See Service; RestartLimit 0 means the default.
	DependsOn      []string `json:"dependsOn"`
	DatabaseID     string   `json:"databaseId"`
	EnvFile        string   `json:"envFile"`
	RestartOnCrash bool     `json:"restartOnCrash"`
	RestartLimit   int      `json:"restartLimit"`
}

// Project groups services and sites that are started and stopped together.
type Project struct {
	ID         string   `json:"id"` // prj_xxxxxxxx
	Name       string   `json:"name"`
	ServiceIDs []string `json:"serviceIds"`
	SiteIDs    []string `json:"siteIds"`
}

// ProjectInput is the body of project create/update.
type ProjectInput struct {
	Name       string   `json:"name"`
	ServiceIDs []string `json:"serviceIds"`
	SiteIDs    []string `json:"siteIds"`
}

// Runtime is an installed or registered language runtime / tool.
type Runtime struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Version     string    `json:"version"`
	Name        string    `json:"name"`
	Binary      string    `json:"binary"`
	Source      string    `json:"source"`
	CatalogID   string    `json:"catalogId,omitempty"` // catalog entry an installed runtime came from
	Default     bool      `json:"default"`             // the runtime services of this kind use when they name none
	InstalledAt time.Time `json:"installedAt"`
}

// Checksum describes how an archive is verified.
type Checksum struct {
	Type  string `json:"type"` // sha256 | shasums | none (only with Unverified entries)
	Value string `json:"value,omitempty"`
	URL   string `json:"url,omitempty"`
}

// CatalogEntry is a downloadable runtime.
type CatalogEntry struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Version         string   `json:"version"`
	Name            string   `json:"name"`
	URL             string   `json:"url"`
	Checksum        Checksum `json:"checksum"`
	BinaryRelPath   string   `json:"binaryRelPath"`
	StripComponents int      `json:"stripComponents"`
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
	Homepage        string   `json:"homepage"`
	FileName        string   `json:"fileName"`
	Installed       bool     `json:"installed"`
	Supported       bool     `json:"supported"`
	// Line is the version line ("8.4", "21", "1.26"); Channel is lts|stable|mainline|"".
	Line       string `json:"line"`
	Channel    string `json:"channel"`
	Source     string `json:"source"`     // builtin | remote
	Latest     bool   `json:"latest"`     // newest of its kind/os/arch/line
	Unverified bool   `json:"unverified"` // the publisher has no checksum (nginx)
}

// Site maps a local domain to a folder or an upstream.
type Site struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	Type         string `json:"type"`
	Root         string `json:"root"`
	ServiceID    string `json:"serviceId"`
	UpstreamPort int    `json:"upstreamPort"`
	HTTPS        bool   `json:"https"`
	Enabled      bool   `json:"enabled"`
}

// SiteView is the API response shape (Site plus computed url).
type SiteView struct {
	Site
	URL string `json:"url"`
}

// SiteInput is the body of site create/update. Enabled defaults to true.
type SiteInput struct {
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	Type         string `json:"type"`
	Root         string `json:"root"`
	ServiceID    string `json:"serviceId"`
	UpstreamPort int    `json:"upstreamPort"`
	HTTPS        bool   `json:"https"`
	Enabled      *bool  `json:"enabled"`
}

// Job is a long running operation: a runtime install (internal/installer) or,
// since 0.0.5, a database import or export, a snapshot, a restore, a clone or a
// migration (internal/jobs). The fields after RuntimeID are set only by the
// jobs that use them.
type Job struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	State     string `json:"state"` // running | done | failed
	Percent   int    `json:"percent"`
	Step      string `json:"step"`
	Error     string `json:"error"`
	RuntimeID string `json:"runtimeId"`
	// ServiceID is the database service of a db-import / db-export, or the
	// service whose snapshot is taken or restored.
	ServiceID string `json:"serviceId,omitempty"`
	// ProjectID is the project of a project snapshot or restore, and the new
	// project of a snapshot-clone (known once the job is done).
	ProjectID   string `json:"projectId,omitempty"`
	SnapshotID  string `json:"snapshotId,omitempty"`
	MigrationID string `json:"migrationId,omitempty"`
	// Path is the file a db-export wrote (set when it is done).
	Path string `json:"path,omitempty"`
}

// Job types of internal/jobs (the installer's are "install", "import" and "xdebug").
const (
	JobDBImport        = "db-import"
	JobDBExport        = "db-export"
	JobSnapshot        = "snapshot"
	JobSnapshotRestore = "snapshot-restore"
	JobSnapshotClone   = "snapshot-clone"
	JobMigrate         = "migrate"
	JobMigrateRevert   = "migrate-revert"
	JobMigrateCleanup  = "migrate-cleanup"
	JobTask            = "task"           // 0.0.6: a one-off task of a service (npm install ...)
	JobNewProject      = "new-project"    // 0.0.6: New project from a framework
	JobProjectImport   = "project-import" // 0.0.6: Import project from a project file
)

// Portable says which user-level caches and global package folders of the
// runtimes Zero2Dev keeps in its data folder instead of the user profile. All
// off by default; a migration (internal/migrate) switches one on and its
// revert switches it off again.
type Portable struct {
	NpmGlobal bool `json:"npmGlobal"` // npm_config_prefix = <home>/node/global
	Composer  bool `json:"composer"`  // COMPOSER_HOME = <home>/composer
	GoCache   bool `json:"goCache"`   // GOMODCACHE / GOCACHE in <home>/go even when isolate is off
}

// Settings are user-editable daemon settings.
type Settings struct {
	APIPort            int    `json:"apiPort"`
	HomeDir            string `json:"homeDir"`
	DomainSuffix       string `json:"domainSuffix"`
	ProxyHTTPPort      int    `json:"proxyHttpPort"`
	ProxyHTTPSPort     int    `json:"proxyHttpsPort"`
	OpenBrowserOnStart bool   `json:"openBrowserOnStart"`
	TrustLocalCA       bool   `json:"trustLocalCA"`
	DownloadProxy      string `json:"downloadProxy"`
	SiteServer         string `json:"siteServer"` // caddy (default) | nginx: which program serves the sites
	Theme              string `json:"theme"`      // system (default) | light | dark: the dashboard's colour scheme
	Accent             string `json:"accent"`     // #rrggbb: the dashboard's accent colour (default #1677ff)
	// Portable is changed by migrations only (ignored on PUT /api/settings).
	Portable Portable `json:"portable"`
}

// EnvConfig is what the user chose to put in their Windows user environment (the one
// opt-in registry change; HKCU only, reversible, see internal/envreg). Every item is
// switched on or off on its own and nothing is on by default.
type EnvConfig struct {
	// Seeded is set once the suggested items were added, so that items the user
	// deleted do not come back.
	Seeded bool      `json:"seeded"`
	Path   []EnvPath `json:"path"`
	Vars   []EnvVar  `json:"vars"`
}

// Sources of an EnvPath and an EnvVar.
const (
	EnvShims   = "shims"   // EnvPath: Zero2Dev's shims folder
	EnvDefault = "default" // EnvVar: the default runtime of Kind
	EnvRuntime = "runtime" // the folder of one runtime (one specific version)
	EnvCustom  = "custom"  // EnvPath: Dir, EnvVar: Value
)

// EnvPath is one folder on the user PATH.
type EnvPath struct {
	ID        string `json:"id"`
	Source    string `json:"source"`              // shims | runtime | custom
	RuntimeID string `json:"runtimeId,omitempty"` // runtime: the folder of its program
	Dir       string `json:"dir,omitempty"`       // custom
	Enabled   bool   `json:"enabled"`
}

// EnvVar is one user environment variable. Zero2Dev only sets it when the user has no
// variable of that name.
type EnvVar struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Source    string `json:"source"`              // default | runtime | custom
	Kind      string `json:"kind,omitempty"`      // default: the runtime kind whose default is used
	RuntimeID string `json:"runtimeId,omitempty"` // runtime: one specific version
	Value     string `json:"value,omitempty"`     // custom
	Enabled   bool   `json:"enabled"`
}

// Appearance defaults and accepted values.
const (
	DefaultTheme  = "system"
	DefaultAccent = "#1677ff"
)

// Themes are the accepted Settings.Theme values.
var Themes = []string{"system", "light", "dark"}

// LogLine is one captured output line.
type LogLine struct {
	Ts     string `json:"ts"`
	Stream string `json:"stream"` // stdout | stderr | system
	Text   string `json:"text"`
}

// ServiceKinds and RuntimeKinds are the accepted enums.
var (
	ServiceKinds = []string{"custom", "node", "php", "go", "java", "python", "rust", "caddy", "nginx", "mariadb", "mysql", "postgres", "redis", "mailpit"}
	RuntimeKinds = []string{"node", "php", "go", "java", "python", "rust", "composer", "viteplus", "caddy", "nginx",
		"mariadb", "mysql", "postgres", "redis", "mailpit",
		"vscode", "vscodium", "notepadpp",
		"heidisql", "dbeaver", "bruno", "hoppscotch", "other"}
	// RemoteKinds can have their version lists fetched from the publishers.
	RemoteKinds = []string{"node", "go", "caddy", "php", "java", "python", "rust", "composer", "viteplus", "nginx",
		"mariadb", "mysql", "postgres", "redis", "mailpit", "vscode", "vscodium", "notepadpp", "heidisql", "dbeaver", "bruno", "hoppscotch"}
)

// Kind categories, as grouped on the Runtimes page.
const (
	CatLanguage = "language"
	CatServer   = "server"
	CatDatabase = "database"
	CatEditor   = "editor"
	CatDBTool   = "dbtool"
	CatMail     = "mail"
	CatTool     = "tool"      // a program other programs run (Composer, the Vite+ CLI)
	CatAPI      = "apiclient" // a desktop program to try an API (Bruno, Hoppscotch)
)

// KindCategory maps every runtime kind (except "other") to its category.
var KindCategory = map[string]string{
	"node": CatLanguage, "php": CatLanguage, "go": CatLanguage, "java": CatLanguage, "python": CatLanguage, "rust": CatLanguage,
	"composer": CatTool, "viteplus": CatTool,
	"bruno": CatAPI, "hoppscotch": CatAPI,
	"caddy": CatServer, "nginx": CatServer,
	"mariadb": CatDatabase, "mysql": CatDatabase, "postgres": CatDatabase, "redis": CatDatabase,
	"mailpit": CatMail,
	"vscode":  CatEditor, "vscodium": CatEditor, "notepadpp": CatEditor,
	"heidisql": CatDBTool, "dbeaver": CatDBTool,
}

// Launchable kinds are desktop programs that are opened, not supervised as services.
func Launchable(kind string) bool {
	c := KindCategory[kind]
	return c == CatEditor || c == CatDBTool || c == CatAPI
}

// dataDirNames are the per-tool folders inside the data folder. Settings,
// extensions, workspaces and database files live there; the bin folder only
// holds the programs themselves.
var dataDirNames = map[string]string{
	"vscode": "code", "vscodium": "codium", "notepadpp": "notepadpp", "heidisql": "heidisql", "dbeaver": "dbeaver",
	"mariadb": "mariadb", "mysql": "mysql", "postgres": "postgres", "redis": "redis", "mailpit": "mailpit",
	"php": "php", "node": "node", "go": "go", "python": "python", "composer": "composer", "rust": "rust", "viteplus": "vite-plus",
	"bruno": "bruno", "hoppscotch": "hoppscotch",
}

// DataDirName is the folder name of a kind's data inside the data folder
// (e.g. "code" for VS Code), or "" for kinds that keep no data there.
func DataDirName(kind string) string { return dataDirNames[kind] }

// IsDatabase reports whether a service or runtime kind is a database server.
func IsDatabase(kind string) bool { return KindCategory[kind] == CatDatabase }

// In reports whether s is in list.
func In(s string, list []string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
