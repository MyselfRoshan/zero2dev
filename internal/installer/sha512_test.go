package installer

import (
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
)

func sum512(b []byte) string { h := sha512.Sum512(b); return hex.EncodeToString(h[:]) }

// Caddy publishes SHA-512 checksums (128 hex characters), unlike Node and
// Go. The first release of Zero2Dev only understood SHA-256, so every Caddy
// install failed with "no checksum for <file> in the published checksum file".
func TestInstallZipWithSha512Shasums(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy.exe": "BIN", "LICENSE": "x", "README.md": "y"})
	other := strings.Repeat("a", 128)
	e := newEnv(t, map[string][]byte{
		"/d/caddy_2.8.4_windows_amd64.zip": z,
		"/d/caddy_2.8.4_checksums.txt": []byte(other + "  caddy_2.8.4_linux_amd64.tar.gz\n" +
			sum512(z) + "  caddy_2.8.4_windows_amd64.zip\n" + other + "  caddy_2.8.4_mac_arm64.tar.gz\n"),
	})
	job := e.install(t, model.CatalogEntry{ID: "caddy-fake", URL: "/d/caddy_2.8.4_windows_amd64.zip", BinaryRelPath: "caddy.exe",
		Checksum: model.Checksum{Type: "shasums", URL: "/d/caddy_2.8.4_checksums.txt"}})
	if job.State != "done" {
		t.Fatalf("sha512 install failed: %+v", job)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "bin", "caddy-fake", "caddy.exe")); err != nil || string(b) != "BIN" {
		t.Fatalf("binary missing: %v %q", err, b)
	}
	log, _ := os.ReadFile(filepath.Join(e.home, "logs", "installer.log"))
	if !strings.Contains(string(log), "sha512 expected=") {
		t.Fatalf("log should name the algorithm:\n%s", log)
	}
}

func TestSha512WrongHashRefuses(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy.exe": "BIN"})
	e := newEnv(t, map[string][]byte{
		"/d/c.zip": z,
		"/d/sums":  []byte(strings.Repeat("b", 128) + "  c.zip\n"),
	})
	job := e.install(t, model.CatalogEntry{ID: "caddy-bad", URL: "/d/c.zip", BinaryRelPath: "caddy.exe",
		Checksum: model.Checksum{Type: "shasums", URL: "/d/sums"}})
	if job.State != "failed" || !strings.Contains(job.Error, "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %+v", job)
	}
	if _, err := os.Stat(filepath.Join(e.home, "bin", "caddy-bad")); !os.IsNotExist(err) {
		t.Fatal("nothing must be extracted on a mismatch")
	}
}

func TestParseShasumsSha512(t *testing.T) {
	h := strings.Repeat("c", 128)
	if got, err := ParseShasums(h+"  a.zip\r\n", "a.zip"); err != nil || got != h {
		t.Fatalf("CRLF sha512 line: %q %v", got, err)
	}
	if got, err := ParseShasums(h+"\n", "anything"); err != nil || got != h {
		t.Fatalf("bare sha512: %q %v", got, err)
	}
	if _, err := ParseShasums(strings.Repeat("c", 100)+"  a.zip", "a.zip"); err == nil {
		t.Fatal("a digest of odd length must not be accepted")
	}
}
