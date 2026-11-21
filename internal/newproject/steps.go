package newproject

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"zero2dev/internal/frameworks"
	"zero2dev/internal/installer"
	"zero2dev/internal/model"
	"zero2dev/internal/supervisor"
	"zero2dev/internal/tasks"
)

// DefaultStepTimeout is how long a generator program may run.
const DefaultStepTimeout = 15 * time.Minute

const maxDownload = 1 << 30

// generate runs the framework's steps in order.
func (j *job) generate() error {
	p := j.p
	steps := p.fw.Steps
	var env []string
	var svc model.Service
	needEnv := false
	for _, st := range steps {
		needEnv = needEnv || st.Type == frameworks.StepRun
	}
	if needEnv {
		var err error
		if svc, env, err = j.runtimeEnv(); err != nil {
			return err
		}
	}
	for i, st := range steps {
		j.progress(45, 80, i, len(steps), p.Steps[len(p.Installs)+dbStep(p)+i])
		label, _ := j.vals.Expand(st.Label)
		var err error
		switch st.Type {
		case frameworks.StepRun:
			err = j.runStep(st, svc, env)
		case frameworks.StepDownload:
			err = j.downloadStep(st)
		case frameworks.StepFiles:
			err = j.filesStep(st)
		}
		if err != nil {
			return fmt.Errorf("%s failed: %v", label, err)
		}
	}
	if fi, err := os.Stat(p.Dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("The generator did not make the folder %s", p.Dir)
	}
	return nil
}

func dbStep(p *planned) int {
	if p.Database != nil {
		return 1
	}
	return 0
}

// ensureDir makes the project folder (remembering that this job made it).
func (j *job) ensureDir() error {
	if _, err := os.Stat(j.p.Dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(j.p.Dir, 0o755); err != nil {
		return err
	}
	j.dirCreated = true
	return nil
}

// runtimeEnv builds the environment the generator programs run with: a service of the framework's
// runtime kind that is not stored, so its runtime folder is first on PATH and the kind's own
// variables (JAVA_HOME, the Go and Composer folders ...) are set. A Python framework gets its
// virtual environment made here.
func (j *job) runtimeEnv() (model.Service, []string, error) {
	m, p := j.m, j.p
	k := p.fw.Runtime.Kind
	if k == "" {
		return model.Service{}, os.Environ(), nil
	}
	svc := model.Service{Name: p.req.Name, Kind: k, Cwd: p.Dir, Env: map[string]string{}, Settings: map[string]string{}, Args: []string{}}
	for i, tpl := range p.fw.Services {
		if tpl.Kind == k {
			settings, err := j.vals.ExpandMap(tpl.Settings)
			if err != nil {
				return svc, nil, err
			}
			svc.ID, svc.Settings = j.svcIDs[i], settings
			break
		}
	}
	if svc.ID == "" {
		svc.ID = model.NewID("svc_")
	}
	if j.rt != nil && !j.rt.Default {
		svc.RuntimeID = j.rt.ID
	}
	if k == "python" {
		if err := j.ensureDir(); err != nil {
			return svc, nil, err
		}
		j.log("Making the virtual environment " + j.vals.Venv)
		if err := m.Sup.PrepareService(svc); err != nil {
			return svc, nil, fmt.Errorf("Making the virtual environment failed: %v", err)
		}
	}
	env, err := m.Sup.Environ(svc)
	if err != nil {
		return svc, nil, fmt.Errorf("Preparing the %s environment failed: %v", kindLabel(k), err)
	}
	return svc, env, nil
}

func setEnv(env []string, k, v string) []string {
	for i, kv := range env {
		if n, _, ok := strings.Cut(kv, "="); ok && strings.EqualFold(n, k) {
			env[i] = k + "=" + v
			return env
		}
	}
	return append(env, k+"="+v)
}

// runStep runs one program: no shell, the program found in the runtime's folder or on its PATH,
// the output kept for the service's log.
func (j *job) runStep(st frameworks.Step, svc model.Service, env []string) error {
	m, p := j.m, j.p
	args, err := j.vals.ExpandAll(st.Args)
	if err != nil {
		return err
	}
	dir := p.vals.Parent
	if st.In == "project" {
		if err := j.ensureDir(); err != nil {
			return err
		}
		dir = p.Dir
	}
	env = append([]string(nil), env...)
	for k, v := range st.Env {
		ev, err := j.vals.Expand(v)
		if err != nil {
			return err
		}
		env = setEnv(env, k, ev)
	}
	var bin string
	logged := false
	switch prog := st.Program; {
	case prog == "composer":
		var pre []string
		if bin, pre, err = m.Sup.ComposerCommand(composerService(svc)); err != nil {
			return err
		}
		logged = true
		j.log("$ " + strings.TrimSpace(st.Program+" "+strings.Join(args, " ")))
		args = append(pre, args...)
	case strings.HasPrefix(prog, "./"):
		bin = filepath.Join(dir, prog[2:])
		if !fileExists(bin) {
			return fmt.Errorf("%s was not found in %s", prog, dir)
		}
	default:
		if bin, err = supervisor.FindProgram(prog, env); err != nil {
			return err
		}
	}
	if !logged {
		j.log("$ " + strings.TrimSpace(st.Program+" "+strings.Join(args, " ")))
	}
	timeout := DefaultStepTimeout
	if st.Timeout > 0 {
		timeout = time.Duration(st.Timeout) * time.Second
	}
	out := &outWriter{emit: j.log, limit: 6}
	run := m.Run
	if run == nil {
		run = tasks.DefaultRunner
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err = run(ctx, tasks.Cmd{Bin: bin, Args: args, Dir: dir, Env: env, Timeout: timeout, Out: out})
	out.flush()
	if err == nil {
		return nil
	}
	name := strings.TrimSuffix(filepath.Base(st.Program), filepath.Ext(st.Program))
	var ee *tasks.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s timed out after %s", name, timeout)
	case errors.As(err, &ee):
		msg := fmt.Sprintf("%s exited with code %d", name, ee.Code)
		if t := out.lastLines(); t != "" {
			msg += ": " + t
		}
		return errors.New(msg)
	}
	return fmt.Errorf("%s: %v", name, err)
}

// composerService is the service Composer runs for: the framework's PHP.
func composerService(svc model.Service) model.Service {
	svc.Kind = "php"
	return svc
}

// outWriter sends whole lines to emit and keeps the last few.
type outWriter struct {
	mu    sync.Mutex
	emit  func(string)
	part  []byte
	tail  []string
	limit int
}

func (w *outWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.part = append(w.part, b...)
	for {
		i := bytes.IndexByte(w.part, '\n')
		if i < 0 {
			break
		}
		w.line(string(w.part[:i]))
		w.part = w.part[i+1:]
	}
	if len(w.part) > 16*1024 {
		w.line(string(w.part))
		w.part = w.part[:0]
	}
	return len(b), nil
}

func (w *outWriter) line(s string) {
	s = strings.TrimRight(s, "\r")
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	if strings.TrimSpace(s) == "" {
		return
	}
	w.emit(s)
	w.tail = append(w.tail, strings.TrimSpace(s))
	if len(w.tail) > w.limit {
		w.tail = w.tail[len(w.tail)-w.limit:]
	}
}

func (w *outWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.part) > 0 {
		w.line(string(w.part))
		w.part = nil
	}
}

