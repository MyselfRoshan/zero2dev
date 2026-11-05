package installer

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zero2dev/internal/catalog"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func logText(e *env) string { return strings.Join(e.in.LogTail(1000), "\n") }

// ---- rename retry and copy fallback ----

func TestRenameRetriesThenSucceeds(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy.exe": "C"})
	e := newEnv(t, map[string][]byte{"/c.zip": z})
	var calls int
	var sleeps []time.Duration
	e.in.Rename = func(a, b string) error {
		calls++
		if calls <= 5 {
			return errors.New("rename: Access is denied.")
		}
		return os.Rename(a, b)
	}
	e.in.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	job := e.install(t, model.CatalogEntry{ID: "c-1", URL: "/c.zip", BinaryRelPath: "caddy.exe", Checksum: model.Checksum{Type: "sha256", Value: sum(z)}})
	if job.State != "done" {
		t.Fatalf("%+v", job)
	}
	if calls != 6 || len(sleeps) != 5 {
		t.Fatalf("calls=%d sleeps=%v", calls, sleeps)
	}
	if sleeps[0] != 250*time.Millisecond {
		t.Fatalf("first delay %v", sleeps[0])
	}
	for i, d := range sleeps {
		if d > 2*time.Second || (i > 0 && d < sleeps[i-1]) {
			t.Fatalf("delays must grow and cap at 2s: %v", sleeps)
		}
	}
	if !strings.Contains(logText(e), "rename attempt 5/20 failed") {
		t.Fatal("rename failures must be logged")
	}
}

func TestRenameDelaysCap(t *testing.T) {
	if renameDelay(0) != 250*time.Millisecond || renameDelay(7) != 2*time.Second || renameDelay(19) != 2*time.Second {
		t.Fatal("renameDelay")
	}
}

func TestRenamePermanentFailureFallsBackToCopy(t *testing.T) {
	tg := makeTarGz(t, []tarFile{{name: "bin/node", body: "ELF", mode: 0o755}, {name: "lib/a.txt", body: "a", mode: 0o644}})
	e := newEnv(t, map[string][]byte{"/p.tar.gz": tg})
	calls := 0
	e.in.Rename = func(a, b string) error { calls++; return errors.New("Access is denied.") }
	job := e.install(t, model.CatalogEntry{ID: "cp-1", URL: "/p.tar.gz", BinaryRelPath: "bin/node", Checksum: model.Checksum{Type: "sha256", Value: sum(tg)}})
	if job.State != "done" || calls != e.in.RenameAttempts {
		t.Fatalf("calls=%d job=%+v", calls, job)
	}
	final := filepath.Join(e.home, "runtimes", "cp-1")
	if b, err := os.ReadFile(filepath.Join(final, "lib", "a.txt")); err != nil || string(b) != "a" {
		t.Fatalf("copied tree missing: %v", err)
	}
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(filepath.Join(final, "bin", "node")); err != nil || st.Mode()&0o111 == 0 {
			t.Fatal("exec bit lost in copy")
		}
	}
	if ents, _ := os.ReadDir(filepath.Join(e.home, "tmp")); len(ents) != 0 {
		t.Fatalf("staging not removed: %v", ents)
	}
	if !strings.Contains(logText(e), "falling back to copy") {
		t.Fatal("fallback must be logged")
	}
}

// ---- HTTP hardening ----

func TestUserAgentRetryAndFinalURL(t *testing.T) {
	z := makeZip(t, map[string]string{"node.exe": "B"})
	var hits, uaOK atomic.Int32
	var mu sync.Mutex
	e := newEnvHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "Zero2Dev/9.9.9" {
			uaOK.Add(1)
		}
		switch r.URL.Path {
		case "/old.zip": // redirect, to prove the final URL is logged
			http.Redirect(w, r, "/final.zip", http.StatusFound)
		case "/final.zip":
			mu.Lock()
			n := hits.Add(1)
			mu.Unlock()
			if n <= 2 { // drop the connection twice mid-response
				hj := w.(http.Hijacker)
				c, _, _ := hj.Hijack()
				c.Close()
				return
			}
			w.Write(z)
		}
	}))
	e.in.Version = "9.9.9"
	// fresh connection per request, so the server-side hit count equals attempts
	e.in.Client = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	var sleeps []time.Duration
	e.in.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
	job := e.install(t, model.CatalogEntry{ID: "r-1", URL: "/old.zip", BinaryRelPath: "node.exe", Checksum: model.Checksum{Type: "sha256", Value: sum(z)}})
	if job.State != "done" || hits.Load() != 3 {
		t.Fatalf("hits=%d job=%+v", hits.Load(), job)
	}
	if len(sleeps) != 2 || sleeps[0] != time.Second || sleeps[1] != 3*time.Second {
		t.Fatalf("backoff %v", sleeps)
	}
	if uaOK.Load() != 6 {
		t.Fatalf("User-Agent sent on %d requests", uaOK.Load())
	}
	lg := logText(e)
	if !strings.Contains(lg, "final URL "+e.srv.URL+"/final.zip") || !strings.Contains(lg, "retry 1") {
		t.Fatalf("log lacks final url / retry:\n%s", lg)
	}
}

