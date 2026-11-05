package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
)

func TestStoreRoundTripAndAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot().Settings.OpenBrowserOnStart || s.Snapshot().Settings.ProxyHTTPPort != 8080 {
		t.Fatal("defaults missing")
	}
	err = s.Update(func(d *Data) error {
		d.Services = append(d.Services, model.Service{ID: "svc_1", Name: "a", Port: 3000})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// no temp files left behind
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("expected only state.json, got %v", ents)
	}
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	sv := s2.Snapshot().Services
	if len(sv) != 1 || sv[0].ID != "svc_1" || sv[0].Args == nil || sv[0].Env == nil || sv[0].State != "stopped" {
		t.Fatalf("bad reload: %+v", sv)
	}
}

func TestUpdateRollsBackOnError(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "state.json"))
	boom := errors.New("boom")
	err := s.Update(func(d *Data) error {
		d.Sites = append(d.Sites, model.Site{ID: "x"})
		return boom
	})
	if !errors.Is(err, boom) || len(s.Snapshot().Sites) != 0 {
		t.Fatal("failed update must not apply")
	}
}

func TestCorruptFileIsError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(p, []byte("{not json"), 0o600)
	if _, err := Open(p); err == nil || !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("want corrupt error, got %v", err)
	}
}

func TestRebasePath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "new")
	want := filepath.Join(home, "runtimes", "node-22", "bin", "node")
	for _, in := range []string{
		`C:\Users\me\.zero2dev\runtimes\node-22\bin\node`,
		"/old/place/data/runtimes/node-22/bin/node",
		`D:\z\bin\data\runtimes\node-22/bin/node`, // mixed separators
	} {
		if got := RebasePath(home, in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	if got := RebasePath(home, "/usr/bin/node"); got != "/usr/bin/node" {
		t.Errorf("no runtimes segment must be left alone: %q", got)
	}
	// "runtimes" appearing earlier in the path: first segment wins when it resolves,
	// otherwise the last one is tried.
	os.MkdirAll(filepath.Join(home, "runtimes", "x"), 0o755)
	os.WriteFile(filepath.Join(home, "runtimes", "x", "bin"), []byte("b"), 0o755)
	if got := RebasePath(home, "/srv/runtimes/app/data/runtimes/x/bin"); got != filepath.Join(home, "runtimes", "x", "bin") {
		t.Errorf("nested runtimes segment: %q", got)
	}
}

func TestRebaseRuntimesOnlyTouchesMissingInstalled(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	os.MkdirAll(filepath.Join(home, "runtimes", "a"), 0o755)
	existing := filepath.Join(home, "runtimes", "a", "bin")
	os.WriteFile(existing, []byte("x"), 0o755)
	outside := filepath.Join(dir, "runtimes", "registered") // path contains "runtimes" but is registered
	s, _ := Open(filepath.Join(home, "state.json"))
	s.Update(func(d *Data) error {
		d.Runtimes = []model.Runtime{
			{ID: "1", Source: "installed", Binary: existing},
			{ID: "2", Source: "installed", Binary: `C:\old\runtimes\b\bin.exe`},
			{ID: "3", Source: "registered", Binary: outside},
		}
		return nil
	})
	n, err := s.RebaseRuntimes(home)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	rs := s.Snapshot().Runtimes
	if rs[0].Binary != existing || rs[1].Binary != filepath.Join(home, "runtimes", "b", "bin.exe") || rs[2].Binary != outside {
		t.Fatalf("%+v", rs)
	}
}
