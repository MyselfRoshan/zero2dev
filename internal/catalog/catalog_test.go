package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
)

func TestEmbeddedManifestValid(t *testing.T) {
	es, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 6 {
		t.Fatalf("want 6 entries, got %d", len(es))
	}
	for _, e := range es {
		if e.Homepage == "" || !strings.HasPrefix(e.Homepage, "https://") {
			t.Errorf("%s: homepage missing", e.ID)
		}
		if e.FileName == "" || strings.ContainsAny(e.FileName, "/?") {
			t.Errorf("%s: fileName %q", e.ID, e.FileName)
		}
		if e.Checksum.Type != "shasums" || e.Checksum.URL == "" {
			t.Errorf("%s: must use a published checksum", e.ID)
		}
	}
}

func TestOverrideAndValidation(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, "catalog.json"), []byte(`[{"id":"x","kind":"node","version":"1","name":"X","url":"http://evil.example/x.zip","checksum":{"type":"sha256","value":"00"},"binaryRelPath":"a","os":"linux","arch":"amd64"}]`), 0o600)
	if _, err := Load(home); err == nil {
		t.Fatal("http url and short hash must be rejected")
	}
	os.WriteFile(filepath.Join(home, "catalog.json"), []byte(`[{"id":"x","kind":"node","version":"1","name":"X","url":"https://h/x.zip","checksum":{"type":"sha256","value":"`+
		"0000000000000000000000000000000000000000000000000000000000000000"+`"},"binaryRelPath":"../a","os":"linux","arch":"amd64"}]`), 0o600)
	if _, err := Load(home); err == nil {
		t.Fatal("binaryRelPath with .. must be rejected")
	}
}

func TestAnnotate(t *testing.T) {
	es := []model.CatalogEntry{{ID: "a", BinaryRelPath: "bin/a", OS: "linux", Arch: "amd64"}}
	got := Annotate(es, "/h", []model.Runtime{{Binary: filepath.Join("/h", "runtimes", "a", "bin", "a")}})
	if !got[0].Installed {
		t.Fatal("should be installed")
	}
}

func TestFileName(t *testing.T) {
	if FileName("https://x.test/a/b/caddy_2.8.4_linux_amd64.tar.gz?x=1") != "caddy_2.8.4_linux_amd64.tar.gz" {
		t.Fatal("FileName")
	}
}
