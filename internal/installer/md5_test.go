package installer

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"zero2dev/internal/model"
)

func md5hex(b []byte) string { h := md5.Sum(b); return hex.EncodeToString(h[:]) }

// mysqlEnv serves a fake MySQL archive whose entry is checked with MD5 (the
// only checksum MySQL publishes), plus one with a wrong MD5.
func mysqlEnv(t *testing.T) (*env, model.CatalogEntry, model.CatalogEntry, []byte) {
	t.Helper()
	z := makeZip(t, map[string]string{"mysql-9.1.0-winx64/bin/mysqld": "db"})
	e := newEnv(t, map[string][]byte{"/mysql-9.1.0-winx64.zip": z, "/mysql-9.0.0-winx64.zip": z})
	mk := func(id, ver, file, md5sum string) model.CatalogEntry {
		return model.CatalogEntry{ID: id, Kind: "mysql", Version: ver, Name: "MySQL " + ver, URL: e.srv.URL + "/" + file,
			Checksum: model.Checksum{Type: "md5", Value: md5sum}, BinaryRelPath: "bin/mysqld", StripComponents: 1,
			OS: runtime.GOOS, Arch: runtime.GOARCH}
	}
	good := mk("mysql-9.1.0", "9.1.0", "mysql-9.1.0-winx64.zip", md5hex(z))
	bad := mk("mysql-9.0.0", "9.0.0", "mysql-9.0.0-winx64.zip", strings.Repeat("0", 32))
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{good, bad}, nil }
	return e, good, bad, z
}

func TestInstallVerifiesMD5(t *testing.T) {
	e, good, bad, z := mysqlEnv(t)

	j, err := e.in.Start(good.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j.ID); done.State != "done" {
		t.Fatalf("md5 match must install: %+v", done)
	}
	log := strings.Join(e.in.LogTail(200), "\n")
	if !strings.Contains(log, "md5="+md5hex(z)) || !strings.Contains(log, "sha256="+sum(z)) {
		t.Fatalf("log must record the md5 and a strong sha256:\n%s", log)
	}

	j, err = e.in.Start(bad.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := e.wait(t, j.ID)
	if done.State != "failed" || !strings.Contains(done.Error, "checksum mismatch") {
		t.Fatalf("md5 mismatch must fail closed: %+v", done)
	}
	if _, err := os.Stat(filepath.Join(e.in.Bin, bad.ID)); !os.IsNotExist(err) {
		t.Fatal("nothing may be extracted after a mismatch")
	}
}

func TestImportWithPastedMD5(t *testing.T) {
	e, _, bad, z := mysqlEnv(t)
	file := filepath.Join(t.TempDir(), "mysql-9.0.0-winx64.zip")
	if err := os.WriteFile(file, z, 0o644); err != nil {
		t.Fatal(err)
	}
	// The catalog's MD5 is wrong for this file, but a pasted (correct) MD5 wins.
	j, err := e.in.Import(bad.ID, file, strings.ToUpper(md5hex(z)))
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j.ID); done.State != "done" {
		t.Fatalf("pasted md5 must verify: %+v", done)
	}
	if _, err := e.in.Import(bad.ID, file, strings.Repeat("a", 31)); err == nil || !strings.Contains(err.Error(), "MD5 (32)") {
		t.Fatalf("31 hex chars must be refused: %v", err)
	}
}

func TestParseShasumsMD5(t *testing.T) {
	body := strings.Repeat("a", 64) + "  other.zip\n" + strings.Repeat("b", 32) + "  mysql.zip\n"
	if got, err := ParseShasums(body, "mysql.zip"); err != nil || got != strings.Repeat("b", 32) {
		t.Fatalf("%q %v", got, err)
	}
}

func TestEditorsInstallPortable(t *testing.T) {
	z := makeZip(t, map[string]string{"Code.exe": "x", "resources/app/package.json": "{}"})
	e := newEnv(t, map[string][]byte{"/VSCode-win32-x64-1.145.0.zip": z})
	entry := model.CatalogEntry{ID: "vscode-1.145.0", Kind: "vscode", Version: "1.145.0", Name: "VS Code 1.145.0",
		URL: e.srv.URL + "/VSCode-win32-x64-1.145.0.zip", Checksum: model.Checksum{Type: "sha256", Value: sum(z)},
		BinaryRelPath: "Code.exe", OS: runtime.GOOS, Arch: runtime.GOARCH}
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{entry}, nil }
	j, err := e.in.Start(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j.ID); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	if st, err := os.Stat(filepath.Join(e.in.Bin, entry.ID, "data")); err != nil || !st.IsDir() {
		t.Fatal("VS Code must be installed in portable mode (data folder next to Code.exe)")
	}
}
