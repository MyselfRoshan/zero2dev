// Package sites manages local domains and generates the Caddyfile and
// nginx.conf. Setting siteServer picks which of the two serves them.
package sites

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"zero2dev/internal/apperr"
	"zero2dev/internal/fsutil"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// Emitter receives events (events.Hub).
type Emitter interface{ Publish(typ string, data any) }

// Restarter lets the manager restart, stop and start the site server
// services (supervisor).
type Restarter interface {
	State(id string) string
	Restart(id string) error
	StopWait(id string) error
	Start(id string) error
}

// Manager owns sites and the generated server configs.
type Manager struct {
	home string
	st   *store.Store
	em   Emitter
	sup  Restarter
	mu   sync.Mutex // serialises config writes
}

// New creates a Manager.
func New(home string, st *store.Store, em Emitter, sup Restarter) *Manager {
	return &Manager{home: home, st: st, em: em, sup: sup}
}

// CaddyfilePath is <home>/config/Caddyfile.
func (m *Manager) CaddyfilePath() string { return filepath.Join(m.home, "config", "Caddyfile") }

// NginxConfPath is <home>/config/nginx.conf.
func (m *Manager) NginxConfPath() string { return filepath.Join(m.home, "config", "nginx.conf") }

var domainRE = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// SuffixRE validates the configured domain suffix (one or more labels).
var SuffixRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// List returns sites sorted by domain, with computed urls.
func (m *Manager) List() []model.SiteView {
	d := m.st.Snapshot()
	ss := append([]model.Site{}, d.Sites...)
	sort.Slice(ss, func(i, j int) bool { return ss[i].Domain < ss[j].Domain })
	out := make([]model.SiteView, 0, len(ss))
	for _, s := range ss {
		out = append(out, model.SiteView{Site: s, URL: URL(s, d.Settings)})
	}
	return out
}

// Create adds a site.
func (m *Manager) Create(in model.SiteInput) (model.SiteView, error) {
	var site model.Site
	err := m.st.Update(func(d *store.Data) error {
		s, err := validate(in, "", *d)
		if err != nil {
			return err
		}
		s.ID = model.NewID("site_")
		d.Sites = append(d.Sites, s)
		site = s
		return nil
	})
	if err != nil {
		return model.SiteView{}, err
	}
	m.afterChange()
	return model.SiteView{Site: site, URL: URL(site, m.st.Snapshot().Settings)}, nil
}

// Update replaces a site's fields.
func (m *Manager) Update(id string, in model.SiteInput) (model.SiteView, error) {
	var site model.Site
	err := m.st.Update(func(d *store.Data) error {
		idx := -1
		for i := range d.Sites {
			if d.Sites[i].ID == id {
				idx = i
			}
		}
		if idx < 0 {
			return apperr.NotFound("site %s not found", id)
		}
		s, err := validate(in, id, *d)
		if err != nil {
			return err
		}
		s.ID = id
		d.Sites[idx] = s
		site = s
		return nil
	})
	if err != nil {
		return model.SiteView{}, err
	}
	m.afterChange()
	return model.SiteView{Site: site, URL: URL(site, m.st.Snapshot().Settings)}, nil
}

// SetEnabled switches a site on or off, like starting or stopping a service.
// Only the flag changes: the rest is not validated again, so a site whose
// folder or service went away can still be switched off. An enabled site
// whose service has no port is left out of the generated configs anyway.
func (m *Manager) SetEnabled(id string, on bool) (model.SiteView, error) {
	var site model.Site
	err := m.st.Update(func(d *store.Data) error {
		for i := range d.Sites {
			if d.Sites[i].ID == id {
				d.Sites[i].Enabled = on
				site = d.Sites[i]
				return nil
			}
		}
		return apperr.NotFound("site %s not found", id)
	})
	if err != nil {
		return model.SiteView{}, err
	}
	m.afterChange()
	return model.SiteView{Site: site, URL: URL(site, m.st.Snapshot().Settings)}, nil
}

// Delete removes a site.
func (m *Manager) Delete(id string) error {
	err := m.st.Update(func(d *store.Data) error {
		for i := range d.Sites {
			if d.Sites[i].ID == id {
				d.Sites = append(d.Sites[:i], d.Sites[i+1:]...)
				return nil
			}
		}
		return apperr.NotFound("site %s not found", id)
	})
	if err != nil {
		return err
	}
	m.afterChange()
	return nil
}

