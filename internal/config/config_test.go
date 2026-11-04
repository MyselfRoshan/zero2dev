package config

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func TestFromEnvAndToken(t *testing.T) {
	home := filepath.Join(t.TempDir(), "h")
	env := map[string]string{"ZERO2DEV_HOME": home, "ZERO2DEV_PORT": "7123", "ZERO2DEV_NO_BROWSER": "1", "ZERO2DEV_DEV_ORIGIN": "http://localhost:5173/"}
	c, err := FromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 7123 || !c.NoBrowser || c.Home != home || c.DevOrigin != "http://localhost:5173" {
		t.Fatalf("%+v", c)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(c.Token) {
		t.Fatalf("token %q", c.Token)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(filepath.Join(home, "token")); st.Mode().Perm() != 0o600 {
			t.Fatalf("token mode %v", st.Mode())
		}
	}
	for _, d := range []string{"runtimes", "config", "logs", "downloads"} {
		if _, err := os.Stat(filepath.Join(home, d)); err != nil {
			t.Fatalf("missing %s", d)
		}
	}
	c2, _ := FromEnv(func(k string) string { return env[k] })
	if c2.Token != c.Token {
		t.Fatal("token must be stable across runs")
	}
	env["ZERO2DEV_PORT"] = "99999"
	if _, err := FromEnv(func(k string) string { return env[k] }); err == nil {
		t.Fatal("bad port must fail")
	}
}
