package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
	"zero2dev/internal/runtimes"
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

func TestSeveralVersionsSideBySide(t *testing.T) {
	e, _ := phpEnv(t)
	j1, err := e.in.Start("php-8.3.1")
	if err != nil {
		t.Fatal(err)
	}
	first := e.wait(t, j1.ID)
	j2, err := e.in.Start("php-8.4.0")
	if err != nil {
		t.Fatalf("a second version of the same runtime must be allowed: %v", err)
	}
	second := e.wait(t, j2.ID)
	if first.State != "done" || second.State != "done" {
		t.Fatalf("%+v %+v", first, second)
	}
	rs := e.rts.List()
	if len(rs) != 2 || rs[0].Kind != "php" || rs[1].Kind != "php" {
		t.Fatalf("both versions must be installed: %+v", rs)
	}
	for _, id := range []string{"php-8.3.1", "php-8.4.0"} {
		if _, err := os.Stat(filepath.Join(e.home, "bin", id, "php")); err != nil {
			t.Fatalf("%s missing: %v", id, err)
		}
	}
	defaults := 0
	for _, r := range rs {
		if r.Default {
			defaults++
			if r.ID != first.RuntimeID {
				t.Fatal("the first installed runtime of a kind becomes the default")
			}
		}
	}
	if defaults != 1 {
		t.Fatalf("exactly one default per kind, got %d", defaults)
	}
	if _, err := e.in.Start("php-8.4.0"); err == nil || apperr.From(err).Status != 409 {
		t.Fatalf("the same version twice must conflict, got %v", err)
	}
}

func TestDefaultSwitchAndPromotionOnRemove(t *testing.T) {
	e, _ := phpEnv(t)
	j1, _ := e.in.Start("php-8.3.1")
	a := e.wait(t, j1.ID)
	j2, _ := e.in.Start("php-8.4.0")
	b := e.wait(t, j2.ID)
	node := filepath.Join(t.TempDir(), "node")
	os.WriteFile(node, []byte("x"), 0o755)
	n, err := e.rts.Register("node", node, "20.0.0")
	if err != nil || !n.Default {
		t.Fatalf("the first runtime of another kind is its own default: %+v %v", n, err)
	}
	if got, err := e.rts.SetDefault(b.RuntimeID); err != nil || !got.Default || got.ID != b.RuntimeID {
		t.Fatalf("%+v %v", got, err)
	}
	def := map[string]string{}
	for _, r := range e.rts.List() {
		if r.Default {
			def[r.Kind] = r.ID
		}
	}
	if def["php"] != b.RuntimeID || def["node"] != n.ID || len(def) != 2 {
		t.Fatalf("setting a default must not touch other kinds: %v", def)
	}
	if _, err := e.rts.SetDefault("rt_nope"); apperr.From(err).Status != 404 {
		t.Fatalf("unknown id: %v", err)
	}
	// Removing the default promotes the other version of that kind.
	if err := e.rts.Delete(b.RuntimeID); err != nil {
		t.Fatal(err)
	}
	for _, r := range e.rts.List() {
		if r.ID == a.RuntimeID && !r.Default {
			t.Fatal("the remaining php must become the default")
		}
	}
	if _, err := os.Stat(filepath.Join(e.home, "bin", "php-8.4.0")); !os.IsNotExist(err) {
		t.Fatal("removed version folder must be deleted")
	}
	// A runtime that a service names cannot be removed.
	e.st.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{ID: "svc_1", Name: "legacy-site", Kind: "php", RuntimeID: a.RuntimeID})
		return nil
	})
	if err := e.rts.Delete(a.RuntimeID); err == nil || apperr.From(err).Status != 409 || !strings.Contains(err.Error(), "legacy-site") {
		t.Fatalf("want 409 naming the service, got %v", err)
	}
}

func TestExistingStateGetsDefaults(t *testing.T) {
	e, _ := phpEnv(t)
	e.st.Update(func(d *store.Data) error { // a 0.0.1 state file: runtimes without the default flag
		d.Runtimes = []model.Runtime{{ID: "rt_a", Kind: "node", Version: "18", Binary: "/x/a"}, {ID: "rt_b", Kind: "node", Version: "20", Binary: "/x/b"}, {ID: "rt_c", Kind: "go", Version: "1.23", Binary: "/x/c"}}
		return nil
	})
	rts := runtimes.New(e.home, e.st, nopEm{})
	got := map[string]bool{}
	for _, r := range rts.List() {
		got[r.ID] = r.Default
	}
	if !got["rt_a"] || got["rt_b"] || !got["rt_c"] {
		t.Fatalf("the first runtime of each kind (what services used before) becomes the default: %v", got)
	}
}

func TestRegisteredAndInstalledVersionsCoexist(t *testing.T) {
	e, _ := phpEnv(t)
	bin := filepath.Join(t.TempDir(), "php")
	os.WriteFile(bin, []byte("x"), 0o755)
	reg, err := e.rts.Register("php", bin, "7.1.33")
	if err != nil {
		t.Fatal(err)
	}
	j, err := e.in.Start("php-8.4.0")
	if err != nil || e.wait(t, j.ID).State != "done" {
		t.Fatalf("installing next to a registered runtime: %v", err)
	}
	rs := e.rts.List()
	if len(rs) != 2 {
		t.Fatalf("%+v", rs)
	}
	for _, r := range rs {
		if r.ID == reg.ID && !r.Default {
			t.Fatal("the registered runtime was first, so it is the default")
		}
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
	if b, err := os.ReadFile(filepath.Join(e.home, "bin", "nginx-1.26.2", "nginx")); err != nil || string(b) != "ng" {
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