func (m *Manager) afterChange() {
	m.em.Publish("sites.changed", struct{}{})
	_ = m.Regenerate()
}

// Regenerate rewrites the Caddyfile and nginx.conf (both are always kept
// current so switching servers needs no extra step) and restarts the
// running service of the active site server in the background (restart
// instead of reload).
func (m *Manager) Regenerate() error {
	m.mu.Lock()
	d := m.st.Snapshot()
	err := m.write(d)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	for _, s := range d.Services {
		if isServerFor(s, Server(d.Settings)) && m.sup.State(s.ID) == model.StateRunning {
			id := s.ID
			go func() { _ = m.sup.Restart(id) }()
		}
	}
	return nil
}

func (m *Manager) write(d store.Data) error {
	if err := fsutil.WriteFileAtomic(m.CaddyfilePath(), []byte(Caddyfile(d)), 0o644); err != nil {
		return err
	}
	// nginx needs its prefix folders: logs/ (also for the error log it opens
	// before reading the config) and temp/ (Windows builds keep their temp
	// paths there).
	for _, sub := range []string{"logs", "temp"} {
		if err := os.MkdirAll(filepath.Join(m.home, "nginx", sub), 0o755); err != nil {
			return err
		}
	}
	if Server(d.Settings) == ServerNginx {
		if err := ensureNginxCerts(m.home, httpsDomains(d)); err != nil {
			return fmt.Errorf("nginx certificates: %w", err)
		}
	}
	return fsutil.WriteFileAtomic(m.NginxConfPath(), []byte(NginxConf(d, m.home)), 0o644)
}

// isServerFor: the service that serves the sites for server srv.
func isServerFor(s model.Service, srv string) bool {
	if srv == ServerNginx {
		return IsSitesNginx(s)
	}
	return s.Kind == "caddy"
}

// SwitchServer is called after setting siteServer changed from old to the
// current value. The servers share the proxy ports, so running services of
// the old server are stopped, and if one was running the new server's
// service is started in its place. Runs in the background.
func (m *Manager) SwitchServer(old string) {
	if _, err := m.EnsureServers(); err != nil {
		return
	}
	d := m.st.Snapshot()
	cur := Server(d.Settings)
	if old == cur {
		return
	}
	var stop []string
	var start string
	for _, s := range d.Services {
		if isServerFor(s, old) && m.sup.State(s.ID) == model.StateRunning {
			stop = append(stop, s.ID)
		}
		if start == "" && isServerFor(s, cur) {
			start = s.ID
		}
	}
	if len(stop) == 0 {
		return
	}
	go func() {
		for _, id := range stop {
			_ = m.sup.StopWait(id)
		}
		if start != "" && m.sup.State(start) != model.StateRunning {
			_ = m.sup.Start(start)
		}
	}()
}

// EnsureServers creates the site server services that are missing: the
// "Caddy" service when a caddy runtime exists, and the "Nginx (sites)"
// service when nginx serves the sites and an nginx runtime exists. Reports
// whether it created one.
func (m *Manager) EnsureServers() (bool, error) {
	c, err := m.EnsureCaddy()
	if err != nil {
		return c, err
	}
	n, err := m.EnsureNginx()
	return c || n, err
}

