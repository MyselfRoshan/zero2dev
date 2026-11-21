package frameworks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinListIsValid(t *testing.T) {
	list, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 {
		t.Fatalf("%d frameworks", len(list))
	}
	if f, ok := Get(list, "go-nethttp"); !ok || f.Runtime.Kind != "go" || f.Steps[0].Program != "go" {
		t.Fatalf("go-nethttp: %+v", f)
	}
	if f, ok := Get(list, "html"); !ok || f.Site == nil || f.Site.Type != "static" || len(f.Services) != 0 {
		t.Fatalf("html: %+v", f)
	}
	if _, ok := Get(list, "nope"); ok {
		t.Fatal("unknown id")
	}
}

// valid is a minimal framework with one field replaced to break it.
func listWith(fw string) []byte {
	return []byte(`{"format":1,"frameworks":[` + fw + `]}`)
}

const okFW = `{"id":"x","label":"X","language":"go","runtime":{"kind":"go"},"steps":[{"type":"run","program":"go"}],"services":[{"name":"{name}","kind":"go"}]}`

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"id":            strings.Replace(okFW, `"id":"x"`, `"id":"X y"`, 1),
		"label":         strings.Replace(okFW, `"label":"X"`, `"label":" "`, 1),
		"language":      strings.Replace(okFW, `"language":"go"`, `"language":"cobol"`, 1),
		"runtime kind":  strings.Replace(okFW, `"runtime":{"kind":"go"}`, `"runtime":{"kind":"ruby"}`, 1),
		"no steps":      strings.Replace(okFW, `"steps":[{"type":"run","program":"go"}]`, `"steps":[]`, 1),
		"run program":   strings.Replace(okFW, `"program":"go"`, `"program":""`, 1),
		"step type":     strings.Replace(okFW, `"type":"run"`, `"type":"shell"`, 1),
		"service kind":  strings.Replace(okFW, `"kind":"go"}]}`, `"kind":"ruby"}]}`, 1),
		"download http": strings.Replace(okFW, `{"type":"run","program":"go"}`, `{"type":"download","url":"http://x/y.zip","checksum":"sha256:aa"}`, 1),
		"download sum":  strings.Replace(okFW, `{"type":"run","program":"go"}`, `{"type":"download","url":"https://x/y.zip"}`, 1),
		"file path":     strings.Replace(okFW, `{"type":"run","program":"go"}`, `{"type":"files","files":{"../x":"y"}}`, 1),
		"composer":      strings.Replace(okFW, `"steps"`, `"composer":true,"steps"`, 1),
		"db uses":       strings.Replace(okFW, `"kind":"go"}]}`, `"kind":"go","usesDatabase":true}]}`, 1),
		"site type":     strings.Replace(okFW, `"services"`, `"site":{"type":"ftp"},"services"`, 1),
		"none":          strings.Replace(okFW, `"services":[{"name":"{name}","kind":"go"}]`, `"services":[]`, 1),
	}
	for name, fw := range cases {
		if _, err := Parse(listWith(fw)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s must be refused, got %v", name, err)
		}
	}
	if _, err := Parse(listWith(okFW)); err != nil {
		t.Fatalf("the base case is valid: %v", err)
	}
	if _, err := Parse(listWith(okFW + "," + okFW)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate id: %v", err)
	}
	if _, err := Parse([]byte(`{"format":2,"frameworks":[]}`)); !errors.Is(err, ErrInvalid) {
		t.Fatal("format 2")
	}
	if _, err := Parse([]byte(`nope`)); !errors.Is(err, ErrInvalid) {
		t.Fatal("not json")
	}
}

func TestDatabaseNeedsAKnownDefault(t *testing.T) {
	db := `"database":{"kinds":["mysql","postgres"],"default":"mysql"},`
	if _, err := Parse(listWith(strings.Replace(okFW, `"runtime"`, db+`"runtime"`, 1))); err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(db, `"default":"mysql"`, `"default":"redis"`, 1)
	if _, err := Parse(listWith(strings.Replace(okFW, `"runtime"`, bad+`"runtime"`, 1))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("default outside kinds: %v", err)
	}
	bad = strings.Replace(db, `"mysql","postgres"`, `"redis"`, 1)
	if _, err := Parse(listWith(strings.Replace(okFW, `"runtime"`, bad+`"runtime"`, 1))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("redis is no project database: %v", err)
	}
}

func TestLoadPrefersTheFileInTheDataFolder(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "frameworks.json"), listWith(okFW), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := Load(home)
	if err != nil || len(list) != 1 || list[0].ID != "x" {
		t.Fatalf("%+v %v", list, err)
	}
	// A damaged file is an error, not silently replaced by the built-in list.
	if err := os.WriteFile(filepath.Join(home, "frameworks.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home); !errors.Is(err, ErrInvalid) {
		t.Fatalf("damaged file: %v", err)
	}
}

