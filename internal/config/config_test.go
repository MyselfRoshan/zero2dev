package config

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func noExe() (string, error) { return "", os.ErrNotExist }

func TestFromEnvAndToken(t *testing.T) {
	home := filepath.Join(t.TempDir(), "h")
	env := map[string]string{"ZERO2DEV_HOME": home, "ZERO2DEV_PORT": "7123", "ZERO2DEV_NO_BROWSER": "1", "ZERO2DEV_DEV_ORIGIN": "http://localhost:5173/"}
	get := func(k string) string { return env[k] }
	c, err := FromEnv(get)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 7123 || !c.NoBrowser || c.Home != home || c.DevOrigin != "http://localhost:5173" || c.HomeSource != SourceEnv || c.Portable() {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(c.Describe(), "Data folder: "+home) || !strings.Contains(c.Describe(), "(from ZERO2DEV_HOME)") {
		t.Fatal(c.Describe())
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(c.Token) {
		t.Fatalf("token %q", c.Token)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(filepath.Join(home, "token")); st.Mode().Perm() != 0o600 {
			t.Fatalf("token mode %v", st.Mode())
		}
	}
	for _, d := range []string{"config", "logs", "tmp", "caddy/data", "caddy/config"} {
		if _, err := os.Stat(filepath.Join(home, d)); err != nil {
			t.Fatalf("missing %s", d)
		}
	}
	c2, _ := FromEnv(get)
	if c2.Token != c.Token {
		t.Fatal("token must be stable across runs")
	}
	env["ZERO2DEV_PORT"] = "99999"
	if _, err := FromEnv(get); err == nil {
		t.Fatal("bad port must fail")
	}
}

func TestHomeResolution(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "app", "bin")
	os.MkdirAll(binDir, 0o755)
	exe := filepath.Join(binDir, "zero2dev")
	os.WriteFile(exe, []byte("x"), 0o755)
	cwd := filepath.Join(root, "work")
	cwdFn := func() (string, error) { return cwd, nil }
	none := func(string) string { return "" }
	real := func(p string) string { r, _ := filepath.EvalSymlinks(p); return r }

	// default: <exe dir>/data
	h, src, ed, err := resolveHome(none, func() (string, error) { return exe, nil }, cwdFn)
	if err != nil || src != SourceExe || h != filepath.Join(real(binDir), "data") || ed != real(binDir) {
		t.Fatalf("exe: %q %q %q %v", h, src, ed, err)
	}
	// symlinked executable resolves to the real directory
	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link")
		if os.Symlink(exe, link) == nil {
			h, src, _, _ := resolveHome(none, func() (string, error) { return link, nil }, cwdFn)
			if src != SourceExe || h != filepath.Join(real(binDir), "data") {
				t.Fatalf("symlink: %q %q", h, src)
			}
		}
	}
	// env wins, even over go-build
	envHome := filepath.Join(root, "custom")
	h, src, _, _ = resolveHome(func(k string) string {
		if k == "ZERO2DEV_HOME" {
			return envHome
		}
		return ""
	}, func() (string, error) { return "/tmp/go-build123/b001/exe/zero2dev", nil }, cwdFn)
	if src != SourceEnv || h != envHome {
		t.Fatalf("env: %q %q", h, src)
	}
	// go run: ./data
	h, src, _, _ = resolveHome(none, func() (string, error) { return "/tmp/go-build123/b001/exe/zero2dev", nil }, cwdFn)
	if src != SourceCwd || h != filepath.Join(cwd, "data") {
		t.Fatalf("go-build: %q %q", h, src)
	}
	// unknown executable: also ./data
	if h, src, _, _ = resolveHome(none, noExe, cwdFn); src != SourceCwd || h != filepath.Join(cwd, "data") {
		t.Fatalf("no exe: %q %q", h, src)
	}
}

func TestDescribeAndTempEnv(t *testing.T) {
	c := &Config{Home: "/x/data", HomeSource: SourceExe}
	if c.Describe() != "Data folder: /x/data (portable, next to the program)" || !c.Portable() {
		t.Fatal(c.Describe())
	}
	if (&Config{Home: "/y", HomeSource: SourceCwd}).Describe() != "Data folder: /y (current directory, dev mode)" {
		t.Fatal("cwd description")
	}
	for _, k := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(k, "orig")
	}
	c.ApplyTempEnv()
	for _, k := range []string{"TMP", "TEMP", "TMPDIR"} {
		if os.Getenv(k) != filepath.Join("/x/data", "tmp") {
			t.Errorf("%s = %q", k, os.Getenv(k))
		}
	}
}
