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
