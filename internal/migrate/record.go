package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/fsutil"
)

// record is <home>\migrations\<id>\migration.json: the Migration the API
// shows plus what a revert needs. Passwords are never written.
type record struct {
	Migration
	Format   int           `json:"format"`
	Steps    []step        `json:"steps"`
	Portable string        `json:"portable,omitempty"` // Settings.Portable flag switched on
	Image    string        `json:"image,omitempty"`    // process to be closed before a revert
	Runtime  *adoption     `json:"runtime,omitempty"`
	Remote   *remoteServer `json:"remote,omitempty"`
	Error    string        `json:"error,omitempty"` // why it failed
}

// step is one item's progress and backups. Backup paths are relative to the
// migration's folder.
type step struct {
	Label     string   `json:"label"`
	Kind      string   `json:"kind"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	SizeBytes int64    `json:"sizeBytes"`
	RegPath   string   `json:"regPath,omitempty"`
	Rewrite   bool     `json:"rewrite,omitempty"`
	Skip      []string `json:"skip,omitempty"`

	Started bool `json:"started"` // the target was touched
	Done    bool `json:"done"`    // the item is in the portable place
	Moved   bool `json:"moved"`   // the original is gone (a move)

	TargetBackup   string   `json:"targetBackup,omitempty"`   // what the portable place held before
	OriginalBackup string   `json:"originalBackup,omitempty"` // HeidiSQL move: the registry key
	RevertBackups  []string `json:"revertBackups,omitempty"`  // what the original place held at revert time
}

// adoption is a runtime adopted into <bin>\adopted\.
type adoption struct {
	RuntimeID      string `json:"runtimeId"`
	Root           string `json:"root"`           // the installation folder that was copied
	OriginalBinary string `json:"originalBinary"` // the registered binary
	Dir            string `json:"dir"`            // <bin>\adopted\<kind>-<version>
	Binary         string `json:"binary"`         // the binary in Dir ("" until adopted)
}

// remoteServer is the database source's server (no password).
type remoteServer struct {
	Kind string `json:"kind"`
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
}

const recordFormat = 1

func (m *Manager) migrationsDir() string { return filepath.Join(m.Home, "migrations") }

func (m *Manager) migDir(id string) string { return filepath.Join(m.migrationsDir(), id) }

func (m *Manager) save(r *record) error {
	r.Format = recordFormat
	if r.Items == nil {
		r.Items = []Item{}
	}
	if r.RevertHow == nil {
		r.RevertHow = []string{}
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(m.migDir(r.ID), "migration.json"), append(raw, '\n'), 0o644)
}

// validID: mig_ and hex, so an id from a URL can never leave the folder.
func validID(id string) bool {
	if !strings.HasPrefix(id, "mig_") || len(id) < 5 || len(id) > 40 {
		return false
	}
	for _, c := range id[4:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (m *Manager) load(id string) (*record, error) {
	if !validID(id) {
		return nil, apperr.NotFound("migration %s not found", id)
	}
	raw, err := os.ReadFile(filepath.Join(m.migDir(id), "migration.json"))
	if err != nil {
		return nil, apperr.NotFound("migration %s not found", id)
	}
	var r record
	if err := json.Unmarshal(raw, &r); err != nil || r.ID != id {
		return nil, apperr.Conflict("the record of migration %s is damaged", id)
	}
	return &r, nil
}

// loadAll reads every record, newest first. Damaged ones are left out.
func (m *Manager) loadAll() ([]*record, error) {
	ents, err := os.ReadDir(m.migrationsDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*record
	for _, e := range ents {
		if !e.IsDir() || !validID(e.Name()) {
			continue
		}
		if r, err := m.load(e.Name()); err == nil {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

// backupPath is a new path <migration>\<kind>\<n>-<label>\<name> (with -2,
// -3 ... when it is taken); rel is the same relative to the migration folder.
func (m *Manager) backupPath(r *record, kind string, n int, label, name string) (abs, rel string) {
	slug := strings.ToLower(safeName(strings.ReplaceAll(label, " ", "-")))
	base := filepath.Join(kind, itoa(n)+"-"+slug)
	rel = base
	for i := 2; exists(filepath.Join(m.migDir(r.ID), rel)); i++ {
		rel = base + "-" + itoa(i)
	}
	rel = filepath.Join(rel, name)
	return filepath.Join(m.migDir(r.ID), rel), rel
}

func (m *Manager) abs(r *record, rel string) string { return filepath.Join(m.migDir(r.ID), rel) }
