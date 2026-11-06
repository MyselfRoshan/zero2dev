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
	if len(es) != 18 {
		t.Fatalf("want 18 entries, got %d", len(es))
	}
	for _, e := range es {
		if e.Homepage == "" || !strings.HasPrefix(e.Homepage, "https://") {
			t.Errorf("%s: homepage missing", e.ID)
		}
		if e.FileName == "" || strings.ContainsAny(e.FileName, "/?") {
			t.Errorf("%s: fileName %q", e.ID, e.FileName)
		}
		switch e.Kind {
		case "nginx", "postgres": // the publishers have no checksums
			if e.Checksum.Type != "none" || !e.Unverified || e.Line == "" {
				t.Errorf("%s: must be an unverified entry with a version line: %+v", e.ID, e)
			}
		case "mysql": // MySQL publishes only MD5
			if e.Checksum.Type != "md5" || e.Unverified || e.Line == "" {
				t.Errorf("%s: must be checked with MD5: %+v", e.ID, e)
			}
		default:
			if e.Unverified || !(e.Checksum.Type == "shasums" && e.Checksum.URL != "" || e.Checksum.Type == "sha256" && e.Checksum.Value != "") {
				t.Errorf("%s: must use a published checksum: %+v", e.ID, e.Checksum)
			}
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
	got := Annotate(es, "/h/bin", []model.Runtime{{Binary: filepath.Join("/h/bin", "a", "bin", "a")}})
	if !got[0].Installed {
		t.Fatal("should be installed")
	}
}

func TestFileName(t *testing.T) {
	if FileName("https://x.test/a/b/caddy_2.8.4_linux_amd64.tar.gz?x=1") != "caddy_2.8.4_linux_amd64.tar.gz" {
		t.Fatal("FileName")
	}
}

func TestMD5Checksum(t *testing.T) {
	ok := model.CatalogEntry{ID: "mysql-9.1.0-windows-amd64", Kind: "mysql", Version: "9.1.0", URL: "https://cdn.mysql.com/Downloads/MySQL-9.1/mysql-9.1.0-winx64.zip",
		Checksum: model.Checksum{Type: "md5", Value: "11d70a5011b7760508e98b3a2fe5db3f"}, BinaryRelPath: "bin/mysqld.exe", StripComponents: 1, OS: "windows", Arch: "amd64"}
	if err := validate(&ok); err != nil {
		t.Fatalf("md5 entry must be valid: %v", err)
	}
	bad := ok
	bad.Checksum.Value = "11d70a5011b7760508e98b3a2fe5db3"
	if err := validate(&bad); err == nil {
		t.Fatal("md5 must be 32 hex chars")
	}
}
