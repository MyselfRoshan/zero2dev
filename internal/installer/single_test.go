package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// phpEnv serves two fake "PHP" archives and an unverified "nginx" one.
func phpEnv(t *testing.T) (*env, map[string]model.CatalogEntry) {
	t.Helper()
	z1 := makeZip(t, map[string]string{"php": "v1"})
	z2 := makeZip(t, map[string]string{"php": "v2"})
	zn := makeZip(t, map[string]string{"top/nginx": "ng"})
	e := newEnv(t, map[string][]byte{"/php1.zip": z1, "/php2.zip": z2, "/nginx.zip": zn})
	mk := func(id, kind, ver, url, bin string, strip int, ck model.Checksum, unv bool) model.CatalogEntry {
		if ck.Type == "sha256" {
			ck.Value = sum(map[string][]byte{"/php1.zip": z1, "/php2.zip": z2}[url])
		}
		return model.CatalogEntry{ID: id, Kind: kind, Version: ver, Name: kind + " " + ver, URL: e.srv.URL + url, Checksum: ck,
			BinaryRelPath: bin, StripComponents: strip, OS: runtime.GOOS, Arch: runtime.GOARCH, Unverified: unv}
	}
	es := map[string]model.CatalogEntry{
		"p1": mk("php-8.3.1", "php", "8.3.1", "/php1.zip", "php", 0, model.Checksum{Type: "sha256"}, false),
		"p2": mk("php-8.4.0", "php", "8.4.0", "/php2.zip", "php", 0, model.Checksum{Type: "sha256"}, false),
		"n1": mk("nginx-1.26.2", "nginx", "1.26.2", "/nginx.zip", "nginx", 1, model.Checksum{Type: "none"}, true),
	}
	e.in.Catalog = func() ([]model.CatalogEntry, error) {
		return []model.CatalogEntry{es["p1"], es["p2"], es["n1"]}, nil
	}
	return e, es
}

func code(err error) string {
	if err == nil {
		return ""
	}
	return apperr.From(err).Code
}

func TestSingleInstallRefusesSecondVersionUnlessReplace(t *testing.T) {
	e, _ := phpEnv(t)
	j, err := e.in.Start("php-8.3.1")
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j.ID); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	_, err = e.in.Start("php-8.4.0")
	if code(err) != "single_install" || apperr.From(err).Status != 409 {
		t.Fatalf("want 409 single_install, got %v", err)
	}
	d := apperr.From(err).Details
	if d["version"] != "8.3.1" || d["runtimeId"] == "" || !strings.Contains(err.Error(), "Only one php version") {
		t.Fatalf("details %v / %v", d, err)
	}
	if len(e.rts.List()) != 1 {
		t.Fatal("nothing may change on a refused install")
	}
	// Import is checked the same way.
	if _, err := e.in.Import("php-8.4.0", filepath.Join(e.home, "php2.zip"), ""); code(err) == "single_install" {
		t.Fatal("import must validate its file before anything else here")
	}
}

func TestReplaceSwitchesVersionAndRepointsServices(t *testing.T) {
	e, _ := phpEnv(t)
	j, _ := e.in.Start("php-8.3.1")
	first := e.wait(t, j.ID)
	oldDir := filepath.Join(e.home, "runtimes", "php-8.3.1")
	if _, err := os.Stat(oldDir); err != nil {
		t.Fatal(err)
	}
	e.st.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{ID: "svc_1", Name: "api", Kind: "php", RuntimeID: first.RuntimeID},
			model.Service{ID: "svc_2", Name: "other", Kind: "node", RuntimeID: "rt_keep"})
		return nil
	})

	j2, err := e.in.Start("php-8.4.0", Options{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	done := e.wait(t, j2.ID)
	if done.State != "done" || done.ReplacedRuntimeID != first.RuntimeID || done.RuntimeID == first.RuntimeID {
		t.Fatalf("%+v", done)
	}
	rs := e.rts.List()
	if len(rs) != 1 || rs[0].ID != done.RuntimeID || rs[0].CatalogID != "php-8.4.0" || rs[0].Version != "8.4.0" {
		t.Fatalf("exactly the new runtime must remain: %+v", rs)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("old version folder must be deleted")
	}
	b, _ := os.ReadFile(filepath.Join(e.home, "runtimes", "php-8.4.0", "php"))
	if string(b) != "v2" {
		t.Fatalf("new binary %q", b)
	}
	svc := e.st.Snapshot().Services
	if svc[0].RuntimeID != done.RuntimeID || svc[1].RuntimeID != "rt_keep" {
		t.Fatalf("only services of the old runtime are re-pointed: %+v", svc)
	}
	// And back again.
	j3, err := e.in.Start("php-8.3.1", Options{Replace: true})
	if err != nil || e.wait(t, j3.ID).State != "done" || len(e.rts.List()) != 1 {
		t.Fatalf("switching back failed: %v %+v", err, e.rts.List())
	}
}

func TestReplaceBlockedWhileServiceRuns(t *testing.T) {
	e, _ := phpEnv(t)
	j, _ := e.in.Start("php-8.3.1")
	first := e.wait(t, j.ID)
	e.st.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{ID: "svc_1", Name: "web", Kind: "php", RuntimeID: first.RuntimeID})
		return nil
	})
	e.rts.ServiceActive = func(id string) bool { return id == "svc_1" } // the supervisor says it is running
	_, err := e.in.Start("php-8.4.0", Options{Replace: true})
	if code(err) != "in_use" || apperr.From(err).Details["service"] != "web" {
		t.Fatalf("want in_use naming the service, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(e.home, "runtimes", "php-8.4.0")); !os.IsNotExist(statErr) {
		t.Fatal("nothing may be downloaded or installed when refused")
	}
}