// EnsureNginx creates the managed "Nginx (sites)" service when siteServer
// is nginx, an nginx runtime exists and no such service does.
func (m *Manager) EnsureNginx() (bool, error) {
	created := false
	err := m.st.Update(func(d *store.Data) error {
		if Server(d.Settings) != ServerNginx {
			return nil
		}
		has := false
		for _, r := range d.Runtimes {
			if r.Kind == "nginx" {
				has = true
			}
		}
		if !has {
			return nil
		}
		for _, s := range d.Services {
			if IsSitesNginx(s) {
				return nil
			}
		}
		d.Services = append(d.Services, model.Service{
			ID: model.NewID("svc_"), Name: "Nginx (sites)", Kind: "nginx", Cwd: m.home,
			Args: append([]string{}, NginxArgs...), CustomArgs: true, Env: map[string]string{},
		})
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if created {
		_ = m.Regenerate() // the config must exist before nginx is started
		m.em.Publish("services.changed", struct{}{})
	}
	return created, nil
}

// EnsureCaddy creates the "Caddy" service when a caddy runtime exists and no
// caddy service does. Reports whether it created one.
func (m *Manager) EnsureCaddy() (bool, error) {
	created := false
	err := m.st.Update(func(d *store.Data) error {
		has := false
		for _, r := range d.Runtimes {
			if r.Kind == "caddy" {
				has = true
			}
		}
		if !has {
			return nil
		}
		for _, s := range d.Services {
			if s.Kind == "caddy" {
				return nil
			}
		}
		d.Services = append(d.Services, model.Service{
			ID: model.NewID("svc_"), Name: "Caddy", Kind: "caddy", Cwd: m.home,
			Args: []string{}, Env: map[string]string{},
		})
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if created {
		_ = m.Regenerate() // the file must exist before Caddy is started
		m.em.Publish("services.changed", struct{}{})
	}
	return created, nil
}

func unsafeForCaddy(p string) bool {
	return strings.ContainsAny(p, "\"{}'`\r\n\x00") || strings.ContainsFunc(p, func(r rune) bool { return r < 0x20 })
}

// validate checks a SiteInput against the current state. selfID is the site
// being updated (excluded from the uniqueness check).
func validate(in model.SiteInput, selfID string, d store.Data) (model.Site, error) {
	s := model.Site{
		Name: strings.TrimSpace(in.Name), Domain: strings.ToLower(strings.TrimSpace(in.Domain)),
		Type: in.Type, HTTPS: in.HTTPS, Enabled: in.Enabled == nil || *in.Enabled,
	}
	if n := len([]rune(s.Name)); n < 1 || n > 64 {
		return s, apperr.BadRequest("name must be 1-64 characters")
	}
	if len(s.Domain) > 253 || !domainRE.MatchString(s.Domain) {
		return s, apperr.BadRequest("domain %q is not a valid hostname", in.Domain)
	}
	ok := false
	for _, suf := range []string{d.Settings.DomainSuffix, "localhost", "test"} {
		if suf != "" && strings.HasSuffix(s.Domain, "."+suf) {
			ok = true
		}
	}
	if !ok {
		return s, apperr.BadRequest("domain must end with .%s (or .localhost / .test)", d.Settings.DomainSuffix)
	}
	for _, o := range d.Sites {
		if o.Domain == s.Domain && o.ID != selfID {
			return s, apperr.Conflict("domain %s is already used by site %q", s.Domain, o.Name)
		}
	}
	svcByID := func(id string) *model.Service {
		for i := range d.Services {
			if d.Services[i].ID == id {
				return &d.Services[i]
			}
		}
		return nil
	}
	switch in.Type {
	case "static":
		if in.Root == "" || !filepath.IsAbs(in.Root) {
			return s, apperr.BadRequest("root must be an absolute path")
		}
		root := filepath.Clean(in.Root)
		if unsafeForCaddy(root) || unsafeForNginx(root) {
			return s, apperr.BadRequest("root contains characters that are not allowed (quotes, braces, $, backslashes in names, control characters)")
		}
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			return s, apperr.BadRequest("root %s is not an existing directory", root)
		}
		s.Root = root
	case "proxy", "php":
		if in.ServiceID != "" {
			sv := svcByID(in.ServiceID)
			if sv == nil {
				return s, apperr.BadRequest("service %s not found", in.ServiceID)
			}
			if in.Type == "php" && sv.Kind != "php" {
				return s, apperr.BadRequest("service %q is not a php service", sv.Name)
			}
			if sv.Port <= 0 {
				return s, apperr.BadRequest("service %q has no port", sv.Name)
			}
			s.ServiceID = in.ServiceID
		} else if in.Type == "php" {
			return s, apperr.BadRequest("php sites need a serviceId of a php service")
		} else {
			if in.UpstreamPort < 1 || in.UpstreamPort > 65535 {
				return s, apperr.BadRequest("proxy sites need a serviceId or an upstreamPort (1-65535)")
			}
			s.UpstreamPort = in.UpstreamPort
		}
	default:
		return s, apperr.BadRequest("type must be static, proxy or php")
	}
	return s, nil
}
