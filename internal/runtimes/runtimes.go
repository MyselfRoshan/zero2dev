// Package runtimes detects, registers and lists language runtimes.
package runtimes

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// Emitter receives events (events.Hub).
type Emitter interface{ Publish(typ string, data any) }

// Manager manages the runtime list.
type Manager struct {
	home     string
	st       *store.Store
	em       Emitter
	OnChange func() // called after the runtime list changed (may be nil)
}

// New creates a Manager.
func New(home string, st *store.Store, em Emitter) *Manager {
	return &Manager{home: home, st: st, em: em}
}

// Detected is a tool found on PATH.
type Detected struct {
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Binary  string `json:"binary"`
	Name    string `json:"name,omitempty"` // set for kind "other" (e.g. nginx)
}

// tools probed by Detect. versionArgs differ per tool (go and caddy use a
// "version" subcommand, java prints -version to stderr).
var tools = []struct {
	prog, kind string
	args       []string
}{
	{"node", "node", []string{"--version"}},
	{"php", "php", []string{"--version"}},
	{"go", "go", []string{"version"}},
	{"java", "java", []string{"-version"}},
	{"caddy", "caddy", []string{"version"}},
	{"nginx", "other", []string{"-v"}},
}

var versionRE = regexp.MustCompile(`\d+(?:\.\d+){1,3}`)

// probe runs `<binary> <args>` with a short timeout and parses a version.
// The binary path is passed straight to exec (no shell).
func probe(binary string, args []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	return versionRE.FindString(string(out))
}

func argsFor(kind, binary string) []string {
	for _, t := range tools {
		if t.kind == kind && t.kind != "other" {
			return t.args
		}
	}
	if strings.EqualFold(strings.TrimSuffix(filepath.Base(binary), ".exe"), "nginx") {
		return []string{"-v"}
	}
	return []string{"--version"}
}

// Detect looks for known tools on PATH.
func (m *Manager) Detect() []Detected {
	res := []Detected{}
	for _, t := range tools {
		p, err := exec.LookPath(t.prog)
		if err != nil {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		d := Detected{Kind: t.kind, Binary: p, Version: probe(p, t.args)}
		if d.Version == "" {
			d.Version = "unknown"
		}
		if t.kind == "other" {
			d.Name = t.prog
		}
		res = append(res, d)
	}
	return res
}

var display = map[string]string{"node": "Node.js", "php": "PHP", "go": "Go", "java": "Java", "caddy": "Caddy"}

// DisplayName builds e.g. "Node.js 22.11.0".
func DisplayName(kind, version, binary string) string {
	n := display[kind]
	if n == "" {
		n = strings.TrimSuffix(filepath.Base(binary), filepath.Ext(binary))
	}
	return strings.TrimSpace(n + " " + version)
}

// List returns runtimes sorted by kind then name.
func (m *Manager) List() []model.Runtime {
	rs := m.st.Snapshot().Runtimes
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Kind != rs[j].Kind {
			return rs[i].Kind < rs[j].Kind
		}
		return rs[i].Name < rs[j].Name
	})
	return rs
}

// Register records a user-provided binary.
func (m *Manager) Register(kind, binary, version string) (model.Runtime, error) {
	if !model.In(kind, model.RuntimeKinds) {
		return model.Runtime{}, apperr.BadRequest("kind must be one of %s", strings.Join(model.RuntimeKinds, ", "))
	}
	if binary == "" || !filepath.IsAbs(binary) {
		return model.Runtime{}, apperr.BadRequest("binary must be an absolute path")
	}
	binary = filepath.Clean(binary)
	if st, err := os.Stat(binary); err != nil || st.IsDir() {
		return model.Runtime{}, apperr.BadRequest("binary %s does not exist or is not a file", binary)
	}
	version = strings.TrimSpace(version)
	if len(version) > 40 || strings.ContainsAny(version, "\r\n") {
		return model.Runtime{}, apperr.BadRequest("version is invalid")
	}
	if version == "" {
		if version = probe(binary, argsFor(kind, binary)); version == "" {
			version = "unknown"
		}
	}
	rt := model.Runtime{
		ID: model.NewID("rt_"), Kind: kind, Version: version, Name: DisplayName(kind, version, binary),
		Binary: binary, Source: "registered", InstalledAt: time.Now().UTC().Truncate(time.Second),
	}
	return rt, m.Add(rt)
}

// Add stores a runtime (used by Register and the installer).
func (m *Manager) Add(rt model.Runtime) error {
	err := m.st.Update(func(d *store.Data) error {
		for _, r := range d.Runtimes {
			if r.Binary == rt.Binary {
				return apperr.Conflict("%s is already registered as %s", rt.Binary, r.ID)
			}
		}
		d.Runtimes = append(d.Runtimes, rt)
		return nil
	})
	if err != nil {
		return err
	}
	m.changed()
	return nil
}

func (m *Manager) changed() {
	m.em.Publish("runtimes.changed", struct{}{})
	if m.OnChange != nil {
		m.OnChange()
	}
}

// Delete removes a runtime; installed ones also lose their folder.
func (m *Manager) Delete(id string) error {
	d := m.st.Snapshot()
	var rt *model.Runtime
	for i := range d.Runtimes {
		if d.Runtimes[i].ID == id {
			rt = &d.Runtimes[i]
		}
	}
	if rt == nil {
		return apperr.NotFound("runtime %s not found", id)
	}
	for _, s := range d.Services {
		if s.RuntimeID == id {
			return apperr.Conflict("runtime is used by service %q", s.Name)
		}
	}
	if rt.Source == "installed" {
		if dir := m.installDir(rt.Binary); dir != "" {
			if err := os.RemoveAll(dir); err != nil {
				return apperr.Internal("cannot remove %s: %v", dir, err)
			}
		}
	}
	if err := m.st.Update(func(d *store.Data) error {
		out := d.Runtimes[:0]
		for _, r := range d.Runtimes {
			if r.ID != id {
				out = append(out, r)
			}
		}
		d.Runtimes = out
		return nil
	}); err != nil {
		return err
	}
	m.changed()
	return nil
}

// installDir returns <home>/runtimes/<first component> for a binary below
// the runtimes dir, or "" if the binary is elsewhere (never delete outside).
func (m *Manager) installDir(binary string) string {
	root := filepath.Join(m.home, "runtimes")
	rel, err := filepath.Rel(root, binary)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	first := strings.Split(filepath.ToSlash(rel), "/")[0]
	return filepath.Join(root, first)
}