func TestHTTP404NotRetriedAndErrorNamesStepStatusURL(t *testing.T) {
	var hits atomic.Int32
	e := newEnvHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	job := e.install(t, model.CatalogEntry{ID: "n-1", URL: "/missing.zip", BinaryRelPath: "node.exe", Checksum: model.Checksum{Type: "sha256", Value: strings.Repeat("a", 64)}})
	if job.State != "failed" || hits.Load() != 1 {
		t.Fatalf("hits=%d job=%+v", hits.Load(), job)
	}
	for _, want := range []string{"Downloading: ", "HTTP 404 Not Found", e.srv.URL + "/missing.zip"} {
		if !strings.Contains(job.Error, want) {
			t.Errorf("error %q lacks %q", job.Error, want)
		}
	}
}

func TestNetworkErrorGivesUpAfterThreeAttempts(t *testing.T) {
	var hits atomic.Int32
	e := newEnvHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		c, _, _ := w.(http.Hijacker).Hijack()
		c.Close()
	}))
	job := e.install(t, model.CatalogEntry{ID: "g-1", URL: "/x.zip", BinaryRelPath: "node.exe", Checksum: model.Checksum{Type: "sha256", Value: strings.Repeat("a", 64)}})
	if job.State != "failed" || hits.Load() != 3 || !strings.HasPrefix(job.Error, "Downloading: Get \"") {
		t.Fatalf("hits=%d job=%+v", hits.Load(), job)
	}
}

func TestStallDetection(t *testing.T) {
	var hits atomic.Int32
	e := newEnvHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Length", "1000000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-r.Context().Done() // then send nothing
	}))
	e.in.StallTimeout = 150 * time.Millisecond
	job := e.install(t, model.CatalogEntry{ID: "s-1", URL: "/slow.zip", BinaryRelPath: "node.exe", Checksum: model.Checksum{Type: "sha256", Value: strings.Repeat("a", 64)}})
	if job.State != "failed" || !strings.Contains(job.Error, "no data received") || hits.Load() != 3 {
		t.Fatalf("hits=%d job=%+v", hits.Load(), job)
	}
}