// The first list of 0.0.6 (docs/ROADMAP.md): every framework is there, its generator runs
// non-interactively, and every placeholder is a known one (Parse checks that).
func TestShippedList(t *testing.T) {
	list, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"laravel", "symfony", "wordpress", "php", "express", "nestjs", "nextjs", "vite-react", "vite-vue", "vite-svelte",
		"astro", "django", "fastapi", "flask", "go-nethttp", "gin", "fiber", "springboot", "html"}
	for _, id := range want {
		if _, ok := Get(list, id); !ok {
			t.Errorf("framework %s is missing", id)
		}
	}
	if len(list) != len(want) {
		t.Errorf("%d frameworks, %d expected", len(list), len(want))
	}
	for _, fw := range list {
		if fw.Language != "static" && fw.Runtime.Kind == "" {
			t.Errorf("%s: a %s framework needs a runtime", fw.ID, fw.Language)
		}
		for i, st := range fw.Steps {
			if st.Label == "" {
				t.Errorf("%s step %d has no label", fw.ID, i+1)
			}
		}
	}
	wp, _ := Get(list, "wordpress")
	if wp.Steps[0].Checksum != "https://wordpress.org/latest.zip.sha1" || wp.Steps[0].Strip != 1 || wp.Database == nil || wp.Database.Optional {
		t.Errorf("wordpress: %+v", wp.Steps[0])
	}
	sp, _ := Get(list, "springboot")
	if !sp.Steps[0].Unverified || sp.Steps[0].Reason == "" {
		t.Errorf("spring boot downloads without a checksum and says why: %+v", sp.Steps[0])
	}
	lv, _ := Get(list, "laravel")
	keys := map[string]bool{}
	for _, e := range lv.Env {
		keys[e.Key] = true
	}
	for _, k := range []string{"DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD", "APP_URL"} {
		if !keys[k] {
			t.Errorf("laravel env lacks %s", k)
		}
	}
}

func TestSchemaRules(t *testing.T) {
	step := func(s string) string {
		return strings.Replace(okFW, `{"type":"run","program":"go"}`, s, 1)
	}
	bad := map[string]string{
		"unknown placeholder":        step(`{"type":"run","program":"go","args":["{nope}"]}`),
		"venv outside python":        step(`{"type":"run","program":"go","args":["{venv}"]}`),
		"db placeholder without db":  step(`{"type":"run","program":"go","args":["{dbPassword}"]}`),
		"program with a path":        step(`{"type":"run","program":"bin/go"}`),
		"program with a backslash":   step(`{"type":"run","program":"c:\go"}`),
		"program with a space":       step(`{"type":"run","program":"go build"}`),
		"program ./a/b":              step(`{"type":"run","program":"./a/b"}`),
		"bad in":                     step(`{"type":"run","program":"go","in":"elsewhere"}`),
		"bad env name":               step(`{"type":"run","program":"go","env":{"A=B":"x"}}`),
		"unverified without reason":  step(`{"type":"download","url":"https://x/y.zip","unverified":true}`),
		"unverified and a checksum":  step(`{"type":"download","url":"https://x/y.zip","unverified":true,"reason":"r","checksum":"https://x/y.sha1"}`),
		"checksum is not a checksum": step(`{"type":"download","url":"https://x/y.zip","checksum":"md5:abc"}`),
		"service cwd up":             strings.Replace(okFW, `"kind":"go"}]}`, `"kind":"go","cwd":"../x"}]}`, 1),
		"php site without php":       strings.Replace(okFW, `"services"`, `"site":{"type":"php"},"services"`, 1),
		"proxy site without port":    strings.Replace(okFW, `"services"`, `"site":{"type":"proxy"},"services"`, 1),
		"site root absolute":         strings.Replace(okFW, `"services"`, `"site":{"type":"static","root":"C:/x"},"services"`, 1),
		"env file up":                strings.Replace(okFW, `"steps"`, `"envFile":"../.env","steps"`, 1),
	}
	for name, fw := range bad {
		if _, err := Parse(listWith(fw)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s must be refused, got %v", name, err)
		}
	}
	good := map[string]string{
		"program ./name":    step(`{"type":"run","program":"./mvnw.cmd","env":{"A":"{home}"}}`),
		"unverified":        step(`{"type":"download","url":"https://x/y.zip","unverified":true,"reason":"no checksum is published"}`),
		"sha1 checksum":     step(`{"type":"download","url":"https://x/y.zip","checksum":"sha1:da39a3ee5e6b4b0d3255bfef95601890afd80709"}`),
		"checksum url":      step(`{"type":"download","url":"https://x/y.zip","checksum":"https://x/y.zip.sha1"}`),
		"python with venv":  strings.NewReplacer(`"language":"go"`, `"language":"python"`, `"runtime":{"kind":"go"}`, `"runtime":{"kind":"python"}`, `"kind":"go"}]}`, `"kind":"python"}]}`, `{"type":"run","program":"go"}`, `{"type":"run","program":"pip","args":["{venv}"]}`).Replace(okFW),
		"uses db with a db": strings.NewReplacer(`"runtime"`, `"database":{"kinds":["mysql"],"default":"mysql"},"runtime"`, `{"type":"run","program":"go"}`, `{"type":"run","program":"go","args":["{dbUrl}"]}`).Replace(okFW),
	}
	for name, fw := range good {
		if _, err := Parse(listWith(fw)); err != nil {
			t.Errorf("%s must be accepted: %v", name, err)
		}
	}
	if !UsesDatabase("x {dbHost}") || UsesDatabase("{name} {port}") {
		t.Error("UsesDatabase")
	}
}
