// Package detect recognises what kind of project a folder holds (package.json, composer.json,
// go.mod, pom.xml, manage.py ...), so the New service form can fill itself in. It only reads.
//
// Contract: docs/API.md, "Amendment: languages, tasks and new projects (0.0.6)" and its
// "Tasks and detection details" subsection (the rules as a table).
package detect

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/kinds"
)

// ErrNotImplemented is what the API answers 501 for, kept for a server without detection.
var ErrNotImplemented = errors.New("not implemented yet")

// Result is GET /api/project/detect?dir=.
type Result struct {
	// Kind is the service kind that fits ("node", "php", "go", "java", "python"), or "" when the
	// folder says nothing (then nothing else is filled in).
	Kind string `json:"kind"`
	// Framework is the framework id from the shipped list ("laravel", "nextjs", "django" ...) or "".
	Framework string `json:"framework"`
	// Name is a name for the service: the project's own name, else the folder's.
	Name string `json:"name"`
	// Confidence is "high" (a marker file and a framework dependency), "medium" (a marker file) or "".
	Confidence string `json:"confidence"`
	// Settings are values for the kind's options (script, package, start script ...), only the
	// ones that were worked out.
	Settings map[string]string `json:"settings"`
	// EnvFile is the .env file of the project when there is one (".env"), else "".
	EnvFile string `json:"envFile"`
	// Port is the port the framework usually listens on, 0 when unknown.
	Port int `json:"port"`
	// Markers are the files that were looked at and found, for the UI to say why ("package.json").
	Markers []string `json:"markers"`
}

// Dir looks at a folder. A folder that does not exist is a 400.
func Dir(dir string) (Result, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return Result{}, apperr.BadRequest("dir must be an absolute path")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Result{}, apperr.BadRequest("%s is not a folder", dir)
	}
	d := &folder{dir: dir}
	var best *Result
	// The order is the contract's; the strongest evidence wins, the earlier group on a tie.
	for _, probe := range []func(*folder) *Result{probePHP, probeNode, probeGo, probeJava, probePython, probeRust} {
		if r := probe(d); r != nil && (best == nil || rank(r.Confidence) > rank(best.Confidence)) {
			best = r
		}
	}
	if best == nil {
		res := Result{Settings: map[string]string{}, Markers: []string{}}
		if d.has("index.html") {
			res.Markers = []string{"index.html"} // a static site: nothing a service runs
		}
		return res, nil
	}
	if best.Name == "" {
		best.Name = filepath.Base(dir)
	}
	if d.has(".env") && !d.isDir(".env") {
		best.EnvFile = ".env"
	}
	if best.Settings == nil {
		best.Settings = map[string]string{}
	}
	return *best, nil
}

func rank(c string) int {
	switch c {
	case "high":
		return 2
	case "medium":
		return 1
	}
	return 0
}

// folder is the folder being looked at, with small cached reads.
type folder struct {
	dir   string
	cache map[string]string
}

func (f *folder) path(rel string) string { return filepath.Join(f.dir, filepath.FromSlash(rel)) }

func (f *folder) has(rel string) bool {
	_, err := os.Stat(f.path(rel))
	return err == nil
}

func (f *folder) isDir(rel string) bool {
	fi, err := os.Stat(f.path(rel))
	return err == nil && fi.IsDir()
}

// read returns the text of a file (the first 1 MB), "" when it cannot be read.
func (f *folder) read(rel string) string {
	if s, ok := f.cache[rel]; ok {
		return s
	}
	if f.cache == nil {
		f.cache = map[string]string{}
	}
	s := ""
	if fh, err := os.Open(f.path(rel)); err == nil {
		buf := make([]byte, 1<<20)
		n, _ := fh.Read(buf)
		fh.Close()
		s = string(buf[:n])
	}
	f.cache[rel] = s
	return s
}

// found returns the names of the candidates that exist, in order.
func (f *folder) found(names ...string) []string {
	out := []string{}
	for _, n := range names {
		if f.has(n) {
			out = append(out, n)
		}
	}
	return out
}

// settings collects values for the kind's options. done keeps only what the kind's profile
// accepts, so a table entry for an option that is not there (yet), or a value that is not one of
// its choices, is dropped instead of breaking the form.
type settings struct {
	kind string
	m    map[string]string
}

func newSettings(kind string) *settings { return &settings{kind: kind, m: map[string]string{}} }

