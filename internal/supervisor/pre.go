package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/model"
)

// PreRunner runs one pre-step program and returns when it ended. Its output goes to out (merged,
// line by line). nil in a Supervisor means the real one (no console window, the process tree is
// ended when the timeout runs out); tests replace it through Supervisor.PreRun.
type PreRunner func(ctx context.Context, bin string, args []string, dir string, env []string, out io.Writer) error

const defaultPreTimeout = 5 * time.Minute

// runPre runs the pre-steps of a resolved service (kinds.Plan.Pre) in order. A step whose Skip file
// exists is not run. Each step and its output are written to the service's log; a failing step
// is an error that names the step.
func (s *Supervisor) runPre(id string, r *resolved) error {
	for _, st := range r.pre {
		if st.Skip != "" {
			if _, err := os.Stat(st.Skip); err == nil {
				continue
			}
		}
		bin := st.Program
		if !filepath.IsAbs(bin) {
			found, err := FindProgram(bin, r.env)
			if err != nil {
				return fmt.Errorf("%s: %v", st.Label, err)
			}
			bin = found
		}
		dir := st.Dir
		if dir == "" {
			dir = r.workdir
		}
		if dir != "" {
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				dir = "" // the service's own folder is checked later with a clearer message
			}
		}
		timeout := defaultPreTimeout
		if st.Timeout > 0 {
			timeout = time.Duration(st.Timeout) * time.Second
		}
		s.logLine(id, "system", fmt.Sprintf("%s: %s %s", st.Label, filepath.Base(bin), strings.Join(st.Args, " ")))
		out := &lineWriter{emit: func(t string) { s.logLine(id, "stdout", t) }}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		run := s.PreRun
		if run == nil {
			run = realPreRun
		}
		err := run(ctx, bin, st.Args, dir, r.env, out)
		cancel()
		out.Flush()
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("%s: timed out after %s", st.Label, timeout)
			}
			return fmt.Errorf("%s failed: %v", st.Label, err)
		}
	}
	return nil
}

// PrepareService does what a start does before the program runs, without starting it: the
// folders and files of the plan, then the pre-steps. A one-off task of the service calls it, so
// the tools the pre-steps make (the virtual environment, Corepack) exist.
func (s *Supervisor) PrepareService(svc model.Service) error {
	r, err := s.resolveWith(svc, s.st.Snapshot(), true)
	if err != nil {
		return err
	}
	if err := s.prepare(svc.ID, r); err != nil {
		return apperr.Conflict("%v", err)
	}
	if err := s.runPre(svc.ID, r); err != nil {
		return apperr.Conflict("%v", err)
	}
	return nil
}

func realPreRun(ctx context.Context, bin string, args []string, dir string, env []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, bin, args...) // never a shell
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 2 * time.Second
	configure(cmd)
	return cmd.Run()
}

// FindProgram finds a program the way the service would: a name without a path is looked up in
// the PATH of env (the service's environment, as Environ builds it), with the extensions of
// PATHEXT on Windows. An absolute path must exist. A name with a separator is refused.
func FindProgram(name string, env []string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\x00\r\n") {
		return "", errors.New("the program name is empty or has a control character")
	}
	if filepath.IsAbs(name) {
		if fi, err := os.Stat(name); err != nil || fi.IsDir() {
			return "", fmt.Errorf("%s does not exist", name)
		}
		return name, nil
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%q is not a program name; give a name or an absolute path", name)
	}
	path, pathext := "", ".COM;.EXE;.BAT;.CMD"
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		switch {
		case ok && strings.EqualFold(k, "PATH"):
			path = v
		case ok && strings.EqualFold(k, "PATHEXT"):
			pathext = v
		}
	}
	exts := []string{""}
	if filepath.Ext(name) == "" && os.PathSeparator == '\\' {
		exts = strings.Split(strings.ToLower(pathext), ";")
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		for _, e := range exts {
			p := filepath.Join(dir, name+e)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s was not found on the service's PATH", name)
}
