// Package installer downloads (or imports), verifies and extracts catalog
// runtimes. See docs/API.md "Installer hardening" and "Import".
package installer

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/catalog"
	"zero2dev/internal/model"
	"zero2dev/internal/runtimes"
	"zero2dev/internal/store"
)

const maxDownload = 2 << 30

// Emitter receives events (events.Hub).
type Emitter interface{ Publish(typ string, data any) }

// Installer runs install and import jobs.
type Installer struct {
	home string
	st   *store.Store
	rt   *runtimes.Manager
	em   Emitter
	log  *fileLog

	Catalog func() ([]model.CatalogEntry, error)
	// Client overrides the HTTP client (tests); nil builds one from settings.
	Client  *http.Client
	Version string
	// Rename is injectable so tests can simulate Windows sharing violations.
	Rename         func(oldpath, newpath string) error
	Sleep          func(time.Duration)
	RenameAttempts int
	StallTimeout   time.Duration
	RetryDelays    []time.Duration
	HashTimeout    time.Duration

	mu   sync.Mutex
	jobs map[string]*job
}

type job struct {
	model.Job
	catalogID string
	finished  time.Time
}

// New creates an Installer.
func New(home string, st *store.Store, rt *runtimes.Manager, em Emitter) *Installer {
	return &Installer{
		home: home, st: st, rt: rt, em: em, Version: "0.0.1",
		log:     &fileLog{path: filepath.Join(home, "logs", "installer.log")},
		Catalog: func() ([]model.CatalogEntry, error) { return catalog.Load(home) },
		Rename:  os.Rename, Sleep: time.Sleep, RenameAttempts: 20,
		StallTimeout: 60 * time.Second, RetryDelays: []time.Duration{time.Second, 3 * time.Second},
		HashTimeout: 10 * time.Second,
		jobs:        map[string]*job{},
	}
}

// LogTail returns the last n lines of installer.log.
func (in *Installer) LogTail(n int) []string { return in.log.Tail(n) }

// Get returns a job by id.
func (in *Installer) Get(id string) (model.Job, bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	j, ok := in.jobs[id]
	if !ok {
		return model.Job{}, false
	}
	return j.Job, true
}

// prepare finds the entry and applies the checks shared by install/import.
func (in *Installer) prepare(catalogID string) (model.CatalogEntry, error) {
	es, err := in.Catalog()
	if err != nil {
		return model.CatalogEntry{}, apperr.Internal("catalog: %v", err)
	}
	e, ok := catalog.Find(es, catalogID)
	if !ok {
		return e, apperr.NotFound("catalog entry %q not found", catalogID)
	}
	e.FileName = catalog.FileName(e.URL)
	if !catalog.Supported(e) {
		return e, apperr.BadRequest("%s is for %s/%s, this machine cannot run it", e.ID, e.OS, e.Arch)
	}
	return e, nil
}