func (s *settings) set(key, value string) {
	if value != "" {
		s.m[key] = value
	}
}

func (s *settings) done() map[string]string {
	for range len(s.m) + 1 {
		err := kinds.Validate(s.kind, s.m)
		if err == nil {
			return s.m
		}
		var ve *kinds.ValidationError
		if !errors.As(err, &ve) {
			break
		}
		if _, ok := s.m[ve.Key]; !ok { // a requirement of an option we did not set: nothing fits together
			break
		}
		delete(s.m, ve.Key)
	}
	return map[string]string{}
}

// ---- PHP: composer.json, WordPress files ---------------------------------------------------

// phpFrameworks: the composer packages that name a framework, in order, with the folder to serve
// and the port the framework's own server uses.
var phpFrameworks = []struct {
	id       string
	packages []string
	docroot  string
	port     int
}{
	{"laravel", []string{"laravel/framework"}, "public", 8000},
	{"symfony", []string{"symfony/framework-bundle", "symfony/symfony", "symfony/http-kernel"}, "public", 8000},
}

func probePHP(d *folder) *Result {
	wp := d.found("wp-config.php", "wp-load.php", "wp-content")
	hasComposer := d.has("composer.json")
	if !hasComposer && len(wp) == 0 {
		return nil
	}
	r := &Result{Kind: "php", Confidence: "medium", Port: 8000}
	st := newSettings("php")
	r.Markers = d.found("composer.json")
	if hasComposer {
		var c struct {
			Name       string            `json:"name"`
			Require    map[string]string `json:"require"`
			RequireDev map[string]string `json:"require-dev"`
		}
		_ = json.Unmarshal([]byte(d.read("composer.json")), &c)
		if i := strings.LastIndexByte(c.Name, '/'); i >= 0 {
			c.Name = c.Name[i+1:]
		}
		r.Name = c.Name
		for _, fw := range phpFrameworks {
			if hasAny(c.Require, c.RequireDev, fw.packages...) {
				r.Framework, r.Confidence, r.Port = fw.id, "high", fw.port
				st.set("docroot", fw.docroot)
				break
			}
		}
		if r.Framework == "" && hasPrefix(c.Require, c.RequireDev, "johnpbloch/wordpress", "roots/wordpress") {
			r.Framework = "wordpress"
		}
	}
	if len(wp) > 0 && (r.Framework == "" || r.Framework == "wordpress") {
		r.Framework, r.Confidence, r.Port = "wordpress", "high", 0
		r.Markers = append(r.Markers, wp...)
	}
	if r.Framework == "laravel" {
		r.Markers = append(r.Markers, d.found("artisan")...)
	}
	if r.Framework == "" && d.has("public/index.php") {
		st.set("docroot", "public")
	}
	r.Settings = st.done()
	return r
}

func hasAny(a, b map[string]string, names ...string) bool {
	for _, n := range names {
		if _, ok := a[n]; ok {
			return true
		}
		if _, ok := b[n]; ok {
			return true
		}
	}
	return false
}

func hasPrefix(a, b map[string]string, prefixes ...string) bool {
	for _, m := range []map[string]string{a, b} {
		for k := range m {
			for _, p := range prefixes {
				if strings.HasPrefix(k, p) {
					return true
				}
			}
		}
	}
	return false
}

// ---- Node: package.json --------------------------------------------------------------------

