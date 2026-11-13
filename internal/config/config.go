// Package config resolves the home directory, port and access token.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"zero2dev/internal/fsutil"
)

// Home sources (see docs/API.md "Portable home folder").
const (
	SourceEnv = "env"
	SourceCwd = "cwd"
	SourceExe = "exe"
)

// Config is the daemon's static configuration (from env at startup).
type Config struct {
	Home       string
	BinDir     string // downloaded runtimes live here, separate from Home (data)
	HomeSource string // env | cwd | exe
	ExeDir     string // directory of the running executable ("" if unknown)
	Port       int
	DevOrigin  string // optional extra allowed Origin (Vite dev server)
	NoBrowser  bool
	Token      string
}

// Portable is true only when the data folder sits next to the executable.
func (c *Config) Portable() bool { return c.HomeSource == SourceExe }

// TmpDir is <home>/tmp, the daemon's own temp folder.
func (c *Config) TmpDir() string { return filepath.Join(c.Home, "tmp") }

// Describe is the startup line naming the data folder and why it was chosen.
func (c *Config) Describe() string {
	how := map[string]string{
		SourceExe: "portable, next to the program",
		SourceEnv: "from ZERO2DEV_HOME",
		SourceCwd: "current directory, dev mode",
	}[c.HomeSource]
	return fmt.Sprintf("Data folder: %s (%s)", c.Home, how)
}

// ApplyTempEnv points TMP/TEMP/TMPDIR of this process at <home>/tmp so that
// temp files of the daemon (and children that inherit them) stay inside home.
func (c *Config) ApplyTempEnv() {
	for _, k := range []string{"TMP", "TEMP", "TMPDIR"} {
		_ = os.Setenv(k, c.TmpDir())
	}
}

// FromEnv builds a Config using getenv (os.Getenv in production). It creates
// the home layout and the token file.
func FromEnv(getenv func(string) string) (*Config, error) {
	c, err := ResolveEnv(getenv)
	if err != nil {
		return nil, err
	}
	if err := c.Initialize(); err != nil {
		return nil, err
	}
	return c, nil
}

// resolveHome picks the home dir: ZERO2DEV_HOME, else ./data when the binary
// is a `go run` temp build, else <exe dir>/data.
func resolveHome(getenv func(string) string, exeFn func() (string, error), cwdFn func() (string, error)) (home, source, exeDir string, err error) {
	exe, exeErr := exeFn()
	if exeErr == nil {
		if r, e := filepath.EvalSymlinks(exe); e == nil {
			exe = r
		}
		if abs, e := filepath.Abs(exe); e == nil {
			exe = abs
		}
		exeDir = filepath.Dir(exe)
	}
	if h := strings.TrimSpace(getenv("ZERO2DEV_HOME")); h != "" {
		abs, e := filepath.Abs(h)
		return abs, SourceEnv, exeDir, e
	}
	if exeErr != nil || strings.Contains(strings.ToLower(exe), "go-build") {
		cwd, e := cwdFn()
		if e != nil {
			return "", "", exeDir, e
		}
		return filepath.Join(cwd, "data"), SourceCwd, exeDir, nil
	}
	return filepath.Join(exeDir, "data"), SourceExe, exeDir, nil
}

func fromEnv(getenv func(string) string, exeFn func() (string, error), cwdFn func() (string, error)) (*Config, error) {
	c, err := resolveEnv(getenv, exeFn, cwdFn)
	if err != nil {
		return nil, err
	}
	if err := c.Initialize(); err != nil {
		return nil, err
	}
	return c, nil
}

// ResolveEnv reads configuration without writing shared files.
func ResolveEnv(getenv func(string) string) (*Config, error) {
	return resolveEnv(getenv, os.Executable, os.Getwd)
}

func resolveEnv(getenv func(string) string, exeFn func() (string, error), cwdFn func() (string, error)) (*Config, error) {
	c := &Config{Port: 7000, DevOrigin: strings.TrimRight(strings.TrimSpace(getenv("ZERO2DEV_DEV_ORIGIN")), "/")}
	if p := strings.TrimSpace(getenv("ZERO2DEV_PORT")); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid ZERO2DEV_PORT %q", p)
		}
		c.Port = n
	}
	c.NoBrowser = getenv("ZERO2DEV_NO_BROWSER") == "1"
	var err error
	if c.Home, c.HomeSource, c.ExeDir, err = resolveHome(getenv, exeFn, cwdFn); err != nil {
		return nil, fmt.Errorf("cannot determine data folder: %w", err)
	}
	c.BinDir = resolveBin(getenv, c)
	return c, nil
}

// Initialize creates the layout and loads the token after ownership is acquired.
func (c *Config) Initialize() error {
	if err := os.MkdirAll(c.BinDir, 0o755); err != nil {
		return fmt.Errorf("cannot create runtimes folder %s: %w", c.BinDir, err)
	}
	for _, d := range []string{"", "config", "logs", "tmp", filepath.Join("caddy", "data"), filepath.Join("caddy", "config")} {
		if err := os.MkdirAll(filepath.Join(c.Home, d), 0o755); err != nil {
			return fmt.Errorf("cannot create data folder %s: %w", c.Home, err)
		}
	}
	var err error
	if c.Token, err = LoadOrCreateToken(c.Home); err != nil {
		return err
	}
	return nil
}

// resolveBin picks the runtimes folder: ZERO2DEV_BIN, else <exe dir>/bin next to the
// data folder in portable mode, else <home>/bin (dev and ZERO2DEV_HOME modes).
func resolveBin(getenv func(string) string, c *Config) string {
	if b := strings.TrimSpace(getenv("ZERO2DEV_BIN")); b != "" {
		if abs, err := filepath.Abs(b); err == nil {
			return abs
		}
		return b
	}
	if c.HomeSource == SourceExe && c.ExeDir != "" {
		return filepath.Join(c.ExeDir, "bin")
	}
	return filepath.Join(c.Home, "bin")
}

var tokenRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// LoadOrCreateToken returns the token stored in <home>/token, generating a
// fresh 32-byte hex token (file mode 0600) if missing or malformed.
func LoadOrCreateToken(home string) (string, error) {
	path := filepath.Join(home, "token")
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); tokenRE.MatchString(t) {
			return t, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	t := hex.EncodeToString(raw)
	if err := fsutil.WriteFileAtomic(path, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}
