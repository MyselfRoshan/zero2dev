package newproject

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/envfile"
	"zero2dev/internal/frameworks"
	"zero2dev/internal/jobs"
	"zero2dev/internal/kinds"
	"zero2dev/internal/launch"
	"zero2dev/internal/model"
	"zero2dev/internal/sites"
	"zero2dev/internal/store"
	"zero2dev/internal/validate"
)

// Start runs the job (type "new-project"; job.done carries projectId and path). It answers
// the same problems as Plan with a 409, so a stale plan cannot start.
func (m *Manager) Start(req Request) (model.Job, error) {
	if m.St == nil || m.Jobs == nil || m.Sup == nil {
		return model.Job{}, ErrNotImplemented
	}
	p, err := m.plan(req)
	if err != nil {
		return model.Job{}, err
	}
	if len(p.Problems) > 0 {
		return model.Job{}, apperr.Conflict("%s", p.Problems[0])
	}
	if len(p.Installs) > 0 && m.Installer == nil || p.Database != nil && m.DB == nil {
		return model.Job{}, ErrNotImplemented
	}
	key := "newproject:" + strings.ToLower(p.Dir)
	return m.Jobs.Go(model.JobNewProject, key, stepChecking, nil, func(h *jobs.Handle) error {
		return (&job{m: m, h: h, req: req}).do()
	})
}

const stepChecking = "Checking the plan"

// job is one run of New project and what it has created so far (for the clean-up).
type job struct {
	m   *Manager
	h   *jobs.Handle
	req Request
	p   *planned

	vals   Values
	rt     *model.Runtime
	svcIDs []string // one id per service of the framework, allocated up front (a Python service's virtual environment needs it)

	dirCreated   bool
	dbSvcID      string
	dbSvcCreated bool
	dbCreated    bool
	userCreated  bool
	createdSvcs  []string
	siteID       string
	projectID    string

	out []string // generator output, kept until the first service exists
}

const maxOut = 2000

func (j *job) log(line string) {
	if pw := j.vals.DBPassword; pw != "" {
		line = strings.ReplaceAll(line, pw, "********")
	}
	if len(j.out) < maxOut {
		j.out = append(j.out, line)
	}
}

// progress maps step i of n inside the stretch [from, to) of the job's percent.
func (j *job) progress(from, to, i, n int, step string) {
	if n < 1 {
		n = 1
	}
	j.h.Progress(from+(to-from)*i/n, step)
}

func (j *job) do() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the job crashed: %v", r)
		}
		if err != nil {
			err = j.fail(err)
		}
	}()
	m := j.m

	// 1. check the plan again
	j.h.Progress(2, stepChecking)
	if j.p, err = m.plan(j.req); err != nil {
		return err
	}
	if len(j.p.Problems) > 0 {
		return errors.New(j.p.Problems[0])
	}
	p := j.p
	j.vals = p.vals
	j.svcIDs = make([]string, len(p.fw.Services))
	for i := range j.svcIDs {
		j.svcIDs[i] = model.NewID("svc_")
	}

	// 2. installs
	for i, in := range p.Installs {
		j.progress(5, 35, i, len(p.Installs), "Installing "+in.Name)
		if err := m.installOne(in, func(pct int) { j.progress(5, 35, i*100+pct, len(p.Installs)*100, "Installing "+in.Name) }); err != nil {
			return fmt.Errorf("Installing %s failed: %v", in.Name, err)
		}
	}
	if k := p.fw.Runtime.Kind; k != "" {
		j.rt = selectRuntime(m.St.Snapshot().Runtimes, k, p.fw.Runtime.Min, p.fw.Runtime.Max)
		if j.rt == nil {
			return fmt.Errorf("No %s%s is installed.", kindLabel(k), boundsText(p.fw.Runtime.Min, p.fw.Runtime.Max))
		}
		if k == "python" {
			for i, tpl := range p.fw.Services {
				if tpl.Kind == "python" {
					j.vals.Venv = kinds.VenvDir(m.Home, j.svcIDs[i])
					break
				}
			}
		}
	}

	// 3. database
	if p.Database != nil {
		step := p.Steps[len(p.Installs)]
		j.h.Progress(35, step)
		if err := j.setupDatabase(); err != nil {
			return err
		}
	}

	// 4. the generator
	if err := j.generate(); err != nil {
		return err
	}

	// 5. .env
	if lines := p.envLines(); len(lines) > 0 {
		j.h.Progress(80, "Writing the "+p.envFile()+" file")
		if err := j.writeEnv(lines); err != nil {
			return err
		}
	}

	// 6. services, site, project
	j.h.Progress(85, "Creating the project")
	if err := j.create(); err != nil {
		return err
	}

	// 7. editor
	if p.editor != nil && m.Launch != nil {
		j.h.Progress(95, "Opening the folder in "+p.editor.Name)
		j.openEditor()
	}
	j.h.Set(func(jb *model.Job) { jb.ProjectID, jb.Path = j.projectID, p.Dir })
	return nil
}

