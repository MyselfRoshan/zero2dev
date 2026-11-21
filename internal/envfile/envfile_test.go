package envfile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sub", ".env")
	if content != "\x00none" {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCreatesFileAndFolder(t *testing.T) {
	p := write(t, "\x00none")
	w, err := Apply(p, []Line{{Key: "A", Value: "1"}, {Key: "B", Value: "two words"}})
	if err != nil || !reflect.DeepEqual(w, []string{"A", "B"}) {
		t.Fatalf("%v %v", w, err)
	}
	if got := read(t, p); got != "A=1\nB=\"two words\"\n" {
		t.Fatalf("%q", got)
	}
}

func TestKeepsEverythingElse(t *testing.T) {
	p := write(t, "# comment\nAPP_NAME=Laravel\n\n# DB_HOST=commented\nexport DB_PORT=1\nDB_PORT=2\nLAST=x")
	if _, err := Apply(p, []Line{{Key: "DB_HOST", Value: "127.0.0.1"}, {Key: "DB_PORT", Value: "3306"}, {Key: "APP_NAME", Value: "My Shop"}}); err != nil {
		t.Fatal(err)
	}
	want := "# comment\nAPP_NAME=\"My Shop\"\n\n# DB_HOST=commented\nexport DB_PORT=1\nDB_PORT=3306\nLAST=x\nDB_HOST=127.0.0.1\n"
	if got := read(t, p); got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestIfMissingNeverOverwrites(t *testing.T) {
	p := write(t, "APP_KEY=base64:keep\r\nDB_HOST=db.example\r\n")
	w, err := Apply(p, []Line{{Key: "APP_KEY", Value: "new", IfMissing: true}, {Key: "DB_HOST", Value: "127.0.0.1", IfMissing: true}, {Key: "NEW", Value: "1", IfMissing: true}})
	if err != nil || !reflect.DeepEqual(w, []string{"NEW"}) {
		t.Fatalf("%v %v", w, err)
	}
	// CRLF is kept
	if got := read(t, p); got != "APP_KEY=base64:keep\r\nDB_HOST=db.example\r\nNEW=1\r\n" {
		t.Fatalf("%q", got)
	}
	// a commented key is not a key: it is added
	p2 := write(t, "# NEW=old\n")
	if _, err := Apply(p2, []Line{{Key: "NEW", Value: "1", IfMissing: true}}); err != nil || read(t, p2) != "# NEW=old\nNEW=1\n" {
		t.Fatalf("%q %v", read(t, p2), err)
	}
}

func TestNothingToChangeLeavesFileAlone(t *testing.T) {
	p := write(t, "A=1")
	if w, err := Apply(p, []Line{{Key: "A", Value: "2", IfMissing: true}}); err != nil || len(w) != 0 {
		t.Fatalf("%v %v", w, err)
	}
	if got := read(t, p); got != "A=1" { // not even a newline was added
		t.Fatalf("%q", got)
	}
}

func TestBadInput(t *testing.T) {
	p := write(t, "\x00none")
	for _, l := range []Line{{Key: "", Value: "x"}, {Key: "A B", Value: "x"}, {Key: "A=B", Value: "x"}, {Key: "A", Value: "x\ny"}, {Key: "1A", Value: "x"}} {
		if _, err := Apply(p, []Line{l}); err == nil {
			t.Errorf("%+v must be refused", l)
		}
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("nothing may be written for a bad line")
	}
}

func TestQuoteAndGet(t *testing.T) {
	for in, want := range map[string]string{"abc": "abc", "https://a.b:1/x": "https://a.b:1/x", "a b": `"a b"`, `say "hi"`: `"say \"hi\""`, "#x": `"#x"`, "": ""} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q)=%q want %q", in, got, want)
		}
	}
	p := write(t, "A=1\nexport B=two\n#C=3\n")
	if v, ok := Get(p, "B"); !ok || v != "two" {
		t.Fatalf("%q %v", v, ok)
	}
	if _, ok := Get(p, "C"); ok {
		t.Fatal("a commented key is not there")
	}
}
