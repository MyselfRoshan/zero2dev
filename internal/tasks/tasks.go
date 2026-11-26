// Package tasks runs one-off tasks of a service (npm install, composer install, go mod download,
// pip install -r requirements.txt, migrations): a program with its arguments, in the service's
// project folder, with the environment the service starts with, its output going to the service's
// log. A task is a job (type "task", key "task:<serviceId>"); only one runs per service.
//
// Contract: docs/API.md, "Amendment: languages, tasks and new projects (0.0.6)" and its
// "Tasks and detection details" subsection.
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/jobs"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

// ErrNotImplemented is what the API answers 501 for: a server without a Manager.
var ErrNotImplemented = errors.New("not implemented yet")

// DefaultTimeout is how long a task may run when its preset sets none.
const DefaultTimeout = 10 * time.Minute

// Cmd is one program to run. Nothing goes through a shell: Bin is the program, Args its arguments.
type Cmd struct {
	Bin     string
	Args    []string
	Dir     string
	Env     []string
	Timeout time.Duration
	// Out gets the merged output (stdout and stderr), line by line as it comes.
	Out io.Writer
}

// Runner runs a Cmd and returns when it ended. nil in a Manager means the real runner (no
// console window, the process tree is killed on timeout); tests replace it. ctx carries the
// timeout. A program that ended with a non-zero code is reported as *ExitError.
type Runner func(ctx context.Context, c Cmd) error

// Request is the body of POST /api/services/{id}/tasks: a preset of the service's kind
// (Preset is its id), or a program of your own (Program and Args). Exactly one of them.
type Request struct {
	Preset  string   `json:"preset"`
	Program string   `json:"program"`
	Args    []string `json:"args"`
}

// Script is one script of the project (package.json "scripts"), offered as a task that runs it
// with the service's package manager.
type Script struct {
	Name    string `json:"name"`
	Command string `json:"command"` // what the script runs, for the label
}

// List is GET /api/services/{id}/tasks: what can be run for the service now.
type List struct {
	// Presets are the tasks of the service's kind whose "when" file exists in its folder.
	Presets []kinds.Task `json:"presets"`
	// Scripts are the project's own scripts (Node services with a package.json), else [].
	Scripts []Script `json:"scripts"`
	// Running is the id of the task job running for the service now, "" when none.
	Running string `json:"running"`
}

// Manager does the work. Fields may be replaced in tests before first use.
type Manager struct {
	St   *store.Store
	Sup  *supervisor.Supervisor
	Jobs *jobs.Registry
	Home string
	Run  Runner
	// Composer resolves the program "composer" for a service: the binary to run (the service's PHP)
	// and the arguments that go before the task's own (the composer.phar path). nil, or an error,
	// is a 409: Composer is not installed.
	// New connects it to supervisor.ComposerCommand (internal/supervisor/composer.go); tests replace it.
	Composer func(svc model.Service) (bin string, args []string, err error)

	mu      sync.Mutex
	running map[string]string // service id -> id of its task job
}

// New returns a Manager.
func New(home string, st *store.Store, sup *supervisor.Supervisor, j *jobs.Registry) *Manager {
	m := &Manager{Home: home, St: st, Sup: sup, Jobs: j}
	if sup != nil {
		m.Composer = sup.ComposerCommand
	}
	return m
}

func (m *Manager) service(id string) (model.Service, error) {
	for _, sv := range m.St.Snapshot().Services {
		if sv.ID == id {
			return sv, nil
		}
	}
	return model.Service{}, apperr.NotFound("service %s not found", id)
}

// List returns the tasks of a service.
func (m *Manager) List(serviceID string) (List, error) {
	svc, err := m.service(serviceID)
	if err != nil {
		return List{}, err
	}
	l := List{Presets: []kinds.Task{}, Scripts: []Script{}}
	if p, ok := kinds.Lookup(svc.Kind); ok {
		for _, t := range p.Tasks {
			if t.When == "" || existsIn(svc.Cwd, t.When) {
				l.Presets = append(l.Presets, t)
			}
		}
	}
	if svc.Kind == "node" {
		l.Scripts = packageScripts(svc.Cwd)
	}
	m.mu.Lock()
	id := m.running[serviceID]
	m.mu.Unlock()
	if id != "" {
		if j, ok := m.Jobs.Get(id); ok && j.State == "running" {
			l.Running = id
		}
	}
	return l, nil
}

