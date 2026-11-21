package kinds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSatisfiesNode(t *testing.T) {
	cases := []struct {
		spec, have string
		ok, known  bool
	}{
		{"22.18.0", "22.18.0", true, true},
		{"22.18.0", "22.18.1", false, true},
		{"v22.18.0", "22.18.0", true, true},
		{"22", "22.5.1", true, true},
		{"22", "23.0.0", false, true},
		{"22.x", "22.0.0", true, true},
		{"22.x", "20.11.1", false, true},
		{"22.1", "22.1.9", true, true},
		{"22.1", "22.2.0", false, true},
		{">=22", "22.0.0", true, true},
		{">=22", "20.11.1", false, true},
		{">= 22", "24.1.0", true, true},
		{">=20 <25", "24.9.0", true, true},
		{">=20 <25", "25.0.0", false, true},
		{">22", "22.9.9", false, true},
		{">22", "23.0.0", true, true},
		{"<=22", "22.9.9", true, true},
		{"<22", "22.0.0", false, true},
		{"^22.18.0 || ^24.11.0", "22.18.0", true, true},
		{"^22.18.0 || ^24.11.0", "22.17.9", false, true},
		{"^22.18.0 || ^24.11.0", "24.12.1", true, true},
		{"^22.18.0 || ^24.11.0", "23.5.0", false, true},
		{"^22.18.0 || ^24.11.0", "25.0.0", false, true},
		{"^0.2.3", "0.2.9", true, true},
		{"^0.2.3", "0.3.0", false, true},
		{"^0.0.3", "0.0.4", false, true},
		{"^22", "22.99.0", true, true},
		{"~22.1.0", "22.1.7", true, true},
		{"~22.1.0", "22.2.0", false, true},
		{"~22", "22.9.0", true, true},
		{"*", "18.0.0", true, true},
		{"20 - 22", "21.0.0", true, false}, // a hyphen range: not understood, nothing is said
		{"lts/*", "18.0.0", true, false},
		{"lts/iron", "18.0.0", true, false},
		{"node", "18.0.0", true, false},
		{"", "18.0.0", true, false},
		{"22", "", true, false}, // an unknown runtime version
		{"22", "v22.1.0", true, true},
	}
	for _, c := range cases {
		ok, known := SatisfiesNode(c.spec, c.have)
		if ok != c.ok || known != c.known {
			t.Errorf("SatisfiesNode(%q, %q) = %v, %v; want %v, %v", c.spec, c.have, ok, known, c.ok, c.known)
		}
	}
}

func writePinFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadNodePin(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  NodePin
		ok    bool
	}{
		{"none", map[string]string{"package.json": `{"name":"x"}`}, NodePin{}, false},
		{".node-version first", map[string]string{".node-version": "22.18.0\n", ".nvmrc": "20", "package.json": `{"engines":{"node":">=18"}}`}, NodePin{"22.18.0", ".node-version"}, true},
		{"devEngines object", map[string]string{"package.json": `{"devEngines":{"runtime":{"name":"node","version":"^22.18.0 || ^24.11.0"}},"engines":{"node":">=18"}}`}, NodePin{"^22.18.0 || ^24.11.0", "devEngines.runtime"}, true},
		{"devEngines list", map[string]string{"package.json": `{"devEngines":{"runtime":[{"name":"bun","version":"1"},{"name":"node","version":"22.x"}]}}`}, NodePin{"22.x", "devEngines.runtime"}, true},
		{"devEngines of another runtime", map[string]string{"package.json": `{"devEngines":{"runtime":{"name":"deno","version":"2"}},"engines":{"node":">=20"}}`}, NodePin{">=20", "engines.node"}, true},
		{"engines.node", map[string]string{"package.json": `{"engines":{"node":">=22"}}`}, NodePin{">=22", "engines.node"}, true},
		{".nvmrc", map[string]string{".nvmrc": "# comment\nv20.11.1\n"}, NodePin{"v20.11.1", ".nvmrc"}, true},
		{"broken package.json", map[string]string{"package.json": `{nope`, ".nvmrc": "20"}, NodePin{"20", ".nvmrc"}, true},
	}
	for _, c := range cases {
		got, ok := ReadNodePin(writePinFiles(t, c.files))
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
	if _, ok := ReadNodePin(""); ok {
		t.Error("an empty folder name pins nothing")
	}
}
