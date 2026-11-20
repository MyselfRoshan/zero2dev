package migrate

import (
	"os"
	"path/filepath"
	"strings"

	"zero2dev/internal/model"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/store"
)

// plan is a source with what Start needs to bring it in.
type plan struct {
	src      Source
	items    []planItem // parallel to src.Items
	portable string     // the Settings.Portable flag the migration switches on
	image    string     // the program's process image (Close ... first)
	runtime  *runtimePlan
}

// planItem is one item and how it is handled.
type planItem struct {
	Item
	rewrite bool     // VS Code's extensions\extensions.json holds absolute paths
	regPath string   // registry: the key below HKEY_CURRENT_USER
	skip    []string // runtime: top-level names left out of the copy
}

// runtimePlan is a registered runtime and where it is installed.
type runtimePlan struct {
	rt     model.Runtime
	root   string // the installation folder (symlinks resolved)
	binary string // the binary, symlinks resolved, inside root
}

// program describes one fixed source.
type program struct {
	id, label string
	image     string // process image name, "" for none
	needs     string // a runtime kind Zero2Dev must have installed ("" for none)
	portable  string
	note      string
	items     func(e Env, home string) []planItem
}

// under joins base and parts, or "" when base is unknown.
func under(base string, parts ...string) string {
	if base == "" {
		return ""
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

const (
	heidiKey   = `Software\HeidiSQL`
	settingsNo = "Logins kept by Windows (Settings Sync) are not moved."
)

var programs = []program{
	{
		id: ProgVSCode, label: "VS Code", image: "Code.exe", needs: "vscode", note: settingsNo,
		items: func(e Env, home string) []planItem {
			return []planItem{
				{Item: Item{Label: "Settings", Kind: KindDir, From: under(e.AppData, "Code", "User"), To: filepath.Join(home, "code", "user-data", "User")}},
				{Item: Item{Label: "Extensions", Kind: KindDir, From: under(e.UserProfile, ".vscode", "extensions"), To: filepath.Join(home, "code", "extensions")}, rewrite: true},
			}
		},
	},
	{
		id: ProgVSCodium, label: "VSCodium", image: "VSCodium.exe", needs: "vscodium", note: settingsNo,
		items: func(e Env, home string) []planItem {
			return []planItem{
				{Item: Item{Label: "Settings", Kind: KindDir, From: under(e.AppData, "VSCodium", "User"), To: filepath.Join(home, "codium", "user-data", "User")}},
				{Item: Item{Label: "Extensions", Kind: KindDir, From: under(e.UserProfile, ".vscode-oss", "extensions"), To: filepath.Join(home, "codium", "extensions")}, rewrite: true},
			}
		},
	},
	{
		id: ProgNotepadPP, label: "Notepad++", image: "notepad++.exe", needs: "notepadpp",
		note: "The settings, shortcuts, themes and plugin settings. Plugins installed in Notepad++'s program folder are not part of it.",
		items: func(e Env, home string) []planItem {
			return []planItem{{Item: Item{Label: "Settings", Kind: KindDir, From: under(e.AppData, "Notepad++"), To: filepath.Join(home, "notepadpp")}}}
		},
	},
	{
		id: ProgHeidiSQL, label: "HeidiSQL", image: "heidisql.exe", needs: "heidisql",
		note: "Read from the registry and written as HeidiSQL's portable_settings.txt. Move also removes the registry key, after saving a backup of it.",
		items: func(e Env, home string) []planItem {
			return []planItem{{Item: Item{Label: "Sessions and settings", Kind: KindRegistry, From: `HKEY_CURRENT_USER\` + heidiKey,
				To: filepath.Join(home, "heidisql", "portable_settings.txt")}, regPath: heidiKey}}
		},
	},
	{
		id: ProgDBeaver, label: "DBeaver", image: "dbeaver.exe", needs: "dbeaver",
		note: "The workspace: connections, scripts and settings. Drivers are downloaded again; passwords kept in Windows' secure storage are not moved.",
		items: func(e Env, home string) []planItem {
			return []planItem{{Item: Item{Label: "Workspace", Kind: KindDir, From: under(e.AppData, "DBeaverData", "workspace6"), To: filepath.Join(home, "dbeaver", "workspace")}}}
		},
	},
	{
		id: ProgNpmGlobal, label: "npm global packages", portable: portNpmGlobal,
		note: "Zero2Dev's Node.js services and terminals then install and find global packages (npm install -g) there.",
		items: func(e Env, home string) []planItem {
			return []planItem{{Item: Item{Label: "Global packages", Kind: KindDir, From: under(e.AppData, "npm"), To: filepath.Join(home, "node", "global")}}}
		},
	},
	{
		id: ProgNpmCache, label: "npm cache",
		note: "Zero2Dev's npm already keeps its download cache in the data folder; this brings in what was downloaded before.",
		items: func(e Env, home string) []planItem {
			return []planItem{{Item: Item{Label: "Download cache", Kind: KindDir, From: under(e.LocalAppData, "npm-cache"), To: filepath.Join(home, "node", "npm-cache")}}}
		},
	},
	{
		id: ProgGoCache, label: "Go caches", portable: portGoCache,
		note: "Zero2Dev's Go services and terminals then keep modules and the build cache in the data folder.",
		items: func(e Env, home string) []planItem {
			return []planItem{
				{Item: Item{Label: "Module cache", Kind: KindDir, From: under(e.UserProfile, "go", "pkg", "mod"), To: filepath.Join(home, "go", "path", "pkg", "mod")}},
				{Item: Item{Label: "Build cache", Kind: KindDir, From: under(e.LocalAppData, "go-build"), To: filepath.Join(home, "go", "cache")}},
			}
		},
	},
	{
		id: ProgComposer, label: "Composer", portable: portComposer,
		note: "Zero2Dev's PHP services and terminals then use it as COMPOSER_HOME (global packages, settings and auth.json).",
		items: func(e Env, home string) []planItem {
			return []planItem{{Item: Item{Label: "Composer home", Kind: KindDir, From: under(e.AppData, "Composer"), To: filepath.Join(home, "composer")}}}
		},
	},
}

// Why the copy-only sources cannot be moved.
const (
	runtimeCopyOnly  = "A runtime's files belong to the program that installed it: moving them would break its uninstaller and needs administrator rights."
	databaseCopyOnly = "Zero2Dev does not remove databases from a server it does not own."
)

// plans lists every source with its plan.
func (m *Manager) plans() []*plan {
	var d store.Data
	if m.St != nil {
		d = m.St.Snapshot()
	}
	e := m.env()
	active := m.activeSources()
	var out []*plan
	for _, pg := range programs {
		out = append(out, m.programPlan(pg, e, d, active))
	}
	for _, rt := range d.Runtimes {
		if p := m.runtimePlan(rt, d, active); p != nil {
			out = append(out, p)
		}
	}
	out = append(out, m.databasePlan(d))
	freeHome, freeBin := m.free(m.Home), m.free(m.Bin)
	for _, p := range out {
		p.src.TargetFreeBytes = freeHome
		if p.src.Program == ProgRuntime {
			p.src.TargetFreeBytes = freeBin
		}
	}
	return out
}

// planFor finds the plan of a source id.
func (m *Manager) planFor(id string) *plan {
	for _, p := range m.plans() {
		if p.src.ID == id {
			return p
		}
	}
	return nil
}

// activeSources maps a source id to the migration of it that is done and not
// reverted (bringing the same thing in twice would mix up the backups).
func (m *Manager) activeSources() map[string]string {
	out := map[string]string{}
	recs, _ := m.loadAll()
	for _, r := range recs {
		if r.State == StateDone || (r.State == StateRunning && m.isActive(r.ID)) {
			if _, ok := out[r.Source]; !ok {
				out[r.Source] = r.ID
			}
		}
	}
	return out
}

func (m *Manager) programPlan(pg program, e Env, d store.Data, active map[string]string) *plan {
	p := &plan{portable: pg.portable, image: pg.image}
	p.src = Source{ID: pg.id, Program: pg.id, Label: pg.label, Modes: []string{ModeCopy, ModeMove}, Blockers: []string{}, Note: pg.note}
	reg := m.reg()
	for _, it := range pg.items(e, m.Home) {
		switch it.Kind {
		case KindRegistry:
			vals, ok, err := reg.ReadTree(it.regPath)
			if err == nil && ok {
				it.Exists = true
				data, _ := encodePortable(vals)
				it.SizeBytes = int64(len(data))
			}
		default:
			if it.From != "" {
				if _, err := os.Stat(it.From); err == nil {
					it.Exists = true
					it.SizeBytes, _ = treeSize(it.From, nil)
				}
			}
		}
		if it.Exists {
			p.src.Found = true
		}
		if hasContent(it.To) {
			p.src.TargetHas = true
		}
		p.items = append(p.items, it)
		p.src.Items = append(p.src.Items, it.Item)
	}
	if pg.needs != "" && !installed(d, pg.needs) {
		p.src.Blockers = append(p.src.Blockers, "Install "+pg.label+" in Zero2Dev first")
	}
	if m.running(pg.image) {
		p.src.Blockers = append(p.src.Blockers, "Close "+pg.label+" first")
	}
	if id := active[pg.id]; id != "" {
		p.src.Blockers = append(p.src.Blockers, pg.label+" was already imported ("+id+"); revert that first")
	}
	return p
}

// runtimePlan is the source of one registered runtime, or nil when the
// runtime is not one (installed by Zero2Dev, or inside its bin folder).
func (m *Manager) runtimePlan(rt model.Runtime, d store.Data, active map[string]string) *plan {
	if rt.Source != "registered" || rt.Binary == "" || inside(m.Bin, rt.Binary) {
		return nil
	}
	id := ProgRuntime + ":" + rt.ID
	label := rt.Name
	if label == "" {
		label = runtimes.DisplayName(rt.Kind, rt.Version, rt.Binary)
	}
	p := &plan{image: "", src: Source{ID: id, Program: ProgRuntime, Label: label, Modes: []string{ModeCopy}, CopyOnly: runtimeCopyOnly,
		Blockers: []string{}, RuntimeID: rt.ID,
		Note: "The program folder is copied into Zero2Dev's bin folder and the runtime keeps its id, so services using it keep working. The installed program stays where it is."}}
	bin := rt.Binary
	if r, err := filepath.EvalSymlinks(bin); err == nil {
		bin = r
	}
	root := installRoot(rt.Kind, bin)
	if isEditorKind(rt.Kind) {
		p.image = filepath.Base(bin)
	}
	it := planItem{Item: Item{Label: "Program", Kind: KindRuntime, From: root, To: m.adoptDir(rt)}}
	if model.IsDatabase(rt.Kind) {
		it.skip = []string{"data"} // a database server's own data folder; Zero2Dev keeps its own
	}
	st, err := os.Stat(bin)
	switch {
	case err != nil || st.IsDir():
		p.src.Blockers = append(p.src.Blockers, "The program "+rt.Binary+" is missing")
	default:
		it.Exists = true
		p.src.Found = true
		it.SizeBytes, _ = treeSize(root, it.skip)
		if why := rootProblem(root); why != "" {
			p.src.Blockers = append(p.src.Blockers, why)
		}
	}
	if p.image != "" && m.running(p.image) {
		p.src.Blockers = append(p.src.Blockers, "Close "+label+" first")
	}
	if aid := active[id]; aid != "" {
		p.src.Blockers = append(p.src.Blockers, label+" was already adopted ("+aid+"); revert that first")
	}
	p.runtime = &runtimePlan{rt: rt, root: root, binary: bin}
	p.items = []planItem{it}
	p.src.Items = []Item{it.Item}
	return p
}

func isEditorKind(kind string) bool {
	switch kind {
	case "vscode", "vscodium", "notepadpp", "heidisql", "dbeaver":
		return true
	}
	return false
}

// installRoot is the folder a runtime is installed in: the binary's folder,
// or the one above it when that is called bin (Go, Java, MariaDB, PostgreSQL ...).
func installRoot(kind, binary string) string {
	dir := filepath.Dir(binary)
	if strings.EqualFold(filepath.Base(dir), "bin") && filepath.Dir(dir) != dir {
		return filepath.Dir(dir)
	}
	return dir
}

// rootProblem says why a folder is not a program's own folder to copy.
func rootProblem(root string) string {
	if filepath.Dir(root) == root {
		return "Its folder " + root + " is a whole drive; Zero2Dev only copies a program's own folder"
	}
	low := strings.ToLower(filepath.ToSlash(root))
	if win := os.Getenv("SystemRoot"); win != "" && inside(win, root) {
		return "It is in the Windows folder (" + root + "); it cannot be adopted"
	}
	for _, shim := range []string{"/windowsapps", "/chocolatey/bin", "/scoop/shims", "/.cargo/bin", "/winget/links"} {
		if strings.HasSuffix(low, shim) {
			return root + " holds launchers of many programs; register the program from its own folder first"
		}
	}
	return ""
}

// inside reports whether p is dir or below it (case-insensitive, as on Windows).
func inside(dir, p string) bool {
	if dir == "" || p == "" {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(dir)), strings.ToLower(filepath.Clean(p)))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// adoptDir is <bin>\adopted\<kind>-<version>, with -2, -3 ... when taken.
func (m *Manager) adoptDir(rt model.Runtime) string {
	v := safeName(rt.Version)
	if v == "" {
		v = "unknown"
	}
	base := filepath.Join(m.Bin, "adopted", safeName(rt.Kind)+"-"+v)
	p := base
	for n := 2; exists(p); n++ {
		p = base + "-" + itoa(n)
	}
	return p
}

// safeName keeps letters, digits, dot, dash and underscore.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), ".")
}

func (m *Manager) databasePlan(d store.Data) *plan {
	p := &plan{src: Source{ID: ProgDatabase, Program: ProgDatabase, Label: "Databases of another server", Found: true,
		Modes: []string{ModeCopy}, CopyOnly: databaseCopyOnly, Blockers: []string{},
		Note:  "Each database is dumped from the server you name and imported into a Zero2Dev database service. The server keeps its databases.",
		Items: []Item{{Label: "Databases", Kind: KindDatabases}}}}
	p.items = []planItem{{Item: p.src.Items[0]}}
	if m.DB == nil {
		p.src.Blockers = append(p.src.Blockers, "Database tools are not available in this build")
	}
	has := false
	for _, s := range d.Services {
		if s.Kind == "mariadb" || s.Kind == "mysql" || s.Kind == "postgres" {
			has = true
		}
	}
	if !has {
		p.src.Blockers = append(p.src.Blockers, "Add a MariaDB, MySQL or PostgreSQL service in Zero2Dev first")
	}
	return p
}
