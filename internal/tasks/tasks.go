// Package tasks runs one-off tasks of a service (npm install, composer install, go mod download,
// pip install -r requirements.txt, migrations): a program with its arguments, in the service's
// project folder, with the environment the service starts with, its output going to the service's
// log. A task is a job (type "task", key "task:<serviceId>"); only one runs per service.
//
// Contract (docs/API.md, "Amendment: languages, tasks and new projects (0.0.6)"). Wave 0 stub:
// the signatures are final, the bodies are filled in by the tasks work.
package tasks

import (
	"context"
	"errors"
	"io"
	"time"

	"zero2dev/internal/jobs"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/supervisor"
)

// ErrNotImplemented is returned by the Wave 0 stubs (the API answers 501).
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
// console window, the process tree is killed on timeout); tests replace it.
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
}

// New returns a Manager.
func New(home string, st *store.Store, sup *supervisor.Supervisor, j *jobs.Registry) *Manager {
	return &Manager{Home: home, St: st, Sup: sup, Jobs: j}
}

// List returns the tasks of a service.
func (m *Manager) List(serviceID string) (List, error) { return List{}, ErrNotImplemented }

// Start runs a task of a service as a job. It is a 404 for an unknown service or preset, a 400
// for a bad program or arguments, a 409 while a task of the service runs or the service is
// reserved by another operation.
func (m *Manager) Start(serviceID string, req Request) (model.Job, error) {
	return model.Job{}, ErrNotImplemented
}