func (w *outWriter) lastLines() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.tail, "; ")
}

// filesStep writes the starter files. A file that exists is left as it is.
func (j *job) filesStep(st frameworks.Step) error {
	if err := j.ensureDir(); err != nil {
		return err
	}
	names := make([]string, 0, len(st.Files))
	for name := range st.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target, err := safeJoin(j.p.Dir, name)
		if err != nil {
			return err
		}
		content, err := j.vals.Expand(st.Files[name])
		if err != nil {
			return err
		}
		if _, err := os.Lstat(target); err == nil {
			j.log("Kept the existing " + name)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // never overwrites
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return err
		}
		_, err = f.WriteString(content)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		j.log("Wrote " + name)
	}
	return nil
}

func (m *Manager) httpClient() *http.Client {
	if m.HTTP != nil {
		return m.HTTP
	}
	return installer.HTTPClient(m.St.Snapshot().Settings.DownloadProxy)
}

// downloadStep downloads a zip, verifies its checksum and unpacks it into the project folder.
func (j *job) downloadStep(st frameworks.Step) error {
	m := j.m
	url, err := j.vals.Expand(st.URL)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(url, "https://") && !(m.AllowHTTP && strings.HasPrefix(url, "http://")) {
		return fmt.Errorf("%s is not a https address", url)
	}
	sum, err := j.vals.Expand(st.Checksum)
	if err != nil {
		return err
	}
	if err := j.ensureDir(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultStepTimeout)
	defer cancel()
	client := m.httpClient()
	var want, algo string
	if !st.Unverified {
		if want, algo, err = expectedHash(ctx, client, sum); err != nil {
			return err
		}
	}
	tmpDir := filepath.Join(m.Home, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(tmpDir, "newproject-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // our own download, never the project's files
	var h hash.Hash = sha256.New()
	if algo == "sha1" {
		h = sha1.New()
	}
	j.log("Downloading " + url)
	err = func() error {
		defer f.Close()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("the server answered %s", resp.Status)
		}
		n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxDownload+1))
		if err == nil && n > maxDownload {
			err = errors.New("the download is larger than 1 GB")
		}
		return err
	}()
	if err != nil {
		return err
	}
	if !st.Unverified {
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
			return fmt.Errorf("the download does not match its %s checksum (expected %s, got %s)", algo, want, got)
		}
		j.log("The " + algo + " checksum matches")
	} else {
		j.log("No checksum is published for this download: " + st.Reason)
	}
	j.log("Unpacking into " + j.p.Dir)
	return installer.ExtractZip(f.Name(), j.p.Dir, st.Strip)
}

// expectedHash reads the checksum of a step: "sha256:<hex>", "sha1:<hex>" or a https address of a
// file whose first word is the hex digest (as wordpress.org's .sha1 file, or "<hex>  <name>").
func expectedHash(ctx context.Context, c *http.Client, spec string) (hexSum, algo string, err error) {
	if a, v, ok := strings.Cut(spec, ":"); ok && (a == "sha256" || a == "sha1") {
		return strings.ToLower(v), a, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("reading the checksum: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("reading the checksum: the server answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return "", "", errors.New("the checksum file is empty")
	}
	sum := strings.ToLower(fields[0])
	if _, err := hex.DecodeString(sum); err != nil {
		return "", "", errors.New("the checksum file does not hold a hex digest")
	}
	switch len(sum) {
	case 40:
		return sum, "sha1", nil
	case 64:
		return sum, "sha256", nil
	}
	return "", "", errors.New("the checksum file holds an unknown digest length")
}
