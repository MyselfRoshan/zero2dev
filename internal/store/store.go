// Package store persists daemon state as one JSON file, written atomically.
package store

import (
	"encoding/json"
	"errors"
	"os"
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
		Settings: model.Settings{DomainSuffix: "localhost", ProxyHTTPPort: 8080, ProxyHTTPSPort: 8443, OpenBrowserOnStart: true},
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
	s.normalize(&s.data)
	return s, nil
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
	for i := range d.Services {
		sv := &d.Services[i]
		if sv.Args == nil {
			sv.Args = []string{}
		}
		if sv.Env == nil {
			sv.Env = map[string]string{}
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
