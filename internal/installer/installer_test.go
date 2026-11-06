package installer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/store"
)

type nopEm struct{}

func (nopEm) Publish(string, any) {}

type env struct {
	home string
	st   *store.Store
	in   *Installer
	srv  *httptest.Server
	rts  *runtimes.Manager
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func makeZip(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

type tarFile struct {
	name, body, link string
	mode             int64
}

func makeTarGz(t *testing.T, files []tarFile) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		h := &tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg}
		if f.link != "" {
			h = &tar.Header{Name: f.name, Typeflag: tar.TypeSymlink, Linkname: f.link, Mode: 0o777}
		}
		tw.WriteHeader(h)
		if f.link == "" {
			tw.Write([]byte(f.body))
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func newEnv(t *testing.T, routes map[string][]byte) *env {
	return newEnvHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
}

func newEnvHandler(t *testing.T, h http.Handler) *env {
	t.Helper()
	home := t.TempDir()
	st, _ := store.Open(filepath.Join(home, "state.json"))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	rts := runtimes.New(home, st, nopEm{})
	in := New(home, st, rts, nopEm{})
	in.Sleep = func(time.Duration) {} // no real backoff in tests
	return &env{home: home, st: st, in: in, srv: srv, rts: rts}
}

// entry normalises a test entry for this machine and the test server, and
// makes it the only catalog entry.
func (e *env) entry(entry model.CatalogEntry) model.CatalogEntry {
	entry.OS, entry.Arch, entry.Kind = runtime.GOOS, runtime.GOARCH, "node"
	entry.Name, entry.Version = "Fake", "1.0.0"
	if entry.URL != "" {
		entry.URL = e.srv.URL + entry.URL
	}
	if entry.Checksum.URL != "" {
		entry.Checksum.URL = e.srv.URL + entry.Checksum.URL
	}
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{entry}, nil }
	return entry
}

func (e *env) wait(t *testing.T, id string) model.Job {
	t.Helper()
	for i := 0; i < 500; i++ {
		cur, _ := e.in.Get(id)
		if cur.State != "running" {
			return cur
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return model.Job{}
}

func (e *env) install(t *testing.T, entry model.CatalogEntry) model.Job {
	t.Helper()
	entry = e.entry(entry)
	j, err := e.in.Start(entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	return e.wait(t, j.ID)
}

func TestInstallZipWithShasums(t *testing.T) {
	z := makeZip(t, map[string]string{"top/node.exe": "BIN", "top/lib/x.txt": "x", "top/": ""})
	e := newEnv(t, map[string][]byte{
		"/a/pkg.zip":    z,
		"/a/SHASUMS256": []byte("deadbeef  other.zip\n" + sum(z) + "  pkg.zip\n"),
	})
	job := e.install(t, model.CatalogEntry{ID: "fake-1", URL: "/a/pkg.zip", BinaryRelPath: "node.exe", StripComponents: 1,
		Checksum: model.Checksum{Type: "shasums", URL: "/a/SHASUMS256"}})
	if job.State != "done" || job.Percent != 100 || job.RuntimeID == "" {
		t.Fatalf("job %+v", job)
	}
	b, err := os.ReadFile(filepath.Join(e.home, "bin", "fake-1", "node.exe"))
	if err != nil || string(b) != "BIN" {
		t.Fatalf("binary not extracted with strip: %v %q", err, b)
	}
	rs := e.rts.List()
	if len(rs) != 1 || rs[0].Source != "installed" || rs[0].ID != job.RuntimeID {
		t.Fatalf("runtime not registered: %+v", rs)
	}
	// installing again conflicts
	if _, err := e.in.Start("fake-1"); err == nil {
		t.Fatal("second install should conflict")
	}
	// deleting an installed runtime removes the folder
	if err := e.rts.Delete(rs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.home, "bin", "fake-1")); !os.IsNotExist(err) {
		t.Fatal("install folder should be removed")
	}
	// no leftovers
	if ents, _ := os.ReadDir(filepath.Join(e.home, "tmp")); len(ents) != 0 {
		t.Fatalf("tmp not cleaned: %v", ents)
	}
}

func TestInstallTarGzBareHashAndSymlink(t *testing.T) {
	tg := makeTarGz(t, []tarFile{
		{name: "pkg/bin/node", body: "ELF", mode: 0o755},
		{name: "pkg/lib/npm-cli.js", body: "js", mode: 0o644},
		{name: "pkg/bin/npm", link: "../lib/npm-cli.js"},
	})
	e := newEnv(t, map[string][]byte{"/x/pkg.tar.gz": tg, "/x/pkg.tar.gz.sha256": []byte(strings.ToUpper(sum(tg)) + "\n")})
	job := e.install(t, model.CatalogEntry{ID: "fake-2", URL: "/x/pkg.tar.gz", BinaryRelPath: "bin/node", StripComponents: 1,
		Checksum: model.Checksum{Type: "shasums", URL: "/x/pkg.tar.gz.sha256"}})
	if job.State != "done" {
		t.Fatalf("job %+v", job)
	}
	root := filepath.Join(e.home, "bin", "fake-2")
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(root, "bin", "node"))
		if err != nil || st.Mode()&0o111 == 0 {
			t.Fatalf("binary must be executable: %v", err)
		}
		if b, err := os.ReadFile(filepath.Join(root, "bin", "npm")); err != nil || string(b) != "js" {
			t.Fatalf("symlink not usable: %v", err)
		}
	}
}

func TestInstallInlineSha256(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy": "C"})
	e := newEnv(t, map[string][]byte{"/c.zip": z})
	job := e.install(t, model.CatalogEntry{ID: "fake-3", URL: "/c.zip", BinaryRelPath: "caddy", Checksum: model.Checksum{Type: "sha256", Value: sum(z)}})
	if job.State != "done" {
		t.Fatalf("job %+v", job)
	}
}

func TestWrongHashRefusesAndExtractsNothing(t *testing.T) {
	z := makeZip(t, map[string]string{"node.exe": "BIN"})
	for _, mode := range []string{"inline", "shasums"} {
		bad := strings.Repeat("0", 64)
		routes := map[string][]byte{"/p.zip": z, "/sums": []byte(bad + "  p.zip\n")}
		e := newEnv(t, routes)
		entry := model.CatalogEntry{ID: "fake-4", URL: "/p.zip", BinaryRelPath: "node.exe"}
		if mode == "inline" {
			entry.Checksum = model.Checksum{Type: "sha256", Value: bad}
		} else {
			entry.Checksum = model.Checksum{Type: "shasums", URL: "/sums"}
		}
		job := e.install(t, entry)
		if job.State != "failed" || !strings.Contains(job.Error, "checksum mismatch") {
			t.Fatalf("%s: job %+v", mode, job)
		}
		ents, _ := os.ReadDir(filepath.Join(e.home, "bin"))
		if len(ents) != 0 {
			t.Fatalf("%s: runtimes dir must stay empty, has %v", mode, ents)
		}
		if len(e.rts.List()) != 0 {
			t.Fatal("no runtime may be registered")
		}
		if ents, _ := os.ReadDir(filepath.Join(e.home, "tmp")); len(ents) != 0 {
			t.Fatalf("tmp not cleaned: %v", ents)
		}
	}
}

func TestMissingChecksumEntryFails(t *testing.T) {
	z := makeZip(t, map[string]string{"node.exe": "BIN"})
	e := newEnv(t, map[string][]byte{"/p.zip": z, "/sums": []byte(sum(z) + "  different.zip\n")})
	job := e.install(t, model.CatalogEntry{ID: "fake-5", URL: "/p.zip", BinaryRelPath: "node.exe", Checksum: model.Checksum{Type: "shasums", URL: "/sums"}})
	if job.State != "failed" || !strings.Contains(job.Error, "no checksum for p.zip") {
		t.Fatalf("job %+v", job)
	}
}

func TestZipSlipRejected(t *testing.T) {
	for _, evil := range []string{"../evil.txt", "a/../../evil.txt", `..\evil.txt`, "/abs/evil.txt"} {
		z := makeZip(t, map[string]string{"ok/node.exe": "BIN", evil: "pwned"})
		e := newEnv(t, map[string][]byte{"/s.zip": z})
		job := e.install(t, model.CatalogEntry{ID: "fake-6", URL: "/s.zip", BinaryRelPath: "node.exe", Checksum: model.Checksum{Type: "sha256", Value: sum(z)}})
		if job.State != "failed" || !strings.Contains(job.Error, "zip-slip") {
			t.Fatalf("%q: job %+v", evil, job)
		}
		for _, p := range []string{filepath.Join(e.home, "evil.txt"), filepath.Join(e.home, "bin", "evil.txt"), filepath.Join(e.home, "..", "evil.txt"), "/abs/evil.txt"} {
			if _, err := os.Stat(p); err == nil {
				t.Fatalf("%q: file escaped to %s", evil, p)
			}
		}
		if ents, _ := os.ReadDir(filepath.Join(e.home, "bin")); len(ents) != 0 {
			t.Fatalf("temp dir not cleaned: %v", ents)
		}
		if len(e.rts.List()) != 0 {
			t.Fatal("must not register")
		}
	}
}

func TestTarSymlinkEscapeRejected(t *testing.T) {
	cases := [][]tarFile{
		{{name: "x/bin/node", body: "b", mode: 0o755}, {name: "x/bin/evil", link: "../../../../etc/passwd"}},
		{{name: "x/bin/node", body: "b", mode: 0o755}, {name: "x/abs", link: "/etc/passwd"}},
		// write through a symlinked directory that points outside
		{{name: "x/d", link: "."}, {name: "x/bin/node", body: "b", mode: 0o755}, {name: "x/d/../../../evil", body: "e"}},
	}
	for i, files := range cases {
		tg := makeTarGz(t, files)
		e := newEnv(t, map[string][]byte{"/s.tar.gz": tg})
		job := e.install(t, model.CatalogEntry{ID: "fake-7", URL: "/s.tar.gz", BinaryRelPath: "bin/node", StripComponents: 1, Checksum: model.Checksum{Type: "sha256", Value: sum(tg)}})
		if job.State != "failed" {
			t.Fatalf("case %d: expected failure, got %+v", i, job)
		}
		if ents, _ := os.ReadDir(filepath.Join(e.home, "bin")); len(ents) != 0 {
			t.Fatalf("case %d: leftovers %v", i, ents)
		}
	}
}

func TestEntryTarget(t *testing.T) {
	dest := filepath.Join(os.TempDir(), "dest")
	if p, skip, err := entryTarget(dest, "top/bin/x", 1); err != nil || skip || p != filepath.Join(dest, "bin", "x") {
		t.Fatalf("%v %v %v", p, skip, err)
	}
	if _, skip, err := entryTarget(dest, "top/", 1); err != nil || !skip {
		t.Fatal("top dir should be skipped")
	}
	if _, _, err := entryTarget(dest, "C:/x", 0); err == nil {
		t.Fatal("drive letter must be rejected")
	}
}

func TestUnsupportedAndUnknown(t *testing.T) {
	e := newEnv(t, nil)
	e.in.Catalog = func() ([]model.CatalogEntry, error) {
		return []model.CatalogEntry{{ID: "nope", OS: "plan9", Arch: "mips"}}, nil
	}
	if _, err := e.in.Start("nope"); err == nil {
		t.Fatal("unsupported platform must be refused")
	}
	if _, err := e.in.Start("missing"); err == nil {
		t.Fatal("unknown id must be refused")
	}
}

func TestParseShasums(t *testing.T) {
	h := strings.Repeat("ab", 32)
	if got, err := ParseShasums(h+"\n", "whatever"); err != nil || got != h {
		t.Fatal("bare hash")
	}
	if got, err := ParseShasums("junk\n"+h+" *node.zip\n", "node.zip"); err != nil || got != h {
		t.Fatal("star-prefixed name")
	}
	if _, err := ParseShasums(h+"  a.zip", "b.zip"); err == nil {
		t.Fatal("wrong name must fail")
	}
}
