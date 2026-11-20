package snapshot

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// DefaultDomainSuffix is what a clone adds to the first label of each domain.
const DefaultDomainSuffix = "-copy"

var suffixRE = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Clone starts a snapshot-clone job that creates a new project from a project
// snapshot (new ids, ports and domains); the job's ProjectID is the new project.
func (m *Manager) Clone(id string, opt CloneOptions) (model.Job, error) {
	dir, mf, err := m.find(id)
	if err != nil {
		return model.Job{}, err
	}
	if mf.Scope != ScopeProject || mf.Project == nil {
		return model.Job{}, apperr.BadRequest("only project snapshots can be cloned")
	}
	name, err := checkName(opt.Name)
	if err != nil {
		return model.Job{}, err
	}
	suffix := strings.ToLower(strings.TrimSpace(opt.DomainSuffix))
	if suffix == "" {
		suffix = DefaultDomainSuffix
	}
	if !suffixRE.MatchString(suffix) || strings.HasSuffix(suffix, "-") {
		return model.Job{}, apperr.BadRequest("domainSuffix must be 1-32 letters, digits or -, and must not end with -")
	}
	unclaim := m.claim(id, "cloning it")
	j, err := m.Jobs.Go(model.JobSnapshotClone, "snapshot-clone:"+id, StepCopying, func(j *model.Job) { j.SnapshotID = id }, func(h *jobs.Handle) error {
		defer unclaim()
		return m.clone(h, dir, mf, name, suffix)
	})
	if err != nil {
		unclaim()
	}
	return j, err
}

func (m *Manager) clone(h *jobs.Handle, dir string, mf Manifest, name, suffix string) error {
	svcIDs := map[string]string{}
	for _, e := range mf.Services {
		svcIDs[e.Service.ID] = model.NewID("svc_")
	}
	siteIDs := map[string]string{}
	for _, s := range mf.Sites {
		siteIDs[s.ID] = model.NewID("site_")
	}
	projectID := model.NewID("prj_")

	var copies []copyJob
	var created []string
	for _, e := range mf.Services {
		if e.HasData {
			dst := m.Sup.DataDir(e.Service.Kind, svcIDs[e.Service.ID])
			copies = append(copies, copyJob{filepath.Join(dir, "data", e.Service.ID), dst})
			created = append(created, dst)
		}
	}
	undo := func() {
		for _, c := range created {
			_ = os.RemoveAll(c)
		}
	}
	h.Progress(0, StepCopying)
	if err := m.copyAll(h, StepCopying, copies); err != nil {
		undo()
		return apperr.Internal("the snapshot could not be cloned: %v", err)
	}
	h.Progress(99, StepCreating)
	err := m.St.Update(func(d *store.Data) error {
		return m.addClone(d, mf, name, suffix, projectID, svcIDs, siteIDs)
	})
	if err != nil {
		undo()
		return err
	}
	h.Set(func(j *model.Job) { j.ProjectID = projectID })
	if m.Sites != nil {
		_ = m.Sites.Regenerate()
	}
	m.emit("services.changed", struct{}{})
	m.emit("sites.changed", struct{}{})
	m.emit("projects.changed", struct{}{})
	for _, e := range mf.Services {
		m.Sup.Log(svcIDs[e.Service.ID], "system", fmt.Sprintf("created from snapshot %q (%s) of %s", mf.Name, mf.ID, e.Service.Name))
	}
	return nil
}

// addClone adds the copies of mf's project, services and sites to d with the
// new ids, every link between them remapped, free ports and unique domains.
func (m *Manager) addClone(d *store.Data, mf Manifest, name, suffix, projectID string, svcIDs, siteIDs map[string]string) error {
	remap := func(id string) string {
		if n, ok := svcIDs[id]; ok {
			return n
		}
		return id // a service outside the project: the copy uses the same one
	}
	used := map[int]bool{d.Settings.ProxyHTTPPort: true, d.Settings.ProxyHTTPSPort: true}
	for _, sv := range d.Services {
		used[sv.Port] = true
	}
	for _, s := range d.Sites {
		used[s.UpstreamPort] = true
	}
	ports := map[int]int{} // old port -> the copy's port
	for _, e := range mf.Services {
		sv := cleared(e.Service)
		sv.ID, sv.State = svcIDs[e.Service.ID], model.StateStopped
		for i, dep := range sv.DependsOn {
			sv.DependsOn[i] = remap(dep)
		}
		if sv.DatabaseID != "" {
			sv.DatabaseID = remap(sv.DatabaseID)
		}
		if rt := matchRuntime(e.Runtime, d.Runtimes); rt != nil {
			sv.RuntimeID = rt.ID
		} else if !slices.ContainsFunc(d.Runtimes, func(r model.Runtime) bool { return r.ID == sv.RuntimeID }) {
			sv.RuntimeID = ""
		}
		if sv.Port > 0 {
			p, err := m.nextPort(sv.Port, used)
			if err != nil {
				return apperr.Conflict("no free port for the copy of %s", sv.Name)
			}
			used[p] = true
			ports[sv.Port] = p
			sv.Port = p
		}
		d.Services = append(d.Services, sv)
	}
	domains := map[string]bool{}
	for _, s := range d.Sites {
		domains[s.Domain] = true
	}
	for _, s := range mf.Sites {
		c := s
		c.ID = siteIDs[s.ID]
		if c.ServiceID != "" {
			c.ServiceID = remap(c.ServiceID)
		} else if p, ok := ports[c.UpstreamPort]; ok {
			c.UpstreamPort = p
		}
		c.Domain = uniqueDomain(s.Domain, suffix, domains)
		domains[c.Domain] = true
		d.Sites = append(d.Sites, c)
	}
	p := model.Project{ID: projectID, Name: name, ServiceIDs: []string{}, SiteIDs: []string{}}
	for _, id := range mf.Project.ServiceIDs {
		if n, ok := svcIDs[id]; ok {
			p.ServiceIDs = append(p.ServiceIDs, n)
		}
	}
	for _, id := range mf.Project.SiteIDs {
		if n, ok := siteIDs[id]; ok {
			p.SiteIDs = append(p.SiteIDs, n)
		}
	}
	d.Projects = append(d.Projects, p)
	return nil
}

// nextPort is the first port after from (wrapping round to 1024) that no
// service uses and that can be listened on now.
func (m *Manager) nextPort(from int, used map[int]bool) (int, error) {
	free := m.portFree
	if free == nil {
		free = portFree
	}
	for i := 1; i <= 65535; i++ {
		p := from + i
		if p > 65535 {
			p = 1024 + (p-65536)%(65536-1024)
		}
		if p == from {
			break
		}
		if !used[p] && free(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port")
}

func portFree(p int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// uniqueDomain adds suffix to the first label of domain (shop.localhost ->
// shop-copy.localhost) and then -2, -3 ... until no domain in used has it.
// Labels stay within 63 characters.
func uniqueDomain(domain, suffix string, used map[string]bool) string {
	first, rest, dotted := strings.Cut(domain, ".")
	join := func(label string) string {
		if dotted {
			return label + "." + rest
		}
		return label
	}
	fit := func(label, tail string) string {
		if len(label)+len(tail) > 63 {
			label = strings.TrimRight(label[:63-len(tail)], "-")
		}
		return label + tail
	}
	base := fit(first, suffix)
	cand := join(base)
	for n := 2; used[cand]; n++ {
		cand = join(fit(base, "-"+strconv.Itoa(n)))
	}
	return cand
}