// installOne installs a catalog entry through the installer and waits for it.
func (m *Manager) installOne(in Install, progress func(percent int)) error {
	return InstallAndWait(m.Installer, m.St, in.CatalogID, m.poll(), m.installWait(), progress)
}

// InstallAndWait installs a catalog entry through the installer and waits for it (an entry that is
// already installed is fine; one that is being installed is waited for). New project and the
// project file import share it.
func InstallAndWait(inst InstallerAPI, st *store.Store, catalogID string, poll, wait time.Duration, progress func(percent int)) error {
	jb, err := inst.Start(catalogID)
	var ae *apperr.Error
	if err != nil {
		switch {
		case errors.As(err, &ae) && ae.Status == 409 && strings.Contains(ae.Message, "already installed"):
			return nil
		case errors.As(err, &ae) && ae.Status == 409 && strings.Contains(ae.Message, "already being installed"):
			return waitInstalled(st, catalogID, poll, wait)
		}
		return err
	}
	deadline := time.Now().Add(wait)
	for {
		cur, ok := inst.Get(jb.ID)
		switch {
		case !ok:
			return errors.New("the install job disappeared")
		case cur.State == "done":
			return nil
		case cur.State == "failed":
			return errors.New(cur.Error)
		}
		progress(cur.Percent)
		if time.Now().After(deadline) {
			return errors.New("it took too long")
		}
		time.Sleep(poll)
	}
}

func waitInstalled(st *store.Store, catalogID string, poll, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if slices.ContainsFunc(st.Snapshot().Runtimes, func(r model.Runtime) bool { return r.CatalogID == catalogID }) {
			return nil
		}
		time.Sleep(poll)
	}
	return errors.New("it took too long")
}