func probeNode(d *folder) *Result {
	if !d.has("package.json") {
		return nil
	}
	var p struct {
		Name            string            `json:"name"`
		Main            string            `json:"main"`
		Scripts         map[string]any    `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	_ = json.Unmarshal([]byte(d.read("package.json")), &p)
	r := &Result{Kind: "node", Confidence: "medium", Markers: []string{"package.json"}}
	r.Name = p.Name
	if strings.HasPrefix(r.Name, "@") { // @scope/name
		if i := strings.IndexByte(r.Name, '/'); i >= 0 {
			r.Name = r.Name[i+1:]
		}
	}
	dep := func(n string) bool { return hasAny(p.Dependencies, p.DevDependencies, n) }
	configs := d.found("vite.config.js", "vite.config.mjs", "vite.config.cjs", "vite.config.ts", "vite.config.mts", "vite.config.cts")
	vitePlus := usesVitePlus(d, p.Scripts, dep("vite-plus"), configs)
	switch {
	case dep("next"):
		r.Framework, r.Port = "nextjs", 3000
		r.Markers = append(r.Markers, d.found("next.config.js", "next.config.mjs", "next.config.ts")...)
	case dep("@nestjs/core"):
		r.Framework, r.Port = "nestjs", 3000
		r.Markers = append(r.Markers, d.found("nest-cli.json")...)
	case dep("astro"):
		r.Framework, r.Port = "astro", 4321
		r.Markers = append(r.Markers, d.found("astro.config.mjs", "astro.config.js", "astro.config.ts")...)
	case vitePlus:
		r.Framework, r.Port = "vite-plus", 5173
		r.Markers = append(r.Markers, configs...)
	case dep("vite"):
		r.Framework, r.Port = "vite", 5173
		switch {
		case dep("react"):
			r.Framework = "vite-react"
		case dep("vue"):
			r.Framework = "vite-vue"
		case dep("svelte"):
			r.Framework = "vite-svelte"
		}
		r.Markers = append(r.Markers, d.found("vite.config.js", "vite.config.mjs", "vite.config.ts")...)
	case dep("express"):
		r.Framework, r.Port = "express", 3000
	}
	if r.Framework != "" {
		r.Confidence = "high"
	}
	st := newSettings("node")
	script := func(n string) bool { _, ok := p.Scripts[n].(string); return ok }
	switch {
	case vitePlus && r.Framework == "vite-plus": // the project's own vp command (0.0.6)
		st.set("run", "vp")
		st.set("vpCommand", "dev")
	case script("dev"):
		st.set("run", "npm")
		st.set("npmScript", "dev")
	case script("start"):
		st.set("run", "npm")
		st.set("npmScript", "start")
	default:
		entry := strings.TrimPrefix(filepath.ToSlash(p.Main), "./")
		if entry == "" || !d.has(entry) {
			entry = ""
			for _, n := range []string{"server.js", "index.js", "app.js", "main.js"} {
				if d.has(n) {
					entry = n
					break
				}
			}
		}
		if entry != "" {
			st.set("run", "script")
			st.set("script", entry)
		}
	}
	// The package manager, from its lock file, when the kind has that option (0.0.6, Package manager).
	switch {
	case d.has("pnpm-lock.yaml"):
		st.set("packageManager", "pnpm")
	case d.has("yarn.lock"):
		st.set("packageManager", "yarn")
	}
	r.Settings = st.done()
	return r
}

// ---- Go: go.mod ----------------------------------------------------------------------------

var (
	goModule  = regexp.MustCompile(`(?m)^\s*module\s+"?(\S+?)"?\s*$`)
	goVersion = regexp.MustCompile(`^v\d+$`)
)

func probeGo(d *folder) *Result {
	if !d.has("go.mod") {
		return nil
	}
	mod := d.read("go.mod")
	r := &Result{Kind: "go", Framework: "go-nethttp", Confidence: "medium", Port: 8080, Markers: []string{"go.mod"}}
	if m := goModule.FindStringSubmatch(mod); m != nil {
		parts := strings.Split(strings.Trim(m[1], "/"), "/")
		name := parts[len(parts)-1]
		if len(parts) > 1 && goVersion.MatchString(name) { // example.com/app/v2
			name = parts[len(parts)-2]
		}
		r.Name = name
	}
	switch {
	case strings.Contains(mod, "github.com/gin-gonic/gin"):
		r.Framework, r.Confidence = "gin", "high"
	case strings.Contains(mod, "github.com/gofiber/fiber"):
		r.Framework, r.Confidence, r.Port = "fiber", "high", 3000
	}
	st := newSettings("go")
	if !d.has("main.go") { // a program in cmd/<name>
		if es, err := os.ReadDir(d.path("cmd")); err == nil {
			var cmds []string
			for _, e := range es {
				if e.IsDir() && d.has("cmd/"+e.Name()+"/main.go") {
					cmds = append(cmds, e.Name())
				}
			}
			if len(cmds) == 1 {
				st.set("package", "./cmd/"+cmds[0])
			}
		}
	}
	r.Settings = st.done()
	return r
}

// ---- Java: pom.xml, build.gradle -----------------------------------------------------------

var (
	xmlParent     = regexp.MustCompile(`(?s)<parent>.*?</parent>`)
	xmlArtifactID = regexp.MustCompile(`<artifactId>\s*([^<\s]+)\s*</artifactId>`)
	gradleName    = regexp.MustCompile(`rootProject\.name\s*=\s*["']([^"']+)["']`)
)

func probeJava(d *folder) *Result {
	files := d.found("pom.xml", "build.gradle", "build.gradle.kts")
	if len(files) == 0 {
		return nil
	}
	r := &Result{Kind: "java", Confidence: "medium", Markers: files, Port: 8080}
	boot := false
	for _, f := range files {
		t := d.read(f)
		if strings.Contains(t, "spring-boot") || strings.Contains(t, "org.springframework.boot") {
			boot = true
		}
		if f == "pom.xml" && r.Name == "" {
			if m := xmlArtifactID.FindStringSubmatch(xmlParent.ReplaceAllString(t, "")); m != nil {
				r.Name = m[1]
			}
		}
	}
	for _, f := range d.found("settings.gradle", "settings.gradle.kts") {
		if m := gradleName.FindStringSubmatch(d.read(f)); m != nil && r.Name == "" {
			r.Name = m[1]
		}
	}
	if boot {
		r.Framework, r.Confidence = "springboot", "high"
	}
	return r
}

// ---- Python: manage.py, requirements.txt, pyproject.toml -----------------------------------

// pythonFrameworks: the package that names a framework (looked for in requirements.txt and
// pyproject.toml), its usual port and the settings of the Python profile to fill in (the run mode
// and its option: see the Run select in internal/kinds/profiles.json). The settings are dropped
// when the profile has no such option, so adjust this table when the run modes of the Python kind
// change.
var pythonFrameworks = []struct {
	id       string
	pkg      string
	port     int
	settings func(d *folder) map[string]string
}{
	{"django", "django", 8000, func(*folder) map[string]string {
		return map[string]string{"run": "django", "manage": "manage.py"}
	}},
	{"fastapi", "fastapi", 8000, func(d *folder) map[string]string {
		m := map[string]string{"run": "uvicorn"}
		switch {
		case d.has("main.py"):
			m["app"] = "main:app"
		case d.has("app.py"):
			m["app"] = "app:app"
		case d.has("app/main.py"):
			m["app"] = "app.main:app"
		}
		return m
	}},
	{"flask", "flask", 5000, func(d *folder) map[string]string {
		m := map[string]string{"run": "flask"}
		switch {
		case d.has("app.py"):
			m["flaskApp"] = "app"
		case d.has("wsgi.py"):
			m["flaskApp"] = "wsgi"
		}
		return m
	}},
}

var tomlName = regexp.MustCompile(`(?m)^name\s*=\s*["']([^"']+)["']`)

func probePython(d *folder) *Result {
	markers := d.found("manage.py", "requirements.txt", "pyproject.toml")
	if len(markers) == 0 {
		return nil
	}
	r := &Result{Kind: "python", Confidence: "medium", Markers: markers}
	text := strings.ToLower(d.read("requirements.txt") + "\n" + d.read("pyproject.toml"))
	st := newSettings("python")
	for _, fw := range pythonFrameworks {
		ref := regexp.MustCompile(`(^|[\s"',\[(])` + fw.pkg + `($|[\s=<>~!\[;"',\])])`)
		named := ref.MatchString(text)
		if !named && !(fw.id == "django" && d.has("manage.py")) {
			continue
		}
		r.Framework, r.Port = fw.id, fw.port
		if named {
			r.Confidence = "high"
		}
		want := fw.settings(d)
		keys := make([]string, 0, len(want))
		for k := range want {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			st.set(k, want[k])
		}
		break
	}
	if m := tomlName.FindStringSubmatch(d.read("pyproject.toml")); m != nil {
		r.Name = m[1]
	}
	r.Settings = st.done()
	return r
}

var (
	vpImportRE = regexp.MustCompile(`['"]vite-plus[/'"]`)
	vpScriptRE = regexp.MustCompile(`(^|[\s&;|(])vpr?(\s|$)`)
)

// usesVitePlus reports whether a Node project is a Vite+ project (0.0.6): it has the vite-plus
// package, a vite.config that imports vite-plus, or a script that runs vp.
func usesVitePlus(d *folder, scripts map[string]any, hasDep bool, configs []string) bool {
	if hasDep {
		return true
	}
	for _, c := range configs {
		if vpImportRE.MatchString(d.read(c)) {
			return true
		}
	}
	for name, v := range scripts {
		if cmd, ok := v.(string); ok && (name == "vp" || vpScriptRE.MatchString(cmd)) {
			return true
		}
	}
	return false
}
