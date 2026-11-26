package catalog

import "testing"

const composerJSON = `{"stable":[
 {"path":"/download/2.11.2/composer.phar","version":"2.11.2","min-php":70205,"lts":false},
 {"path":"/download/2.2.32/composer.phar","version":"2.2.32","min-php":50300,"lts":true}],
 "preview":[{"path":"/download/9.9.9/composer.phar","version":"9.9.9"}],
 "snapshot":[{"path":"/composer.phar","version":"8a53515e"}]}`

func TestParseComposer(t *testing.T) {
	es, err := parseComposer([]byte(composerJSON), "https://getcomposer.org")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for i, e := range es {
		got[e.ID] = i
		if err := validate(&es[i]); err != nil {
			t.Errorf("%s: %v", e.ID, err)
		}
	}
	if _, ok := got["composer-2.11.2-windows-amd64"]; len(es) != 4 || !ok {
		t.Fatalf("stable releases only, for amd64 and arm64: %+v", got)
	}
	w := es[got["composer-2.11.2-windows-arm64"]]
	if w.URL != "https://getcomposer.org/download/2.11.2/composer.phar" || w.Checksum.Type != "shasums" ||
		w.Checksum.URL != "https://getcomposer.org/download/2.11.2/composer.phar.sha256sum" || w.BinaryRelPath != "composer.bat" ||
		w.OS != "windows" || w.Arch != "arm64" || w.Line != "2" || w.Channel != "stable" || w.Kind != "composer" {
		t.Fatalf("%+v", w)
	}
	if l := es[got["composer-2.2.32-windows-amd64"]]; l.Channel != "lts" || l.Line != "2.2" {
		t.Fatalf("%+v", l)
	}
	for _, bad := range []string{`{"stable":[{"path":"/evil/composer.phar","version":"2.1.0"}]}`, `{"stable":[]}`, `nope`} {
		if _, err := parseComposer([]byte(bad), "https://getcomposer.org"); err == nil {
			t.Errorf("%s must fail", bad)
		}
	}
	if _, err := parseComposer([]byte(composerJSON), "https://evil.example"); err == nil {
		t.Error("another host must be refused")
	}
}