// setupDatabase makes sure the server service exists and runs, then creates the database and its
// user. The password goes to the .env file only.
func (j *job) setupDatabase() error {
	m, db := j.m, j.p.Database
	id := db.ServiceID
	if id != "" && !slices.ContainsFunc(m.St.Snapshot().Services, func(s model.Service) bool { return s.ID == id }) {
		return fmt.Errorf("The %s service was removed while the plan was being made.", kindLabel(db.Kind))
	}
	if id == "" {
		id = model.NewID("svc_")
		in := model.ServiceInput{Name: kindLabel(db.Kind), Kind: db.Kind, Port: j.p.newDBPort, Settings: map[string]string{}}
		err := m.St.Update(func(d *store.Data) error {
			sv, err := validate.Service(in, *d, "")
			if err != nil {
				return err
			}
			sv.ID = id
			d.Services = append(d.Services, sv)
			return nil
		})
		if err != nil {
			return fmt.Errorf("Creating the %s service failed: %v", kindLabel(db.Kind), err)
		}
		j.dbSvcID, j.dbSvcCreated = id, true
		m.emit("services.changed")
	}
	if m.Sup.State(id) != model.StateRunning {
		if err := m.Sup.StartWithDeps(id); err != nil {
			return fmt.Errorf("Starting %s failed: %v", kindLabel(db.Kind), err)
		}
		deadline := time.Now().Add(m.startTimeout())
		for m.Sup.State(id) != model.StateRunning {
			if st := m.Sup.State(id); st == model.StateStopped || time.Now().After(deadline) {
				return fmt.Errorf("%s did not start (see its log on the Services page).", kindLabel(db.Kind))
			}
			time.Sleep(m.poll())
		}
	}
	j.dbSvcID = id
	j.vals.DBPassword = randomPassword(j.vals)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := m.DB.CreateDatabase(ctx, id, db.Name); err != nil {
		msg := fmt.Sprintf("Creating the database %s failed: %v", db.Name, err)
		if strings.Contains(err.Error(), "exists") {
			msg += ". Delete it on the Databases page or choose another project name"
		}
		return errors.New(msg)
	}
	j.dbCreated = true
	if err := m.DB.CreateUser(ctx, id, db.User, j.vals.DBPassword, db.Name); err != nil {
		msg := fmt.Sprintf("Creating the database user %s failed: %v", db.User, err)
		if strings.Contains(err.Error(), "exists") {
			msg += ". Delete it on the Databases page or choose another project name"
		}
		return errors.New(msg)
	}
	j.userCreated = true
	j.vals.DBPort = portOf(m.St.Snapshot(), id)
	return nil
}

func portOf(d store.Data, id string) int {
	for _, s := range d.Services {
		if s.ID == id {
			return s.Port
		}
	}
	return 0
}

// writeEnv puts the framework's env lines into the project's .env file.
func (j *job) writeEnv(lines []frameworks.EnvLine) error {
	var out []envfile.Line
	for _, l := range lines {
		v, err := j.vals.Expand(l.Value)
		if err != nil {
			return err
		}
		out = append(out, envfile.Line{Key: l.Key, Value: v, IfMissing: l.IfMissing})
	}
	path, err := safeJoin(j.p.Dir, j.p.envFile())
	if err != nil {
		return err
	}
	if _, err := envfile.Apply(path, out); err != nil {
		return fmt.Errorf("Writing %s failed: %v", j.p.envFile(), err)
	}
	return nil
}

// create adds the services, the site and the project in one store update, so a failure leaves
// nothing half registered.
func (j *job) create() error {
	m, p, fw := j.m, j.p, j.p.fw
	var siteID, projectID string
	var created []string
	err := m.St.Update(func(d *store.Data) error {
		var svcIDs []string
		mainSvc, phpSvc := -1, -1
		for i, tpl := range fw.Services {
			in, err := j.serviceInput(i, tpl)
			if err != nil {
				return err
			}
			sv, err := validate.Service(in, *d, "")
			if err != nil {
				return fmt.Errorf("Creating the service %s failed: %v", in.Name, err)
			}
			sv.ID = j.svcIDs[i]
			d.Services = append(d.Services, sv)
			svcIDs = append(svcIDs, sv.ID)
			if mainSvc < 0 && sv.Port > 0 {
				mainSvc = i
			}
			if phpSvc < 0 && sv.Kind == "php" {
				phpSvc = i
			}
		}
		var siteIDs []string
		if t := fw.Site; t != nil {
			in := model.SiteInput{Name: p.req.Name, Domain: p.Domain, Type: t.Type, HTTPS: t.HTTPS}
			switch t.Type {
			case "static":
				in.Root = filepath.Join(p.Dir, filepath.FromSlash(t.Root))
			case "php":
				in.ServiceID = j.svcIDs[phpSvc]
			default:
				in.ServiceID = j.svcIDs[mainSvc]
			}
			s, err := sites.Validate(in, "", *d)
			if err != nil {
				return fmt.Errorf("Creating the site failed: %v", err)
			}
			s.ID = model.NewID("site_")
			d.Sites = append(d.Sites, s)
			siteIDs = append(siteIDs, s.ID)
		}
		pr, err := validate.Project(model.ProjectInput{Name: p.req.Name, ServiceIDs: svcIDs, SiteIDs: siteIDs}, *d)
		if err != nil {
			return fmt.Errorf("Creating the project failed: %v", err)
		}
		pr.ID = model.NewID("prj_")
		d.Projects = append(d.Projects, pr)
		created, projectID = svcIDs, pr.ID
		if len(siteIDs) > 0 {
			siteID = siteIDs[0]
		}
		return nil
	})
	if err != nil {
		return err
	}
	j.createdSvcs, j.siteID, j.projectID = created, siteID, projectID
	if len(created) > 0 {
		j.flushLog(created[0])
	}
	m.emit("services.changed")
	if siteID != "" {
		m.emit("sites.changed")
		if m.Sites != nil {
			if _, err := m.Sites.EnsureServers(); err != nil && len(created) > 0 {
				m.Sup.Log(created[0], "system", "The site server could not be set up: "+err.Error())
			}
			_ = m.Sites.Regenerate()
		}
	}
	m.emit("projects.changed")
	return nil
}

