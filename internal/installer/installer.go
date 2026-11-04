// Package installer downloads, verifies and extracts catalog runtimes.
package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// Installer runs install jobs.
type Installer struct {
	home    string
	st      *store.Store
	rt      *runtimes.Manager
	em      Emitter
	Catalog func() ([]model.CatalogEntry, error)
	Client  *http.Client

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
		home: home, st: st, rt: rt, em: em,
		Catalog: func() ([]model.CatalogEntry, error) { return catalog.Load(home) },
		Client:  &http.Client{},
		jobs:    map[string]*job{},
	}
}

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

// Start validates the request and runs the install in the background.
func (in *Installer) Start(catalogID string) (model.Job, error) {
	es, err := in.Catalog()
	if err != nil {
		return model.Job{}, apperr.Internal("catalog: %v", err)
	}
	e, ok := catalog.Find(es, catalogID)
	if !ok {
		return model.Job{}, apperr.NotFound("catalog entry %q not found", catalogID)
	}
	if !catalog.Supported(e) {
		return model.Job{}, apperr.BadRequest("%s is for %s/%s, this machine cannot run it", e.ID, e.OS, e.Arch)
	}
	if catalog.Annotate([]model.CatalogEntry{e}, in.home, in.st.Snapshot().Runtimes)[0].Installed {
		return model.Job{}, apperr.Conflict("%s is already installed", e.ID)
	}
	in.mu.Lock()
	for id, j := range in.jobs { // forget old finished jobs
		if !j.finished.IsZero() && time.Since(j.finished) > time.Hour {
			delete(in.jobs, id)
		}
	}
	for _, j := range in.jobs {
		if j.catalogID == e.ID && j.State == "running" {
			in.mu.Unlock()
			return model.Job{}, apperr.Conflict("%s is already being installed (%s)", e.ID, j.ID)
		}
	}
	j := &job{Job: model.Job{ID: model.NewID("job_"), Type: "install", State: "running", Step: "Downloading"}, catalogID: e.ID}
	in.jobs[j.ID] = j
	snap := j.Job
	in.mu.Unlock()
	go in.run(j, e)
	return snap, nil
}

func (in *Installer) progress(j *job, percent int, step string) {
	in.mu.Lock()
	if percent == j.Percent && step == j.Step {
		in.mu.Unlock()
		return
	}
	j.Percent, j.Step = percent, step
	in.mu.Unlock()
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
	in.em.Publish("job.done", map[string]any{"id": snap.ID, "state": snap.State, "error": snap.Error, "runtimeId": snap.RuntimeID})
}

func (in *Installer) run(j *job, e model.CatalogEntry) {
	rid, err := in.install(j, e)
	in.finish(j, err, rid)
}

func (in *Installer) install(j *job, e model.CatalogEntry) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Fetch the published checksum first so we fail fast and never extract
	// without something to verify against.
	in.progress(j, 0, "Downloading")
	want, err := in.expectedHash(ctx, e)
	if err != nil {
		return "", err
	}

	dlDir := filepath.Join(in.home, "downloads")
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		return "", err
	}
	archive := filepath.Join(dlDir, j.ID+"-"+safeBase(e.URL))
	defer os.Remove(archive)
	got, err := in.download(ctx, j, e.URL, archive)
	if err != nil {
		return "", err
	}

	in.progress(j, 82, "Verifying")
	if !strings.EqualFold(got, want) {
		return "", fmt.Errorf("checksum mismatch: expected %s, downloaded file has %s; nothing was extracted", want, got)
	}

	in.progress(j, 85, "Extracting")
	runtimesDir := filepath.Join(in.home, "runtimes")
	if err := os.MkdirAll(runtimesDir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(runtimesDir, ".install-"+e.ID+"-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) // no-op after the rename below
	lower := strings.ToLower(archive)
	if strings.HasSuffix(lower, ".zip") {
		err = extractZip(archive, tmp, e.StripComponents)
	} else {
		err = extractTarGz(archive, tmp, e.StripComponents)
	}
	if err != nil {
		return "", fmt.Errorf("extract: %w", err)
	}
	rel := filepath.FromSlash(e.BinaryRelPath)
	if st, err := os.Stat(filepath.Join(tmp, rel)); err != nil || st.IsDir() {
		return "", fmt.Errorf("archive does not contain %s", e.BinaryRelPath)
	}
	_ = os.Chmod(filepath.Join(tmp, rel), 0o755)

	in.progress(j, 95, "Registering")
	final := filepath.Join(runtimesDir, e.ID)
	if err := os.RemoveAll(final); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		return "", err
	}
	rt := model.Runtime{
		ID: model.NewID("rt_"), Kind: e.Kind, Version: e.Version, Name: e.Name,
		Binary: filepath.Join(final, rel), Source: "installed", InstalledAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := in.rt.Add(rt); err != nil {
		os.RemoveAll(final)
		return "", err
	}
	return rt.ID, nil
}

func safeBase(rawURL string) string {
	u, _ := url.Parse(rawURL)
	b := filepath.Base(path.Base(u.Path))
	if b == "" || b == "." || b == "/" || b == ".." {
		return "download"
	}
	return b
}

var hexRE = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

func (in *Installer) expectedHash(ctx context.Context, e model.CatalogEntry) (string, error) {
	if e.Checksum.Type == "sha256" {
		return strings.ToLower(e.Checksum.Value), nil
	}
	if !catalog.IsAllowedURL(e.Checksum.URL) {
		return "", fmt.Errorf("checksum url not allowed")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, e.Checksum.URL, nil)
	resp, err := in.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch checksum: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch checksum: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return ParseShasums(string(body), safeBase(e.URL))
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

// download saves url to dst, reporting progress (0-80%) and returns the
// sha256 hex digest of the bytes received.
func (in *Installer) download(ctx context.Context, j *job, rawURL, dst string) (string, error) {
	if !catalog.IsAllowedURL(rawURL) {
		return "", fmt.Errorf("download url not allowed")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	resp, err := in.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	pr := &progressWriter{total: resp.ContentLength, cb: func(p int) { in.progress(j, p*80/100, "Downloading") }}
	n, err := io.Copy(io.MultiWriter(f, h, pr), io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if n > maxDownload {
		return "", fmt.Errorf("download exceeds size limit")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
