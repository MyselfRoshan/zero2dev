// Package store persists daemon state as one JSON file, written atomically.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"zero2dev/internal/fsutil"
	"zero2dev/internal/model"
)

// Data is the whole persisted state.
type Data struct {
	Services []model.Service `json:"services"`
	Runtimes []model.Runtime `json:"runtimes"`
	Sites    []model.Site    `json:"sites"`
	Settings model.Settings  `json:"settings"`
}

// Store guards Data with a mutex and persists every successful Update.
type Store struct {
	mu   sync.RWMutex
	path string
	data Data
}

func defaults() Data {
	return Data{
		Services: []model.Service{}, Runtimes: []model.Runtime{}, Sites: []model.Site{},
		Settings: model.Settings{DomainSuffix: "localhost", ProxyHTTPPort: 8080, ProxyHTTPSPort: 8443, OpenBrowserOnStart: true, SiteServer: "caddy",
			Theme: model.DefaultTheme, Accent: model.DefaultAccent},
	}
}

// Open loads path (or starts empty if it does not exist). A corrupt file is
// an error: we never silently discard the user's data.
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: defaults()}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, errors.New("state file " + path + " is corrupt: " + err.Error())
	}
	migrate(&s.data)
	s.normalize(&s.data)
	return s, nil
}

// migrate updates state written by older versions. Services saved before
// per-kind settings existed (no "settings" key) whose args were given keep
// exactly that command line: their args become the whole argument list.
func migrate(d *Data) {
	for i := range d.Services {
		sv := &d.Services[i]
		if sv.Settings == nil && len(sv.Args) > 0 && sv.Kind != "custom" {
			sv.CustomArgs = true
		}
	}
}

func (s *Store) normalize(d *Data) {
	if d.Services == nil {
		d.Services = []model.Service{}
	}
	if d.Runtimes == nil {
		d.Runtimes = []model.Runtime{}
	}
	if d.Sites == nil {
		d.Sites = []model.Site{}
	}
	// state written before appearance settings existed
	if d.Settings.Theme == "" {
		d.Settings.Theme = model.DefaultTheme
	}
	if d.Settings.Accent == "" {
		d.Settings.Accent = model.DefaultAccent
	}
	for i := range d.Services {
		sv := &d.Services[i]
		if sv.Args == nil {
			sv.Args = []string{}
		}
		if sv.Env == nil {
			sv.Env = map[string]string{}
		}
		if sv.Settings == nil {
			sv.Settings = map[string]string{}
		}
		// runtime fields are never persisted meaningfully
		sv.State, sv.Pid, sv.StartedAt, sv.LastError = model.StateStopped, 0, nil, ""
	}
}

func clone(d Data) Data {
	return Data{
		Services: append([]model.Service{}, d.Services...),
		Runtimes: append([]model.Runtime{}, d.Runtimes...),
		Sites:    append([]model.Site{}, d.Sites...),
		Settings: d.Settings,
	}
}

// Snapshot returns a copy safe to read without locking. Callers must treat
// nested slices/maps (Args, Env) as read-only.
func (s *Store) Snapshot() Data {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data)
}

// Update runs fn on a copy; if fn succeeds the copy is written to disk and
// becomes current. If fn or the write fails nothing changes.
func (s *Store) Update(fn func(*Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := clone(s.data)
	if err := fn(&c); err != nil {
		return err
	}
	s.normalize(&c)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(s.path, b, 0o600); err != nil {
		return err
	}
	s.data = c
	return nil
}

// RebasePath maps a stored installed-runtime binary path onto the runtimes
// folder bin: the segments after a "runtimes" (the pre-0.1 name, inside the data
// folder) or "bin" segment are joined to bin, and the first candidate that exists is
// used. If none exists, a legacy "runtimes" path is rebased anyway (its files may be
// moved next). Both \ and / separators are understood, so a path written on Windows
// can be rebased on Linux and vice versa. Anything else is returned unchanged.
func RebasePath(bin, stored string) string {
	parts := strings.Split(strings.ReplaceAll(stored, "\\", "/"), "/")
	var cands []string
	legacy := ""
	for i, p := range parts {
		if (p == "runtimes" || p == "bin") && i < len(parts)-1 {
			c := filepath.Join(append([]string{bin}, parts[i+1:]...)...)
			cands = append(cands, c)
			if p == "runtimes" && legacy == "" {
				legacy = c
			}
		}
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if legacy != "" {
		return legacy
	}
	return stored
}

// RebaseRuntimes makes the state relocatable: installed runtimes whose
// binary no longer exists are re-pointed into the runtimes folder bin and the result
// is persisted. Registered runtimes keep their absolute paths. It returns the
// number of runtimes changed.
func (s *Store) RebaseRuntimes(bin string) (int, error) {
	n := 0
	err := s.Update(func(d *Data) error {
		for i := range d.Runtimes {
			r := &d.Runtimes[i]
			if r.Source != "installed" {
				continue
			}
			if _, err := os.Stat(r.Binary); err == nil {
				continue
			}
			if nb := RebasePath(bin, r.Binary); nb != r.Binary {
				r.Binary = nb
				n++
			}
		}
		if n == 0 {
			return errNoChange
		}
		return nil
	})
	if err == errNoChange {
		return 0, nil
	}
	return n, err
}

var errNoChange = errors.New("no change")
