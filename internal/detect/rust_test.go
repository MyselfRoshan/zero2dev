package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectRust(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  Result
	}{
		{"axum", map[string]string{"Cargo.toml": "[package]\nname = \"web-api\"\nversion = \"0.1.0\"\n\n[dependencies]\naxum = \"0.7\"\ntokio = { version = \"1\", features = [\"full\"] }\n", "src/main.rs": ""},
			Result{Kind: "rust", Framework: "axum", Name: "web-api", Confidence: "high", Port: 3000, Markers: []string{"Cargo.toml"}}},
		{"axum with features", map[string]string{"Cargo.toml": "[package]\nname=\"srv\"\n[dependencies]\naxum = { version = \"0.7\", features = [\"macros\"] }\n"},
			Result{Kind: "rust", Framework: "axum", Name: "srv", Confidence: "high", Port: 3000, Markers: []string{"Cargo.toml"}}},
		{"axum as its own table", map[string]string{"Cargo.toml": "[package]\nname = \"srv\"\n\n[dependencies.axum]\nversion = \"0.7\"\n"},
			Result{Kind: "rust", Framework: "axum", Name: "srv", Confidence: "high", Port: 3000, Markers: []string{"Cargo.toml"}}},
		{"plain program", map[string]string{"Cargo.toml": "[package]\nname = \"tool\"\nversion = \"0.1.0\"\n[dependencies]\nserde = \"1\"\n"},
			Result{Kind: "rust", Name: "tool", Confidence: "medium", Port: 8080, Markers: []string{"Cargo.toml"}}},
		{"axum-extra is not axum", map[string]string{"Cargo.toml": "[package]\nname = \"x\"\n[dependencies]\naxum-extra = \"0.9\"\n"},
			Result{Kind: "rust", Name: "x", Confidence: "medium", Port: 8080, Markers: []string{"Cargo.toml"}}},
		{"axum in a comment or in the package table is not a dependency", map[string]string{"Cargo.toml": "[package]\nname = \"x\"\ndescription = \"axum\"\n# axum = \"0.7\"\n[dependencies]\n# axum = \"0.7\"\n"},
			Result{Kind: "rust", Name: "x", Confidence: "medium", Port: 8080, Markers: []string{"Cargo.toml"}}},
		{"default-run becomes the bin setting", map[string]string{"Cargo.toml": "[package]\nname = \"multi\"\ndefault-run = \"server\"\n"},
			Result{Kind: "rust", Name: "multi", Confidence: "medium", Port: 8080, Settings: map[string]string{"bin": "server"}, Markers: []string{"Cargo.toml"}}},
		{"workspace without a package takes the folder's name", map[string]string{"Cargo.toml": "[workspace]\nmembers = [\"api\"]\n\n[workspace.dependencies]\naxum = \"0.7\"\n"},
			Result{Kind: "rust", Name: "my-folder", Framework: "axum", Confidence: "high", Port: 3000, Markers: []string{"Cargo.toml"}}},
		{"a go.mod beside it does not win over a framework", map[string]string{"Cargo.toml": "[package]\nname = \"a\"\n[dependencies]\naxum = \"0.7\"\n", "go.mod": "module x\n"},
			Result{Kind: "rust", Framework: "axum", Name: "a", Confidence: "high", Port: 3000, Markers: []string{"Cargo.toml"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "my-folder")
			for rel, content := range c.files {
				p := filepath.Join(dir, filepath.FromSlash(rel))
				os.MkdirAll(filepath.Dir(p), 0o755)
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Dir(dir)
			if err != nil {
				t.Fatal(err)
			}
			want := c.want
			if want.Settings == nil {
				want.Settings = map[string]string{}
			}
			if want.Name == "" {
				want.Name = "my-folder"
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got  %+v\nwant %+v", got, want)
			}
		})
	}
}