// serviceInput turns the framework's service template into the input the API would validate.
func (j *job) serviceInput(i int, tpl frameworks.ServiceTpl) (model.ServiceInput, error) {
	p, v := j.p, j.vals
	name, err := v.Expand(tpl.Name)
	if err != nil {
		return model.ServiceInput{}, err
	}
	command, err := v.Expand(tpl.Command)
	if err != nil {
		return model.ServiceInput{}, err
	}
	args, err := v.ExpandAll(tpl.Args)
	if err != nil {
		return model.ServiceInput{}, err
	}
	settings, err := v.ExpandMap(tpl.Settings)
	if err != nil {
		return model.ServiceInput{}, err
	}
	env, err := v.ExpandMap(tpl.Env)
	if err != nil {
		return model.ServiceInput{}, err
	}
	cwd, err := v.Expand(tpl.Cwd)
	if err != nil {
		return model.ServiceInput{}, err
	}
	in := model.ServiceInput{
		Name: name, Kind: tpl.Kind, Command: command, Args: args, Settings: settings, Env: env,
		Cwd: filepath.Join(p.Dir, filepath.FromSlash(cwd)), Port: p.ports[i],
		CustomArgs: tpl.Kind == "custom",
	}
	if tpl.Kind == "custom" {
		in.Settings = nil
	}
	if tpl.Kind == p.fw.Runtime.Kind && j.rt != nil && !j.rt.Default {
		in.RuntimeID = j.rt.ID
	}
	if tpl.UsesDatabase && j.dbSvcID != "" {
		in.DatabaseID, in.DependsOn = j.dbSvcID, []string{j.dbSvcID}
	}
	if tpl.Cwd == "" && len(p.envLines()) > 0 {
		in.EnvFile = p.envFile()
	}
	return in, nil
}

// flushLog writes the generator's output to the log of the new service.
func (j *job) flushLog(serviceID string) {
	for _, l := range j.out {
		j.m.Sup.Log(serviceID, "task", l)
	}
	j.out = nil
}

func (j *job) openEditor() {
	m, rt := j.m, j.p.editor
	args, env, err := launch.ToolDataArgs(*rt, m.Home)
	if err == nil {
		if m.Runtimes != nil {
			_ = m.Runtimes.EnsureEditorData(*rt)
		}
		err = m.Launch(rt.Binary, append(args, j.p.Dir), j.p.Dir, env)
	}
	if err != nil && len(j.createdSvcs) > 0 { // the project is made: a missing editor is no failure
		m.Sup.Log(j.createdSvcs[0], "system", "Opening the folder in "+rt.Name+" failed: "+err.Error())
	}
}