// register creates the job unless the entry is installed or being installed.
func (in *Installer) register(e model.CatalogEntry, typ, step string) (*job, error) {
	if catalog.Annotate([]model.CatalogEntry{e}, in.home, in.st.Snapshot().Runtimes)[0].Installed {
		return nil, apperr.Conflict("%s is already installed", e.ID)
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	for id, j := range in.jobs { // forget old finished jobs
		if !j.finished.IsZero() && time.Since(j.finished) > time.Hour {
			delete(in.jobs, id)
		}
	}
	for _, j := range in.jobs {
		if j.catalogID == e.ID && j.State == "running" {
			return nil, apperr.Conflict("%s is already being installed (%s)", e.ID, j.ID)
		}
	}
	j := &job{Job: model.Job{ID: model.NewID("job_"), Type: typ, State: "running", Step: step}, catalogID: e.ID}
	in.jobs[j.ID] = j
	return j, nil
}

// Start validates the request and runs the install in the background.
func (in *Installer) Start(catalogID string) (model.Job, error) {
	e, err := in.prepare(catalogID)
	if err != nil {
		return model.Job{}, err
	}
	j, err := in.register(e, "install", "Downloading")
	if err != nil {
		return model.Job{}, err
	}
	snap := in.snapshot(j)
	go in.run(j, e, func(ctx context.Context) (string, string, string, error) {
		return in.fetchArchive(ctx, j, e)
	})
	return snap, nil
}

// Import verifies and installs an archive the user downloaded themselves.
// sha is optional; without it the published checksum is fetched (10 s).
func (in *Installer) Import(catalogID, filePath, sha string) (model.Job, error) {
	e, err := in.prepare(catalogID)
	if err != nil {
		return model.Job{}, err
	}
	if filePath == "" || !filepath.IsAbs(filePath) {
		return model.Job{}, apperr.BadRequest("filePath must be an absolute path")
	}
	filePath = filepath.Clean(filePath)
	if st, err := os.Stat(filePath); err != nil || !st.Mode().IsRegular() {
		return model.Job{}, apperr.BadRequest("filePath %s does not exist or is not a file", filePath)
	}
	if filepath.Base(filePath) != e.FileName {
		return model.Job{}, apperr.BadRequest("the file must be named %s (got %s)", e.FileName, filepath.Base(filePath))
	}
	sha = strings.TrimSpace(sha)
	if sha != "" && !hexRE.MatchString(sha) {
		return model.Job{}, apperr.BadRequest("checksum must be a SHA-256 (64) or SHA-512 (128) hexadecimal digest")
	}
	j, err := in.register(e, "import", "Importing") // conflict checks first
	if err != nil {
		return model.Job{}, err
	}
	want := strings.ToLower(sha)
	if want == "" {
		want = in.publishedHash(e)
	}
	if want == "" {
		in.mu.Lock()
		delete(in.jobs, j.ID)
		in.mu.Unlock()
		cu := e.Checksum.URL
		if cu == "" {
			cu = "the publisher's download page (" + e.Homepage + ")"
		}
		return model.Job{}, apperr.BadRequest("Cannot verify the file because the published checksum could not be fetched. Paste the SHA-256 or SHA-512 for %s from %s into the checksum field.", e.FileName, cu)
	}
	snap := in.snapshot(j)
	go in.run(j, e, func(ctx context.Context) (string, string, string, error) {
		return in.copyArchive(ctx, j, filePath, want)
	})
	return snap, nil
}

// publishedHash returns the published checksum or "" if unavailable.
func (in *Installer) publishedHash(e model.CatalogEntry) string {
	ctx, cancel := context.WithTimeout(context.Background(), in.HashTimeout)
	defer cancel()
	h, err := in.expectedHash(ctx, "import", e)
	if err != nil {
		in.log.printf("import", "published checksum unavailable for %s: %v", e.ID, err)
		return ""
	}
	return h
}

func (in *Installer) snapshot(j *job) model.Job {
	in.mu.Lock()
	defer in.mu.Unlock()
	return j.Job
}

func (in *Installer) progress(j *job, percent int, step string) {
	in.mu.Lock()
	if percent == j.Percent && step == j.Step {
		in.mu.Unlock()
		return
	}
	stepChanged := step != j.Step
	j.Percent, j.Step = percent, step
	in.mu.Unlock()
	if stepChanged {
		in.log.printf(j.ID, "step: %s", step)
	}
	in.em.Publish("job.progress", map[string]any{"id": j.ID, "percent": percent, "step": step})
}

func (in *Installer) finish(j *job, err error, runtimeID string) {
	in.mu.Lock()
	j.finished = time.Now()
	if err != nil {
		j.State, j.Error = "failed", err.Error()
	} else {
		j.State, j.Percent, j.RuntimeID = "done", 100, runtimeID
	}
	snap := j.Job
	in.mu.Unlock()
	if err != nil {
		in.log.printf(j.ID, "FAILED: %s", snap.Error)
	} else {
		in.log.printf(j.ID, "done: runtime %s", runtimeID)
	}
	in.em.Publish("job.done", map[string]any{"id": snap.ID, "state": snap.State, "error": snap.Error, "runtimeId": snap.RuntimeID})
}

// acquire produces the archive file and its expected and actual digest
// (SHA-256 or SHA-512, whichever the expected hash uses).
type acquire func(ctx context.Context) (archive, want, got string, err error)

func (in *Installer) run(j *job, e model.CatalogEntry, acq acquire) {
	in.log.printf(j.ID, "%s %s (%s) url=%s", j.Type, e.ID, e.Version, e.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	archive, want, got, err := acq(ctx)
	if archive != "" {
		defer os.Remove(archive)
	}
	if err != nil {
		in.finish(j, err, "")
		return
	}
	rid, err := in.process(j, e, archive, want, got)
	in.finish(j, err, rid)
}

func stepErr(step string, err error) error { return fmt.Errorf("%s: %w", step, err) }

func (in *Installer) tmpDir() (string, error) {
	d := filepath.Join(in.home, "tmp")
	return d, os.MkdirAll(d, 0o755)
}

// fetchArchive downloads e.URL into <home>/tmp (0-80 % progress).
func (in *Installer) fetchArchive(ctx context.Context, j *job, e model.CatalogEntry) (string, string, string, error) {
	in.progress(j, 0, "Downloading")
	// Fetch the published checksum first: fail fast, and never extract
	// without something to verify against.
	want, err := in.expectedHash(ctx, j.ID, e)
	if err != nil {
		return "", "", "", stepErr("Downloading", fmt.Errorf("checksum: %w", err))
	}
	tmp, err := in.tmpDir()
	if err != nil {
		return "", "", "", stepErr("Downloading", err)
	}
	if !catalog.IsAllowedURL(e.URL) {
		return "", "", "", stepErr("Downloading", errors.New("download url not allowed"))
	}
	archive := filepath.Join(tmp, j.ID+"-"+e.FileName)
	var got string
	err = in.get(ctx, j.ID, e.URL, func(resp *http.Response, body io.Reader) error {
		f, err := os.Create(archive)
		if err != nil {
			return fatal{err}
		}
		defer f.Close()
		h, algo := hasherFor(want)
		pr := &progressWriter{total: resp.ContentLength, cb: func(p int) { in.progress(j, p*80/100, "Downloading") }}
		n, err := io.Copy(io.MultiWriter(f, h, pr), io.LimitReader(body, maxDownload+1))
		if err != nil {
			return err
		}
		if n > maxDownload {
			return fatal{errors.New("download exceeds size limit")}
		}
		got = hex.EncodeToString(h.Sum(nil))
		in.log.printf(j.ID, "downloaded %d bytes %s=%s", n, algo, got)
		return nil
	})
	if err != nil {
		return archive, "", "", stepErr("Downloading", err)
	}
	return archive, want, got, nil
}

// copyArchive copies the user's file into <home>/tmp (0-80 %), hashing it.
// The original is only read.
func (in *Installer) copyArchive(ctx context.Context, j *job, src, want string) (string, string, string, error) {
	in.progress(j, 0, "Importing")
	tmp, err := in.tmpDir()
	if err != nil {
		return "", "", "", stepErr("Importing", err)
	}
	archive := filepath.Join(tmp, j.ID+"-"+filepath.Base(src))
	in.log.printf(j.ID, "importing %s", src)
	f, err2 := os.Open(src)
	if err2 != nil {
		return "", "", "", stepErr("Importing", err2)
	}
	defer f.Close()
	st, _ := f.Stat()
	out, err2 := os.Create(archive)
	if err2 != nil {
		return "", "", "", stepErr("Importing", err2)
	}
	h, algo := hasherFor(want)
	var total int64
	if st != nil {
		total = st.Size()
	}
	pr := &progressWriter{total: total, cb: func(p int) { in.progress(j, p*80/100, "Importing") }}
	n, err2 := io.Copy(io.MultiWriter(out, h, pr), f)
	if cerr := out.Close(); err2 == nil {
		err2 = cerr
	}
	if err2 != nil {
		return archive, "", "", stepErr("Importing", err2)
	}
	got := hex.EncodeToString(h.Sum(nil))
	in.log.printf(j.ID, "imported %d bytes %s=%s", n, algo, got)
	return archive, want, got, nil
}

// process is the shared tail: verify, extract, move into place, register.
func (in *Installer) process(j *job, e model.CatalogEntry, archive, want, got string) (string, error) {
	in.progress(j, 82, "Verifying")
	_, algo := hasherFor(want)
	in.log.printf(j.ID, "%s expected=%s got=%s", algo, want, got)
	if !strings.EqualFold(got, want) {
		return "", stepErr("Verifying", fmt.Errorf("checksum mismatch: expected %s, file has %s; nothing was extracted", want, got))
	}

	in.progress(j, 85, "Extracting")
	tmpRoot, err := in.tmpDir()
	if err != nil {
		return "", stepErr("Extracting", err)
	}
	stage, err := os.MkdirTemp(tmpRoot, "stage-"+e.ID+"-")
	if err != nil {
		return "", stepErr("Extracting", err)
	}
	defer os.RemoveAll(stage) // no-op after a successful rename
	if strings.HasSuffix(strings.ToLower(archive), ".zip") {
		err = extractZip(archive, stage, e.StripComponents)
	} else {
		err = extractTarGz(archive, stage, e.StripComponents)
	}
	if err != nil {
		return "", stepErr("Extracting", err)
	}
	rel := filepath.FromSlash(e.BinaryRelPath)
	if st, err := os.Stat(filepath.Join(stage, rel)); err != nil || st.IsDir() {
		return "", stepErr("Extracting", fmt.Errorf("archive does not contain %s", e.BinaryRelPath))
	}
	_ = os.Chmod(filepath.Join(stage, rel), 0o755)

	in.progress(j, 95, "Registering")
	runtimesDir := filepath.Join(in.home, "runtimes")
	if err := os.MkdirAll(runtimesDir, 0o755); err != nil {
		return "", stepErr("Registering", err)
	}
	final := filepath.Join(runtimesDir, e.ID)
	if err := os.RemoveAll(final); err != nil {
		return "", stepErr("Registering", err)
	}
	if err := in.moveDir(j.ID, stage, final); err != nil {
		return "", stepErr("Registering", err)
	}
	rt := model.Runtime{
		ID: model.NewID("rt_"), Kind: e.Kind, Version: e.Version, Name: e.Name,
		Binary: filepath.Join(final, rel), Source: "installed", InstalledAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := in.rt.Add(rt); err != nil {
		os.RemoveAll(final)
		return "", stepErr("Registering", err)
	}
	return rt.ID, nil
}

func renameDelay(attempt int) time.Duration {
	d := 250 * time.Millisecond * time.Duration(attempt+1)
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	return d
}

func avHint(err error) string {
	m := err.Error()
	if errors.Is(err, fs.ErrPermission) || strings.Contains(m, "Access is denied") || strings.Contains(m, "being used by another process") {
		return " (antivirus may be scanning the file; try again)"
	}
	return ""
}

// moveDir renames src to dst. On Windows, antivirus scanning a freshly
// extracted .exe makes the rename fail with "Access is denied", so it is
// retried with growing delays, and finally replaced by copy + delete.
func (in *Installer) moveDir(jobID, src, dst string) error {
	var err error
	for i := 0; i < in.RenameAttempts; i++ {
		if err = in.Rename(src, dst); err == nil {
			return nil
		}
		in.log.printf(jobID, "rename attempt %d/%d failed: %v", i+1, in.RenameAttempts, err)
		if i < in.RenameAttempts-1 {
			in.Sleep(renameDelay(i))
		}
	}
	in.log.printf(jobID, "rename kept failing, falling back to copy")
	_ = os.RemoveAll(dst)
	if cerr := copyTree(src, dst); cerr != nil {
		_ = os.RemoveAll(dst)
		return fmt.Errorf("%v; copy fallback failed: %v%s", err, cerr, avHint(err))
	}
	_ = os.RemoveAll(src)
	return nil
}

// hexRE matches a SHA-256 (64 hex) or SHA-512 (128 hex) digest. Caddy, for
// one, publishes SHA-512 checksums; Node and Go publish SHA-256.
var hexRE = regexp.MustCompile(`^([0-9a-fA-F]{64}|[0-9a-fA-F]{128})$`)

// hasherFor picks the digest algorithm that matches the expected hash.
func hasherFor(want string) (hash.Hash, string) {
	if len(want) == 128 {
		return sha512.New(), "sha512"
	}
	return sha256.New(), "sha256"
}

// expectedHash returns the published checksum (SHA-256 or SHA-512) for the entry's archive.
func (in *Installer) expectedHash(ctx context.Context, jobID string, e model.CatalogEntry) (string, error) {
	if e.Checksum.Type == "sha256" {
		return strings.ToLower(e.Checksum.Value), nil
	}
	if !catalog.IsAllowedURL(e.Checksum.URL) {
		return "", errors.New("checksum url not allowed")
	}
	var want string
	err := in.get(ctx, jobID, e.Checksum.URL, func(resp *http.Response, body io.Reader) error {
		b, err := io.ReadAll(io.LimitReader(body, 1<<20))
		if err != nil {
			return err
		}
		want, err = ParseShasums(string(b), catalog.FileName(e.URL))
		if err != nil {
			return fatal{err}
		}
		return nil
	})
	return want, err
}

// ParseShasums accepts either a single bare hash or `<hex>  <filename>` lines
// and returns the lowercase hash for filename.
func ParseShasums(body, filename string) (string, error) {
	body = strings.TrimSpace(body)
	if hexRE.MatchString(body) {
		return strings.ToLower(body), nil
	}
	for _, line := range strings.Split(body, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !hexRE.MatchString(f[0]) {
			continue
		}
		if name := strings.TrimPrefix(f[1], "*"); path.Base(name) == filename {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in the published checksum file", filename)
}

type progressWriter struct {
	total, n int64
	last     int
	cb       func(int)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.n += int64(len(b))
	if p.total > 0 {
		if pc := int(p.n * 100 / p.total); pc != p.last {
			p.last = pc
			p.cb(pc)
		}
	}
	return len(b), nil
}
