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