// fail removes what the job created, newest first, keeps the generated folder, and returns the
// error that says so.
func (j *job) fail(cause error) error {
	m := j.m
	var notes []string
	note := func(what string, err error) {
		if err != nil {
			notes = append(notes, fmt.Sprintf("%s: %v", what, err))
		}
	}
	if j.projectID != "" || j.siteID != "" || len(j.createdSvcs) > 0 {
		for _, id := range j.createdSvcs {
			note("removing a service", m.Sup.Remove(id))
		}
		note("removing the project", m.removeFromStore(j.projectID, j.siteID, j.createdSvcs))
		m.emit("projects.changed")
		m.emit("sites.changed")
		m.emit("services.changed")
		if m.Sites != nil {
			_ = m.Sites.Regenerate()
		}
	}
	if j.dbCreated || j.userCreated {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if j.dbCreated {
			note("dropping the database "+j.p.Database.Name, m.DB.DropDatabases(ctx, j.dbSvcID, []string{j.p.Database.Name}))
		}
		if j.userCreated {
			note("dropping the database user "+j.p.Database.User, m.DB.DropUser(ctx, j.dbSvcID, j.p.Database.User))
		}
		cancel()
	}
	if j.dbSvcCreated {
		note("removing the database service", m.Sup.Remove(j.dbSvcID))
		note("removing the database service", m.removeFromStore("", "", []string{j.dbSvcID}))
		m.emit("services.changed")
	}
	msg := strings.TrimRight(cause.Error(), ". ") + "."
	if dir := j.p.dirOrEmpty(); dir != "" {
		if j.dirCreated {
			_ = os.Remove(dir) // only succeeds while the folder is empty: nothing of ours is ever deleted
		}
		if _, err := os.Stat(dir); err == nil {
			msg += fmt.Sprintf(" The folder %s was kept.", dir)
		}
	}
	if len(j.out) > 0 && j.projectID == "" {
		path := filepath.Join(m.Home, "logs", "newproject-"+j.p.Slug+".log")
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil &&
			os.WriteFile(path, []byte(strings.Join(j.out, "\n")+"\n"), 0o644) == nil {
			msg += fmt.Sprintf(" The output of the generator is in %s.", path)
		}
	}
	if len(notes) > 0 {
		msg += " Cleaning up was not complete (" + strings.Join(notes, "; ") + ")."
	}
	return errors.New(msg)
}

func (p *planned) dirOrEmpty() string {
	if p == nil {
		return ""
	}
	return p.Dir
}

// removeFromStore deletes the project, the site and the services from the state.
func (m *Manager) removeFromStore(projectID, siteID string, svcIDs []string) error {
	return m.St.Update(func(d *store.Data) error {
		d.Projects = slices.DeleteFunc(d.Projects, func(p model.Project) bool { return p.ID == projectID && projectID != "" })
		d.Sites = slices.DeleteFunc(d.Sites, func(s model.Site) bool { return s.ID == siteID && siteID != "" })
		d.Services = slices.DeleteFunc(d.Services, func(s model.Service) bool { return slices.Contains(svcIDs, s.ID) })
		return nil
	})
}

// safeJoin joins a relative path (with / or the OS separator) below dir; it refuses anything that
// would leave dir.
func safeJoin(dir, rel string) (string, error) {
	r := filepath.FromSlash(rel)
	if rel == "" || filepath.IsAbs(r) || filepath.VolumeName(r) != "" || strings.ContainsAny(rel, "\x00:") ||
		strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return "", fmt.Errorf("%q is not a path inside the project folder", rel)
	}
	p := filepath.Join(dir, r)
	if rr, err := filepath.Rel(dir, p); err != nil || rr == ".." || strings.HasPrefix(rr, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q is not a path inside the project folder", rel)
	}
	return p, nil
}
