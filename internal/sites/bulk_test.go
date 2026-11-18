package sites

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// bulkMgr is a manager with a running Caddy service (so every Regenerate
// restarts it, which the fake supervisor counts) and four sites: a and b on,
// c and d off.
func bulkMgr(t *testing.T) (*Manager, *store.Store, *fakeSup, string) {
	t.Helper()
	m, st, sup, home := newMgr(t)
	if err := st.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{ID: "svc_c", Name: "Caddy", Kind: "caddy"})
		for _, s := range []struct {
			id   string
			port int
			on   bool
		}{{"a", 4001, true}, {"b", 4002, true}, {"c", 4003, false}, {"d", 4004, false}} {
			d.Sites = append(d.Sites, model.Site{ID: s.id, Name: s.id, Domain: s.id + ".localhost", Type: "proxy", UpstreamPort: s.port, Enabled: s.on})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sup.setState("running")
	return m, st, sup, home
}

func enabledIDs(st *store.Store) string {
	var on []string
	for _, s := range st.Snapshot().Sites {
		if s.Enabled {
			on = append(on, s.ID)
		}
	}
	return strings.Join(on, ",")
}

// settle waits for the background restart(s) of Regenerate and a little longer,
// so a second, unwanted restart would be counted too.
func settle(sup *fakeSup, want int) int {
	for i := 0; i < 200 && sup.count() < want; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	return sup.count()
}

func TestSetEnabledManyAllRestartsOnce(t *testing.T) {
	m, st, sup, home := bulkMgr(t)

	n, err := m.SetEnabledMany(nil, true)
	if err != nil || n != 2 { // c and d changed; a and b were on already
		t.Fatalf("enable all: changed %d, err %v", n, err)
	}
	if got := enabledIDs(st); got != "a,b,c,d" {
		t.Fatalf("enabled = %s", got)
	}
	if got := settle(sup, 1); got != 1 {
		t.Fatalf("site server restarted %d times, want exactly 1", got)
	}
	b, _ := os.ReadFile(filepath.Join(home, "config", "Caddyfile"))
	for _, d := range []string{"a.localhost", "b.localhost", "c.localhost", "d.localhost"} {
		if !strings.Contains(string(b), d) {
			t.Errorf("Caddyfile lacks %s:\n%s", d, b)
		}
	}

	n, err = m.SetEnabledMany(nil, false)
	if err != nil || n != 4 {
		t.Fatalf("disable all: changed %d, err %v", n, err)
	}
	if got := enabledIDs(st); got != "" {
		t.Fatalf("enabled = %q", got)
	}
	if got := settle(sup, 2); got != 2 {
		t.Fatalf("after disable-all the server was restarted %d times in total, want 2", got)
	}
}

func TestSetEnabledManyNoChangeWritesAndRestartsNothing(t *testing.T) {
	m, _, sup, _ := bulkMgr(t)
	n, err := m.SetEnabledMany(nil, true)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	settle(sup, 1)
	cfg, _ := os.ReadFile(m.CaddyfilePath())
	mod := func() time.Time { fi, _ := os.Stat(m.CaddyfilePath()); return fi.ModTime() }()

	for _, tc := range []struct {
		name string
		ids  []string
	}{{"all already on", nil}, {"named already on", []string{"a", "b", "c"}}, {"empty list", []string{}}, {"unknown ids", []string{"zzz"}}} {
		if n, err := m.SetEnabledMany(tc.ids, true); err != nil || n != 0 {
			t.Fatalf("%s: changed %d, err %v", tc.name, n, err)
		}
	}
	if got := settle(sup, 1); got != 1 {
		t.Fatalf("no-op calls restarted the server (%d restarts)", got)
	}
	if now, _ := os.ReadFile(m.CaddyfilePath()); string(now) != string(cfg) {
		t.Fatal("Caddyfile changed")
	}
	if fi, _ := os.Stat(m.CaddyfilePath()); !fi.ModTime().Equal(mod) {
		t.Fatal("Caddyfile was rewritten by a no-op")
	}
}

func TestSetEnabledManySubset(t *testing.T) {
	m, st, sup, _ := bulkMgr(t)

	// a is already on, c is off, "zzz" does not exist: only c counts
	n, err := m.SetEnabledMany([]string{"a", "c", "zzz"}, true)
	if err != nil || n != 1 {
		t.Fatalf("changed %d, err %v", n, err)
	}
	if got := enabledIDs(st); got != "a,b,c" {
		t.Fatalf("enabled = %s", got)
	}
	if got := settle(sup, 1); got != 1 {
		t.Fatalf("restarts = %d, want 1", got)
	}

	n, err = m.SetEnabledMany([]string{"a", "b"}, false)
	if err != nil || n != 2 {
		t.Fatalf("changed %d, err %v", n, err)
	}
	if got := enabledIDs(st); got != "c" {
		t.Fatalf("enabled = %s", got)
	}
	if got := settle(sup, 2); got != 2 {
		t.Fatalf("restarts = %d, want 2", got)
	}
}

func TestSetEnabledManyStoppedServerIsNotRestarted(t *testing.T) {
	m, _, sup, _ := bulkMgr(t)
	sup.setState("stopped")
	if n, err := m.SetEnabledMany(nil, true); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := sup.count(); got != 0 {
		t.Fatalf("a stopped site server must not be restarted (%d)", got)
	}
}
