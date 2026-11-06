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

func TestMergeRemoteBackfillsOnlyMissingManifestMetadata(t *testing.T) {
	base := model.CatalogEntry{ID: "node-1", Kind: "node", Version: "1", Name: "Built in", URL: "https://example.com/a.zip", Checksum: model.Checksum{Type: "none"}, Unverified: true, BinaryRelPath: "node", OS: "linux", Arch: "amd64", Channel: "stable"}
	invalid := base
	invalid.URL = "http://evil.example/a.zip"
	invalid.Line = "bad"
	remote := base
	remote.Name, remote.Line, remote.Channel = "Remote", "1", "lts"
	newEntry := remote
	newEntry.ID = "node-2"
	newEntry.Source, newEntry.Installed, newEntry.Supported = "untrusted", true, true
	duplicate := newEntry
	duplicate.Line = "different"
	got := mergeRemote([]model.CatalogEntry{base}, []model.CatalogEntry{invalid, remote, newEntry, duplicate})
	want := base
	want.Line = "1"
	if len(got) != 2 || got[0] != want {
		t.Fatalf("manifest entry should only gain missing metadata: %+v", got)
	}
	if got[1].ID != "node-2" || got[1].Line != "1" || got[1].Source != "remote" || got[1].Installed || got[1].Supported || got[1].FileName != "a.zip" {
		t.Fatalf("remote entry was not normalized: %+v", got[1])
	}
	base.Channel = ""
	got = mergeRemote([]model.CatalogEntry{base}, []model.CatalogEntry{invalid})
	if got[0] != base {
		t.Fatalf("invalid clash changed manifest: %+v", got[0])
	}
	got = mergeRemote([]model.CatalogEntry{base}, []model.CatalogEntry{remote})
	if got[0].Channel != "lts" {
		t.Fatalf("missing channel not backfilled: %+v", got[0])
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
