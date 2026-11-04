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

// Config is the daemon's static configuration (from env at startup).
type Config struct {
	Home      string
	Port      int
	DevOrigin string // optional extra allowed Origin (Vite dev server)
	NoBrowser bool
	Token     string
}

// FromEnv builds a Config using getenv (os.Getenv in production). It creates
// the home layout and the token file.
func FromEnv(getenv func(string) string) (*Config, error) {
	c := &Config{Port: 7000, DevOrigin: strings.TrimRight(strings.TrimSpace(getenv("ZERO2DEV_DEV_ORIGIN")), "/")}
	if p := strings.TrimSpace(getenv("ZERO2DEV_PORT")); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid ZERO2DEV_PORT %q", p)
		}
		c.Port = n
	}
	c.NoBrowser = getenv("ZERO2DEV_NO_BROWSER") == "1"
	home := strings.TrimSpace(getenv("ZERO2DEV_HOME"))
	if home == "" {
		uh, err := os.UserHomeDir() // %USERPROFILE% on Windows
		if err != nil {
			return nil, fmt.Errorf("cannot determine home directory: %w", err)
		}
		home = filepath.Join(uh, ".zero2dev")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	c.Home = abs
	for _, d := range []string{"", "runtimes", "config", "logs", "downloads"} {
		if err := os.MkdirAll(filepath.Join(c.Home, d), 0o755); err != nil {
			return nil, err
		}
	}
	if c.Token, err = LoadOrCreateToken(c.Home); err != nil {
		return nil, err
	}
	return c, nil
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
