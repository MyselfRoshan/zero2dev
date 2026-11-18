package supervisor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []envVar
		bad  int // problems expected
	}{
		{"plain", "A=1\nB=two", []envVar{{"A", "1"}, {"B", "two"}}, 0},
		{"export prefix", "export A=1\nexport   B = 2\nexportC=3", []envVar{{"A", "1"}, {"B", "2"}, {"exportC", "3"}}, 0},
		{"comments and blanks", "# top\n\n   # indented\nA=1\n\n", []envVar{{"A", "1"}}, 0},
		{"inline comment needs whitespace", "A=1 # note\nB=1#2\nC=\"x\" # c\nD= # only comment\nE=#nospace", []envVar{{"A", "1"}, {"B", "1#2"}, {"C", "x"}, {"D", ""}, {"E", "#nospace"}}, 0},
		{"tab before comment", "A=1\t# note", []envVar{{"A", "1"}}, 0},
		{"double quotes with escapes", `A="a b"` + "\n" + `B="line1\nline2\t\"q\" \\ \x"`, []envVar{{"A", "a b"}, {"B", "line1\nline2\t\"q\" \\ \\x"}}, 0},
		{"double quotes keep hash and spaces", `A=" # not a comment  "`, []envVar{{"A", " # not a comment  "}}, 0},
		{"single quotes are literal", `A='x\ny $HOME # c'`, []envVar{{"A", `x\ny $HOME # c`}}, 0},
		{"no expansion", "A=$HOME\nB=${A}x\nC=\"$A\"", []envVar{{"A", "$HOME"}, {"B", "${A}x"}, {"C", "$A"}}, 0},
		{"equals inside value", "URL=postgres://u:p@h/db?x=1&y=2", []envVar{{"URL", "postgres://u:p@h/db?x=1&y=2"}}, 0},
		{"empty value", "A=\nB=\"\"\nC=''", []envVar{{"A", ""}, {"B", ""}, {"C", ""}}, 0},
		{"last duplicate wins, first position kept", "A=1\nB=2\nA=3", []envVar{{"A", "3"}, {"B", "2"}}, 0},
		{"crlf and bom", "\ufeffA=1\r\nB=\"x\"\r\n", []envVar{{"A", "1"}, {"B", "x"}}, 0},
		{"multi-line double quoted", "KEY=\"-----BEGIN\nabc\n-----END\"\nB=2", []envVar{{"KEY", "-----BEGIN\nabc\n-----END"}, {"B", "2"}}, 0},
		{"unterminated quote does not swallow the file", "A=\"oops\nB=2", []envVar{{"A", "oops"}, {"B", "2"}}, 1},
		{"invalid lines skipped", "just words\n=novalue\n1BAD=x\nA B=1\nOK=1", []envVar{{"OK", "1"}}, 4},
		{"dots and dashes in keys", "my.key=1\nmy-key=2", []envVar{{"my.key", "1"}, {"my-key", "2"}}, 0},
		{"trailing junk after quote ignored", `A="x"junk`, []envVar{{"A", "x"}}, 0},
		{"surrounding spaces trimmed", "  A  =   v   \n", []envVar{{"A", "v"}}, 0},
		{"empty file", "", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, bad := parseEnvFile([]byte(c.in))
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("vars\n got %q\nwant %q", got, c.want)
			}
			if len(bad) != c.bad {
				t.Errorf("problems %q, want %d", bad, c.bad)
			}
			for _, p := range bad { // never repeat a value
				if strings.Contains(p, "oops") || strings.Contains(p, "words") {
					t.Errorf("problem leaks content: %q", p)
				}
			}
		})
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("A=1\nB=\"2\"\nbad line\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "config"), 0o755)
	os.WriteFile(filepath.Join(dir, "config", "app.env"), []byte("C=3"), 0o644)

	vars, warns, err := loadEnvFile(dir, ".env")
	if err != nil || len(vars) != 2 || vars[1] != (envVar{"B", "2"}) || len(warns) != 1 || !strings.Contains(warns[0], "line 3") {
		t.Fatalf("plain: %v %q %v", vars, warns, err)
	}
	if vars, _, err = loadEnvFile(dir, filepath.Join("config", "app.env")); err != nil || len(vars) != 1 || vars[0].Key != "C" {
		t.Fatalf("nested: %v %v", vars, err)
	}
	if vars, _, err = loadEnvFile(dir, "./config/../.env"); err != nil || len(vars) != 2 {
		t.Fatalf("cleaned inside path: %v %v", vars, err)
	}

	// missing: a warning, not an error
	vars, warns, err = loadEnvFile(dir, "nope.env")
	if err != nil || vars != nil || len(warns) != 1 || !strings.Contains(warns[0], "not found") {
		t.Fatalf("missing: %v %q %v", vars, warns, err)
	}
	// a folder where the file should be: also only a warning
	if _, warns, err = loadEnvFile(dir, "config"); err != nil || len(warns) != 1 {
		t.Fatalf("directory: %q %v", warns, err)
	}
	// no folder: nothing to read, a warning
	if vars, warns, err = loadEnvFile("", ".env"); err != nil || vars != nil || len(warns) != 1 {
		t.Fatalf("no cwd: %v %q %v", vars, warns, err)
	}
	// not set: nothing at all
	if vars, warns, err = loadEnvFile(dir, "  "); err != nil || vars != nil || warns != nil {
		t.Fatalf("empty: %v %q %v", vars, warns, err)
	}
	// escaping the folder is an error
	for _, p := range []string{"../.env", filepath.Join("a", "..", "..", ".env"), filepath.Join(dir, ".env"), "..", `\evil`, `\\server\share\.env`, `C:.env`, `z:/config/.env`} {
		if _, _, err := loadEnvFile(dir, p); err == nil {
			t.Errorf("%q must be refused", p)
		}
	}
	// too large: warning
	big := make([]byte, maxEnvFile+10)
	for i := range big {
		big[i] = 'a'
	}
	os.WriteFile(filepath.Join(dir, "big.env"), big, 0o644)
	if vars, warns, err = loadEnvFile(dir, "big.env"); err != nil || vars != nil || len(warns) != 1 {
		t.Fatalf("big: %v %q %v", vars, warns, err)
	}
}
