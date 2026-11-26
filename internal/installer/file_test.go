package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"zero2dev/internal/catalog"
	"zero2dev/internal/model"
)

// Composer is one file, not an archive: it is installed as it is, with a launcher next to it.
func TestSingleFileInstallComposer(t *testing.T) {
	phar := []byte("#!/usr/bin/env php\n<?php echo 'composer';")
	e := newEnv(t, map[string][]byte{"/download/2.8.4/composer.phar": phar})
	entry := model.CatalogEntry{ID: "composer-2.8.4", Kind: "composer", Version: "2.8.4", Name: "Composer 2.8.4",
		URL: e.srv.URL + "/download/2.8.4/composer.phar", Checksum: model.Checksum{Type: "sha256", Value: sum(phar)},
		BinaryRelPath: "composer.bat", OS: runtime.GOOS, Arch: runtime.GOARCH}
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{entry}, nil }
	j, err := e.in.Start(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j.ID); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	dir := filepath.Join(e.in.Bin, entry.ID)
	got, err := os.ReadFile(filepath.Join(dir, "composer.phar"))
	if err != nil || string(got) != string(phar) {
		t.Fatalf("composer.phar: %q %v", got, err)
	}
	bat, err := os.ReadFile(filepath.Join(dir, "composer.bat"))
	if err != nil || !strings.Contains(string(bat), `php "%~dp0composer.phar" %*`) {
		t.Fatalf("composer.bat: %q %v", bat, err)
	}
	var rt model.Runtime
	for _, r := range e.st.Snapshot().Runtimes {
		if r.Kind == "composer" {
			rt = r
		}
	}
	if rt.Binary != filepath.Join(dir, "composer.bat") || rt.Source != "installed" {
		t.Fatalf("runtime %+v", rt)
	}
}

func TestSingleFileChecksumMismatchInstallsNothing(t *testing.T) {
	e := newEnv(t, map[string][]byte{"/composer.phar": []byte("real")})
	entry := model.CatalogEntry{ID: "composer-9.9.9", Kind: "composer", Version: "9.9.9", Name: "Composer",
		URL: e.srv.URL + "/composer.phar", Checksum: model.Checksum{Type: "sha256", Value: strings.Repeat("a", 64)},
		BinaryRelPath: "composer.bat", OS: runtime.GOOS, Arch: runtime.GOARCH}
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{entry}, nil }
	j, err := e.in.Start(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j.ID); done.State != "failed" || !strings.Contains(done.Error, "checksum mismatch") {
		t.Fatalf("%+v", done)
	}
	if _, err := os.Stat(filepath.Join(e.in.Bin, entry.ID)); !os.IsNotExist(err) {
		t.Fatal("nothing may be installed")
	}
}

func TestInstallFileRefusesBadNames(t *testing.T) {
	src := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	for _, name := range []string{"", ".", "a:b"} {
		if err := installFile(src, dest, name); err == nil {
			t.Errorf("name %q must be refused", name)
		}
	}
	// A file is never overwritten.
	if err := installFile(src, dest, "ok.phar"); err != nil {
		t.Fatal(err)
	}
	if err := installFile(src, dest, "ok.phar"); err == nil {
		t.Fatal("existing file must not be replaced")
	}
}

// The catalog accepts a single .phar file besides archives.
func TestCatalogAcceptsPhar(t *testing.T) {
	raw := `[{"id":"composer-2.8.4","kind":"composer","version":"2.8.4","name":"Composer 2.8.4","url":"https://getcomposer.org/download/2.8.4/composer.phar","checksum":{"type":"sha256","value":"` + strings.Repeat("a", 64) + `"},"binaryRelPath":"composer.bat","os":"windows","arch":"amd64"}]`
	es, err := catalog.Parse([]byte(raw))
	if err != nil || len(es) != 1 || es[0].FileName != "composer.phar" {
		t.Fatalf("%+v %v", es, err)
	}
}