func TestValidateProxy(t *testing.T) {
	for _, ok := range []string{"", "  ", "http://proxy:8080", "https://u:p@proxy.corp:3128", "http://10.0.0.1:80/"} {
		if _, err := ValidateProxy(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"proxy:8080", "ftp://p:1", "http://", "http://p:99999", "http://p:80/path", "socks5://p:1", "not a url", "http://p:80?x=1"} {
		if _, err := ValidateProxy(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestDownloadUsesConfiguredProxy(t *testing.T) {
	// A request through an HTTP proxy arrives at the proxy with an absolute URL.
	var seen atomic.Value
	proxy := newEnvHandler(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.URL.String())
		http.Error(w, "proxy says no", http.StatusForbidden)
	}))
	e := newEnv(t, nil)
	e.st.Update(func(d *store.Data) error { d.Settings.DownloadProxy = proxy.srv.URL; return nil })
	// 127.0.0.1 is normally exempt only from *environment* proxies; an explicit proxy is always used.
	entry := model.CatalogEntry{ID: "px-1", URL: "/a.zip", BinaryRelPath: "x", Checksum: model.Checksum{Type: "sha256", Value: strings.Repeat("a", 64)}}
	job := e.install(t, entry)
	if job.State != "failed" || !strings.Contains(job.Error, "HTTP 403") {
		t.Fatalf("%+v", job)
	}
	if u, _ := seen.Load().(string); !strings.HasSuffix(u, "/a.zip") || !strings.HasPrefix(u, "http://") {
		t.Fatalf("proxy saw %q", u)
	}
}

// ---- installer.log ----

func TestInstallerLogOnFailure(t *testing.T) {
	z := makeZip(t, map[string]string{"node.exe": "B"})
	bad := strings.Repeat("0", 64)
	e := newEnv(t, map[string][]byte{"/p.zip": z})
	job := e.install(t, model.CatalogEntry{ID: "l-1", URL: "/p.zip", BinaryRelPath: "node.exe", Checksum: model.Checksum{Type: "sha256", Value: bad}})
	if job.State != "failed" || !strings.HasPrefix(job.Error, "Verifying: checksum mismatch") {
		t.Fatalf("%+v", job)
	}
	lg := logText(e)
	for _, want := range []string{"install l-1", e.srv.URL + "/p.zip", "HTTP 200", "final URL", "step: Verifying", "expected=" + bad, "got=" + sum(z), "FAILED: Verifying: checksum mismatch", "downloaded "} {
		if !strings.Contains(lg, want) {
			t.Errorf("installer.log lacks %q:\n%s", want, lg)
		}
	}
	for _, l := range e.in.LogTail(1000) {
		if _, err := time.Parse(time.RFC3339, strings.Fields(l)[0]); err != nil {
			t.Fatalf("line must start with RFC3339 timestamp: %q", l)
		}
	}
	if got := e.in.LogTail(2); len(got) != 2 {
		t.Fatalf("tail(2) = %v", got)
	}
}

func TestInstallerLogRotation(t *testing.T) {
	l := &fileLog{path: filepath.Join(t.TempDir(), "logs", "installer.log")}
	line := strings.Repeat("x", 1000)
	for i := 0; i < 1200; i++ {
		l.printf("job", "%s", line)
	}
	if _, err := os.Stat(l.path + ".1"); err != nil {
		t.Fatal("rotation file missing")
	}
	if st, _ := os.Stat(l.path); st.Size() > maxInstallerLog {
		t.Fatalf("log too large: %d", st.Size())
	}
	if len(l.Tail(5000)) < 1100 {
		t.Fatal("tail should span the rotated file")
	}
}

// ---- import ----

func importEnv(t *testing.T, z []byte, mode string) (*env, model.CatalogEntry, string) {
	t.Helper()
	routes := map[string][]byte{}
	entry := model.CatalogEntry{ID: "imp-1", URL: "/dl/caddy_x_linux_amd64.zip", BinaryRelPath: "caddy", Checksum: model.Checksum{Type: "shasums", URL: "/dl/sums.txt"}, Homepage: "https://example.test/dl"}
	switch mode {
	case "sums":
		routes["/dl/sums.txt"] = []byte(sum(z) + "  caddy_x_linux_amd64.zip\n")
	case "nosums": // checksum file 404s
	}
	e := newEnv(t, routes)
	entry = e.entry(entry)
	file := filepath.Join(t.TempDir(), "caddy_x_linux_amd64.zip")
	if err := os.WriteFile(file, z, 0o644); err != nil {
		t.Fatal(err)
	}
	return e, entry, file
}

func TestImportLocalZipWithGivenHash(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy": "BIN", "LICENSE": "l"})
	e, entry, file := importEnv(t, z, "nosums")
	j, err := e.in.Import(entry.ID, file, strings.ToUpper(sum(z)))
	if err != nil {
		t.Fatal(err)
	}
	if j.Type != "import" || j.Step != "Importing" {
		t.Fatalf("%+v", j)
	}
	if got := e.wait(t, j.ID); got.State != "done" || got.RuntimeID == "" {
		t.Fatalf("%+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "runtimes", "imp-1", "caddy")); string(b) != "BIN" {
		t.Fatal("not extracted")
	}
	if b, _ := os.ReadFile(file); len(b) != len(z) {
		t.Fatal("user's file must be untouched")
	}
	if ents, _ := os.ReadDir(filepath.Join(e.home, "tmp")); len(ents) != 0 {
		t.Fatalf("tmp not cleaned: %v", ents)
	}
	if rs := e.rts.List(); len(rs) != 1 || rs[0].Source != "installed" {
		t.Fatalf("%+v", rs)
	}
	if !strings.Contains(logText(e), "importing "+file) {
		t.Fatal("import not logged")
	}
	// now installed -> 409
	if _, err := e.in.Import(entry.ID, file, sum(z)); err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestImportWithPublishedChecksum(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy": "BIN"})
	e, entry, file := importEnv(t, z, "sums")
	j, err := e.in.Import(entry.ID, file, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := e.wait(t, j.ID); got.State != "done" {
		t.Fatalf("%+v", got)
	}
}

func TestImportWrongHash(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy": "BIN"})
	e, entry, file := importEnv(t, z, "nosums")
	j, err := e.in.Import(entry.ID, file, strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	got := e.wait(t, j.ID)
	if got.State != "failed" || !strings.Contains(got.Error, "Verifying: checksum mismatch") {
		t.Fatalf("%+v", got)
	}
	if ents, _ := os.ReadDir(filepath.Join(e.home, "runtimes")); len(ents) != 0 {
		t.Fatalf("nothing may be extracted: %v", ents)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("user's file must survive a failed import")
	}
	// a failed job frees the entry for a retry
	j2, err := e.in.Import(entry.ID, file, sum(z))
	if err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	e.wait(t, j2.ID)
}

func TestImportValidation(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy": "BIN"})
	e, entry, file := importEnv(t, z, "nosums") // published checksum is a 404
	dir := t.TempDir()
	wrongName := filepath.Join(dir, "something-else.zip")
	os.WriteFile(wrongName, z, 0o644)
	cases := []struct{ name, path, sha, want string }{
		{"relative path", "caddy.zip", "", "absolute"},
		{"empty path", "", "", "absolute"},
		{"missing file", filepath.Join(dir, "caddy_x_linux_amd64.zip"), "", "does not exist"},
		{"directory", dir, "", "not a file"},
		{"wrong name", wrongName, sum(z), "must be named caddy_x_linux_amd64.zip"},
		{"malformed sha", file, "abc", "SHA-512 (128) hexadecimal"},
		{"no checksum available", file, "", "Cannot verify the file because the published checksum could not be fetched. Paste the SHA-256 or SHA-512 for caddy_x_linux_amd64.zip from " + e.srv.URL + "/dl/sums.txt into the checksum field."},
	}
	for _, c := range cases {
		_, err := e.in.Import(entry.ID, c.path, c.sha)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want containing %q", c.name, err, c.want)
		}
	}
	if _, err := e.in.Import("nope", file, ""); err == nil {
		t.Error("unknown catalog id")
	}
	// none of the rejected requests may leave a running job behind
	j, err := e.in.Import(entry.ID, file, sum(z))
	if err != nil {
		t.Fatalf("a valid import must still be accepted: %v", err)
	}
	e.wait(t, j.ID)
}

func TestImportConflictWhileRunning(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy": "BIN"})
	e, entry, file := importEnv(t, z, "nosums")
	release := make(chan struct{})
	e.in.Rename = func(a, b string) error { <-release; return os.Rename(a, b) }
	j, err := e.in.Import(entry.ID, file, sum(z))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.in.Import(entry.ID, file, sum(z)); err == nil || !strings.Contains(err.Error(), "already being installed") {
		t.Fatalf("want conflict, got %v", err)
	}
	close(release)
	e.wait(t, j.ID)
}

// ---- relocation ----

func TestMovedHomeKeepsRuntimesAndCatalogFlags(t *testing.T) {
	z := makeZip(t, map[string]string{"caddy": "BIN"})
	e, entry, file := importEnv(t, z, "nosums")
	entry.Checksum = model.Checksum{Type: "sha256", Value: sum(z)}
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{entry}, nil }
	sj, err := e.in.Import(entry.ID, file, sum(z))
	if err != nil {
		t.Fatal(err)
	}
	if job := e.wait(t, sj.ID); job.State != "done" {
		t.Fatalf("%+v", job)
	}
	// also a registered runtime outside the folder: must keep its path
	outside := filepath.Join(t.TempDir(), "mynode")
	os.WriteFile(outside, []byte("x"), 0o755)
	if _, err := e.rts.Register("node", outside, "1.0.0"); err != nil {
		t.Fatal(err)
	}
	oldBinary := e.st.Snapshot().Runtimes[0].Binary

	moved := filepath.Join(t.TempDir(), "new", "location", "data")
	os.MkdirAll(filepath.Dir(moved), 0o755)
	if err := os.Rename(e.home, moved); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(moved, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Snapshot().Runtimes[0].Binary != oldBinary {
		t.Fatal("precondition: still the old path before rebase")
	}
	n, err := st.RebaseRuntimes(moved)
	if err != nil || n != 1 {
		t.Fatalf("rebase n=%d err=%v", n, err)
	}
	var installed, registered model.Runtime
	for _, r := range st.Snapshot().Runtimes {
		if r.Source == "installed" {
			installed = r
		} else {
			registered = r
		}
	}
	want := filepath.Join(moved, "runtimes", "imp-1", "caddy")
	if installed.Binary != want {
		t.Fatalf("binary %q, want %q", installed.Binary, want)
	}
	if _, err := os.Stat(installed.Binary); err != nil {
		t.Fatalf("rebased binary must exist: %v", err)
	}
	if registered.Binary != outside {
		t.Fatalf("registered runtime must not be rewritten: %q", registered.Binary)
	}
	es := catalog.Annotate([]model.CatalogEntry{entry}, moved, st.Snapshot().Runtimes)
	if !es[0].Installed {
		t.Fatal("catalog installed flag must survive the move")
	}
	// persisted: a fresh Open sees the rebased value and a second rebase is a no-op
	st2, _ := store.Open(filepath.Join(moved, "state.json"))
	if st2.Snapshot().Runtimes[0].Binary != want && st2.Snapshot().Runtimes[1].Binary != want {
		t.Fatal("rebase not persisted")
	}
	if n, _ := st2.RebaseRuntimes(moved); n != 0 {
		t.Fatalf("second rebase changed %d", n)
	}
}
