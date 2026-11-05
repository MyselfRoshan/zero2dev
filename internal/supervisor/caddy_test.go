package supervisor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/store"
)

// dumpEnv starts the helper as a service and returns the env it saw.
func dumpEnv(t *testing.T, s *Supervisor, st *store.Store, id string) map[string]string {
	t.Helper()
	dump := filepath.Join(t.TempDir(), "env.txt")
	st.Update(func(d *store.Data) error {
		d.Services[0].Env["Z2D_HELPER"] = "dumpenv"
		d.Services[0].Env["Z2D_DUMP"] = dump
		// user-provided values must be overridden for caddy
		d.Services[0].Env["XDG_DATA_HOME"] = "/evil/data"
		d.Services[0].Env["TMPDIR"] = "/evil/tmp"
		return nil
	})
	if err := s.Start(id); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, "running", 5*time.Second)
	var b []byte
	for i := 0; i < 100 && len(b) == 0; i++ {
		b, _ = os.ReadFile(dump)
		time.Sleep(20 * time.Millisecond)
	}
	out := map[string]string{}
	for _, kv := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		t.Fatal("helper did not dump its environment")
	}
	s.StopWait(id)
	return out
}

func TestCaddyServiceEnvIsolated(t *testing.T) {
	for _, mode := range []string{"kind", "binary-name"} {
		s, st, _, id := setup(t, "idle", 0)
		if mode == "kind" {
			st.Update(func(d *store.Data) error { d.Services[0].Kind = "caddy"; return nil })
		} else {
			if runtime.GOOS == "windows" {
				t.Skip("symlink naming trick")
			}
			link := filepath.Join(t.TempDir(), "caddy") // custom service whose binary is called caddy
			if err := os.Symlink(os.Args[0], link); err != nil {
				t.Skip(err)
			}
			st.Update(func(d *store.Data) error { d.Services[0].Command = link; return nil })
		}
		env := dumpEnv(t, s, st, id)
		want := map[string]string{
			"XDG_DATA_HOME":   filepath.Join(s.home, "caddy", "data"),
			"XDG_CONFIG_HOME": filepath.Join(s.home, "caddy", "config"),
			"TMP":             filepath.Join(s.home, "tmp"),
			"TEMP":            filepath.Join(s.home, "tmp"),
			"TMPDIR":          filepath.Join(s.home, "tmp"),
		}
		for k, v := range want {
			if env[k] != v {
				t.Errorf("%s: %s = %q, want %q", mode, k, env[k], v)
			}
			if !strings.HasPrefix(env[k], s.home) {
				t.Errorf("%s: %s points outside home", mode, k)
			}
		}
		for _, d := range []string{"caddy/data", "caddy/config", "tmp"} {
			if st, err := os.Stat(filepath.Join(s.home, d)); err != nil || !st.IsDir() {
				t.Errorf("%s: %s not created", mode, d)
			}
		}
	}
}

func TestNonCaddyServiceEnvUntouched(t *testing.T) {
	s, st, _, id := setup(t, "idle", 0)
	env := dumpEnv(t, s, st, id)
	if env["XDG_DATA_HOME"] != "/evil/data" || env["TMPDIR"] != "/evil/tmp" {
		t.Fatalf("non-caddy services keep their own env: %v %v", env["XDG_DATA_HOME"], env["TMPDIR"])
	}
}