// existsIn reports whether rel (a relative file or folder, with "/" or "\") exists below dir.
func existsIn(dir, rel string) bool {
	rel = strings.ReplaceAll(rel, `\`, "/")
	if dir == "" || rel == "" || filepath.IsAbs(rel) || slices.Contains(strings.Split(rel, "/"), "..") {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

// packageScripts reads "scripts" of dir/package.json, sorted by name. A missing or damaged file is
// no scripts.
func packageScripts(dir string) []Script {
	out := []Script{}
	if dir == "" {
		return out
	}
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return out
	}
	var pkg struct {
		Scripts map[string]any `json:"scripts"`
	}
	if json.Unmarshal(b, &pkg) != nil {
		return out
	}
	for name, v := range pkg.Scripts {
		if cmd, ok := v.(string); ok && name != "" {
			out = append(out, Script{Name: name, Command: cmd})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

var settingRef = regexp.MustCompile(`\{s\.([A-Za-z0-9_]+)\}`)

// expand replaces {s.key} (the service's setting, "" when unknown; packageManager is npm unless
// set) and {port} in s.
func expand(s string, svc model.Service) string {
	eff := kinds.Defaults(svc.Kind)
	for k, v := range svc.Settings {
		eff[k] = v
	}
	if eff["packageManager"] == "" {
		eff["packageManager"] = "npm"
	}
	s = settingRef.ReplaceAllStringFunc(s, func(m string) string { return eff[m[3:len(m)-1]] })
	port := ""
	if svc.Port > 0 {
		port = strconv.Itoa(svc.Port)
	}
	return strings.ReplaceAll(s, "{port}", port)
}

const maxArgs = 200

func checkProgram(name string) error {
	switch {
	case name == "":
		return apperr.BadRequest("the program is empty")
	case strings.ContainsAny(name, "\x00\r\n"):
		return apperr.BadRequest("the program name has a control character")
	case strings.ContainsAny(name, `/\`):
		return apperr.BadRequest("%q is not a program name: give the name only, the service's PATH decides where it is", name)
	}
	return nil
}

func checkArgs(args []string) error {
	if len(args) > maxArgs {
		return apperr.BadRequest("too many arguments (at most %d)", maxArgs)
	}
	for _, a := range args {
		if strings.ContainsAny(a, "\x00\r\n") {
			return apperr.BadRequest("an argument has a NUL or a line break")
		}
	}
	return nil
}

var errNoComposer = apperr.Conflict("Composer is not installed: install it on the Runtimes page")

// Start runs a task of a service as a job. It is a 404 for an unknown service or preset, a 400
// for a bad program or arguments, a 409 while a task of the service runs or the service is
// reserved by another operation.
func (m *Manager) Start(serviceID string, req Request) (model.Job, error) {
	svc, err := m.service(serviceID)
	if err != nil {
		return model.Job{}, err
	}
	if (req.Preset == "") == (req.Program == "") {
		return model.Job{}, apperr.BadRequest("give either a preset or a program, not both and not neither")
	}
	label, program, args := "", req.Program, slices.Clone(req.Args)
	timeout := DefaultTimeout
	if req.Preset != "" {
		if len(req.Args) > 0 {
			return model.Job{}, apperr.BadRequest("a preset takes no arguments of your own")
		}
		var t *kinds.Task
		if p, ok := kinds.Lookup(svc.Kind); ok {
			for i := range p.Tasks {
				if p.Tasks[i].ID == req.Preset {
					t = &p.Tasks[i]
				}
			}
		}
		if t == nil {
			return model.Job{}, apperr.NotFound("%s has no task %q", svc.Name, req.Preset)
		}
		if t.When != "" && !existsIn(svc.Cwd, t.When) {
			return model.Job{}, apperr.Conflict("%s needs %s in the project folder", t.Label, t.When)
		}
		label, program = t.Label, expand(t.Program, svc)
		args = make([]string, len(t.Args))
		for i, a := range t.Args {
			args[i] = expand(a, svc)
		}
		if t.Timeout > 0 {
			timeout = time.Duration(t.Timeout) * time.Second
		}
	}
	if err := checkProgram(program); err != nil {
		return model.Job{}, err
	}
	if err := checkArgs(args); err != nil {
		return model.Job{}, err
	}
	if svc.Cwd == "" {
		return model.Job{}, apperr.Conflict("%s has no project folder", svc.Name)
	}
	if fi, err := os.Stat(svc.Cwd); err != nil || !fi.IsDir() {
		return model.Job{}, apperr.Conflict("the project folder %s does not exist", svc.Cwd)
	}
	if err := m.Sup.Busy(svc.ID); err != nil {
		return model.Job{}, err
	}
	// Composer runs as php <composer.phar>. That is known before the job, so a missing Composer
	// is a 409 right away.
	var composerBin string
	var composerPre []string
	if program == "composer" {
		if m.Composer == nil {
			return model.Job{}, errNoComposer
		}
		if composerBin, composerPre, err = m.Composer(svc); err != nil || composerBin == "" {
			return model.Job{}, errNoComposer
		}
	}
	if label == "" { // a program of your own: the label is the command line
		label = commandLine(program, args)
	}
	h, err := m.Jobs.Start(model.JobTask, "task:"+svc.ID, label, func(j *model.Job) { j.ServiceID = svc.ID })
	if err != nil {
		return model.Job{}, err
	}
	m.mu.Lock()
	if m.running == nil {
		m.running = map[string]string{}
	}
	m.running[svc.ID] = h.ID()
	m.mu.Unlock()
	job := h.Job()
	go func() {
		defer func() {
			m.mu.Lock()
			if m.running[svc.ID] == h.ID() {
				delete(m.running, svc.ID)
			}
			m.mu.Unlock()
			if p := recover(); p != nil {
				h.Fail(apperr.Internal("task crashed: %v", p))
			}
		}()
		if err := m.execute(svc, program, args, composerBin, composerPre, timeout); err != nil {
			h.Fail(err)
			return
		}
		h.Done()
	}()
	return job, nil
}

func commandLine(program string, args []string) string {
	return strings.TrimSpace(program + " " + strings.Join(args, " "))
}

// execute prepares the service (virtual environment, Corepack), finds the program the way the
// service would, and runs it with its output going to the service's log.
func (m *Manager) execute(svc model.Service, program string, args []string, composerBin string, composerPre []string, timeout time.Duration) error {
	log := func(s string) { m.Sup.Log(svc.ID, "task", s) }
	log("$ " + commandLine(program, args))
	if err := m.Sup.PrepareService(svc); err != nil {
		log("Preparing the service failed: " + err.Error())
		return err
	}
	env, err := m.Sup.Environ(svc)
	if err != nil {
		return err
	}
	bin, runArgs := composerBin, append(slices.Clone(composerPre), args...)
	if composerBin == "" {
		if bin, err = supervisor.FindProgram(program, env); err != nil {
			log(err.Error())
			return fmt.Errorf("%s: %v", program, err)
		}
		runArgs = args
	}
	out := &lineWriter{emit: log, limit: 5}
	run := m.Run
	if run == nil {
		run = DefaultRunner
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err = run(ctx, Cmd{Bin: bin, Args: runArgs, Dir: svc.Cwd, Env: env, Timeout: timeout, Out: out})
	out.flush()
	if err == nil {
		return nil
	}
	name := progName(program)
	var ee *ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		msg := fmt.Sprintf("%s timed out after %s", name, timeout)
		log(msg)
		return errors.New(msg)
	case errors.As(err, &ee):
		msg := fmt.Sprintf("%s exited with code %d", name, ee.Code)
		log(msg)
		if t := out.lastLines(); t != "" {
			msg += ": " + t
		}
		return errors.New(msg)
	}
	msg := fmt.Sprintf("%s: %v", name, err)
	log(msg)
	return errors.New(msg)
}
