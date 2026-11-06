package installer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/catalog"
	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
)

// XdebugTarget is the name Xdebug gets in a PHP runtime's ext folder.
const XdebugTarget = "php_xdebug.dll"

// InstallXdebug downloads the Xdebug build matching a PHP runtime from
// xdebug.org, checks it against the SHA-256 the page lists and puts it into
// the runtime's ext folder (a DLL is program code, so it belongs in bin). The
// job's runtimeId is the PHP runtime.
func (in *Installer) InstallXdebug(rt model.Runtime) (model.Job, error) {
	if rt.Kind != "php" {
		return model.Job{}, apperr.BadRequest("%s is not a PHP runtime", rt.ID)
	}
	if runtime.GOOS != "windows" && in.XdebugPage == catalog.XdebugPage {
		return model.Job{}, apperr.BadRequest("Xdebug downloads are Windows builds; install Xdebug with your system's package manager")
	}
	ext := kinds.ExtDir(rt.Binary)
	if st, err := os.Stat(ext); err != nil || !st.IsDir() {
		return model.Job{}, apperr.BadRequest("%s has no ext folder (%s)", rt.Name, ext)
	}
	minor, err := catalog.PHPMinor(rt.Version)
	if err != nil {
		return model.Job{}, apperr.BadRequest("%v", err)
	}
	if strings.HasPrefix(minor, "5.") || strings.HasPrefix(minor, "7.") {
		return model.Job{}, apperr.BadRequest("Xdebug downloads are available for PHP 8.0 and newer; this is PHP %s", rt.Version)
	}
	key := "xdebug-" + rt.ID
	in.mu.Lock()
	for _, j := range in.jobs {
		if j.catalogID == key && j.State == "running" {
			in.mu.Unlock()
			return model.Job{}, apperr.Conflict("Xdebug is already being downloaded for %s (%s)", rt.Name, j.ID)
		}
	}
	j := &job{Job: model.Job{ID: model.NewID("job_"), Type: "xdebug", State: "running", Step: "Downloading"}, catalogID: key, kind: "php"}
	in.jobs[j.ID] = j
	in.mu.Unlock()
	snap := in.snapshot(j)
	go func() {
		err := in.xdebug(j, rt, minor, ext)
		in.finish(j, err, rt.ID)
	}()
	return snap, nil
}

func (in *Installer) xdebug(j *job, rt model.Runtime, minor, ext string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	in.progress(j, 0, "Downloading")
	in.log.printf(j.ID, "xdebug for %s (PHP %s) from %s", rt.ID, rt.Version, in.XdebugPage)
	var page string
	err := in.get(ctx, j.ID, in.XdebugPage, func(_ *http.Response, body io.Reader) error {
		b, err := io.ReadAll(io.LimitReader(body, 4<<20))
		page = string(b)
		return err
	})
	if err != nil {
		return stepErr("Downloading", fmt.Errorf("Xdebug download page: %w", err))
	}
	b, err := catalog.ParseXdebug(page, in.XdebugPage, minor)
	if err != nil {
		return stepErr("Downloading", err)
	}
	in.log.printf(j.ID, "found %s sha256=%s", b.URL, b.SHA256)
	tmp, err := in.tmpDir()
	if err != nil {
		return stepErr("Downloading", err)
	}
	part := filepath.Join(tmp, j.ID+"-"+b.FileName)
	defer os.Remove(part)
	var got string
	err = in.get(ctx, j.ID, b.URL, func(resp *http.Response, body io.Reader) error {
		f, err := os.Create(part)
		if err != nil {
			return fatal{err}
		}
		defer f.Close()
		h := newDigests(b.SHA256)
		pr := &progressWriter{total: resp.ContentLength, cb: func(p int) { in.progress(j, p*80/100, "Downloading") }}
		n, err := io.Copy(io.MultiWriter(f, h, pr), io.LimitReader(body, 64<<20+1))
		if err != nil {
			return err
		}
		if n > 64<<20 {
			return fatal{errors.New("download exceeds size limit")}
		}
		got = h.sum()
		in.log.printf(j.ID, "downloaded %d bytes sha256=%s", n, got)
		return nil
	})
	if err != nil {
		return stepErr("Downloading", err)
	}
	in.progress(j, 85, "Verifying")
	if !strings.EqualFold(got, b.SHA256) {
		return stepErr("Verifying", fmt.Errorf("checksum mismatch: xdebug.org lists %s, the file has %s; nothing was installed", b.SHA256, got))
	}
	in.progress(j, 95, "Registering")
	dst := filepath.Join(ext, XdebugTarget)
	var rerr error
	for i := 0; i < in.RenameAttempts; i++ { // antivirus may hold the new DLL for a moment
		_ = os.Remove(dst)
		if rerr = in.Rename(part, dst); rerr == nil {
			break
		}
		in.Sleep(renameDelay(i))
	}
	if rerr != nil {
		return stepErr("Registering", fmt.Errorf("%v%s", rerr, avHint(rerr)))
	}
	in.log.printf(j.ID, "Xdebug %s installed as %s", b.Version, dst)
	return nil
}