func TestFailedReplaceKeepsOldVersion(t *testing.T) {
	e, es := phpEnv(t)
	j, _ := e.in.Start("php-8.3.1")
	first := e.wait(t, j.ID)
	bad := es["p2"]
	bad.Checksum.Value = strings.Repeat("0", 64) // wrong hash: install must fail
	e.in.Catalog = func() ([]model.CatalogEntry, error) { return []model.CatalogEntry{es["p1"], bad}, nil }
	j2, err := e.in.Start("php-8.4.0", Options{Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j2.ID); done.State != "failed" || !strings.Contains(done.Error, "checksum mismatch") {
		t.Fatalf("%+v", done)
	}
	rs := e.rts.List()
	if len(rs) != 1 || rs[0].ID != first.RuntimeID {
		t.Fatalf("old runtime must be untouched: %+v", rs)
	}
	if _, err := os.Stat(filepath.Join(e.home, "runtimes", "php-8.3.1", "php")); err != nil {
		t.Fatal("old files must be untouched")
	}
}

func TestRegisteredRuntimesDoNotCountForSingleInstall(t *testing.T) {
	e, _ := phpEnv(t)
	bin := filepath.Join(t.TempDir(), "php")
	os.WriteFile(bin, []byte("x"), 0o755)
	if _, err := e.rts.Register("php", bin, "7.4.0"); err != nil {
		t.Fatal(err)
	}
	j, err := e.in.Start("php-8.3.1")
	if err != nil || e.wait(t, j.ID).State != "done" {
		t.Fatalf("a runtime registered by path must not block installing: %v", err)
	}
	// Replacing removes only the installed one.
	j2, _ := e.in.Start("php-8.4.0", Options{Replace: true})
	e.wait(t, j2.ID)
	rs := e.rts.List()
	if len(rs) != 2 {
		t.Fatalf("registered runtime must stay: %+v", rs)
	}
}

func TestUnverifiedNeedsConfirmationAndRecordsHash(t *testing.T) {
	e, _ := phpEnv(t)
	_, err := e.in.Start("nginx-1.26.2")
	if code(err) != "unverified" || apperr.From(err).Status != 400 {
		t.Fatalf("want 400 unverified, got %v", err)
	}
	j, err := e.in.Start("nginx-1.26.2", Options{AcceptUnverified: true})
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j.ID); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "runtimes", "nginx-1.26.2", "nginx")); err != nil || string(b) != "ng" {
		t.Fatalf("strip-1 extraction failed: %v %q", err, b)
	}
	log := strings.Join(e.in.LogTail(50), "\n")
	if !strings.Contains(log, "UNVERIFIED") || !strings.Contains(log, "sha256=") {
		t.Fatalf("the computed hash must be recorded in installer.log:\n%s", log)
	}
}

func TestUnverifiedImportWithPastedHash(t *testing.T) {
	e, es := phpEnv(t)
	zn := makeZip(t, map[string]string{"top/nginx": "ng"})
	file := filepath.Join(t.TempDir(), "nginx.zip")
	os.WriteFile(file, zn, 0o644)
	// no hash, no confirmation: refused
	if _, err := e.in.Import(es["n1"].ID, file, ""); code(err) != "unverified" {
		t.Fatalf("got %v", err)
	}
	// wrong pasted hash: verified path fails closed
	j, err := e.in.Import(es["n1"].ID, file, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t, j.ID); done.State != "failed" || !strings.Contains(done.Error, "checksum mismatch") {
		t.Fatalf("%+v", done)
	}
	// right pasted hash: installs without needing acceptUnverified
	j, err = e.in.Import(es["n1"].ID, file, sum(zn))
	if err != nil || e.wait(t, j.ID).State != "done" {
		t.Fatalf("pasted hash must verify: %v", err)
	}
}

func TestSimultaneousInstallsOfOneSingleKindAreRefused(t *testing.T) {
	e, _ := phpEnv(t)
	j, err := e.in.Start("php-8.3.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.in.Start("php-8.4.0", Options{Replace: true}); err == nil || apperr.From(err).Status != 409 {
		// the first job may already be finished on a very fast machine
		if cur, _ := e.in.Get(j.ID); cur.State == "running" {
			t.Fatalf("second concurrent php install must conflict, got %v", err)
		}
	}
	e.wait(t, j.ID)
}
