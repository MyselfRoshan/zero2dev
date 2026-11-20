package dbtools

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"zero2dev/internal/apperr"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
)

// countReader counts the bytes read through it and reports them.
type countReader struct {
	r      io.Reader
	n      int64
	report func(n int64)
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if n > 0 && c.report != nil {
		c.report(c.n)
	}
	return n, err
}

// countWriter counts the bytes written through it.
type countWriter struct {
	w      io.Writer
	n      atomic.Int64
	report func(n int64)
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	total := c.n.Add(int64(n))
	if c.report != nil {
		c.report(total)
	}
	return n, err
}

// gzipMagic are the first two bytes of every gzip file.
var gzipMagic = []byte{0x1f, 0x8b}

// openDump opens a dump for reading: plain SQL, or gzip when it starts with
// 1f 8b, whatever it is called. read reports the bytes read from the file.
func openDump(path string, read func(n int64)) (io.Reader, io.Closer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	br := bufio.NewReaderSize(&countReader{r: f, report: read}, 256*1024)
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == gzipMagic[0] && magic[1] == gzipMagic[1] {
		gz, err := gzip.NewReader(br)
		if err != nil {
			f.Close()
			return nil, nil, fmt.Errorf("%s looks like a gzip file but cannot be read: %v", path, err)
		}
		return gz, f, nil
	}
	return br, f, nil
}

// startJob runs fn as a job of typ on the service, holding it for Reset.
func (m *Manager) startJob(typ, step string, svc model.Service, fn func(h *jobs.Handle) error) (model.Job, error) {
	if m.Jobs == nil {
		return model.Job{}, ErrNotImplemented
	}
	h, err := m.Jobs.Start(typ, "db:"+svc.ID, step, func(j *model.Job) { j.ServiceID = svc.ID })
	if err != nil {
		return model.Job{}, err
	}
	m.hold(svc.ID, h.ID())
	j := h.Job()
	go func() {
		defer m.unhold(svc.ID)
		defer func() {
			if p := recover(); p != nil {
				h.Fail(apperr.Internal("job crashed: %v", p))
			}
		}()
		if err := fn(h); err != nil {
			h.Fail(err)
			return
		}
		h.Done()
	}()
	return j, nil
}

// Import starts a db-import job: filePath (.sql, or gzip-compressed, detected
// by its first bytes) is read into database, which is created first when
// create is set.
func (m *Manager) Import(serviceID, database, filePath string, create bool) (model.Job, error) {
	svc, err := m.sqlService(serviceID, "dumps to import")
	if err != nil {
		return model.Job{}, err
	}
	if err := checkDatabase(database); err != nil {
		return model.Job{}, err
	}
	path, err := checkAbs(filePath)
	if err != nil {
		return model.Job{}, err
	}
	st, err := os.Stat(path)
	switch {
	case err != nil:
		return model.Job{}, apperr.BadRequest("%s does not exist", path)
	case st.IsDir():
		return model.Job{}, apperr.BadRequest("%s is a folder, not a dump file", path)
	}
	if err := m.needRunning(svc); err != nil {
		return model.Job{}, err
	}
	t, err := m.local(svc)
	if err != nil {
		return model.Job{}, err
	}
	bin, err := m.program(t, false, "")
	if err != nil {
		return model.Job{}, err
	}
	ctx, cancel := bg()
	dbs, err := m.listDatabases(ctx, t)
	cancel()
	if err != nil {
		return model.Job{}, err
	}
	exists := hasName(t.Kind, databaseNames(dbs), database)
	if !exists && !create {
		return model.Job{}, apperr.BadRequest("database %s does not exist; create it first", database)
	}
	size := st.Size()
	return m.startJob(model.JobDBImport, "Importing", svc, func(h *jobs.Handle) error {
		ctx := context.Background()
		if !exists {
			if err := m.createDB(ctx, t, svc.ID, database); err != nil {
				return err
			}
		}
		in, closer, err := openDump(path, func(n int64) {
			if size > 0 {
				h.Progress(int(n*100/size), "Importing")
			}
		})
		if err != nil {
			m.log(svc.ID, "import failed: "+err.Error())
			return apperr.BadRequest("%v", err)
		}
		defer closer.Close()
		err = m.exec(ctx, svc.ID, "import "+path+" into "+database, Cmd{
			Bin: bin, Args: importArgs(t, database), Env: passwordEnv(t), Stdin: in, Stdout: io.Discard,
		})
		if err != nil {
			return fmt.Errorf("importing into %s failed: %v", database, err)
		}
		m.log(svc.ID, "imported "+path+" into "+database)
		return nil
	})
}

// Export starts a db-export job writing database to filePath (gzip when it
// ends in .gz); an existing file is refused unless overwrite.
func (m *Manager) Export(serviceID, database, filePath string, overwrite bool) (model.Job, error) {
	svc, err := m.sqlService(serviceID, "dumps to export")
	if err != nil {
		return model.Job{}, err
	}
	if err := checkDatabase(database); err != nil {
		return model.Job{}, err
	}
	path, err := checkAbs(filePath)
	if err != nil {
		return model.Job{}, err
	}
	if st, err := os.Stat(filepath.Dir(path)); err != nil || !st.IsDir() {
		return model.Job{}, apperr.BadRequest("folder %s does not exist", filepath.Dir(path))
	}
	if st, err := os.Stat(path); err == nil {
		if st.IsDir() {
			return model.Job{}, apperr.BadRequest("%s is a folder", path)
		}
		if !overwrite {
			return model.Job{}, apperr.Conflict("%s exists", path)
		}
	}
	if err := m.needRunning(svc); err != nil {
		return model.Job{}, err
	}
	t, err := m.local(svc)
	if err != nil {
		return model.Job{}, err
	}
	bin, err := m.program(t, true, "")
	if err != nil {
		return model.Job{}, err
	}
	ctx, cancel := bg()
	dbs, err := m.listDatabases(ctx, t)
	cancel()
	if err != nil {
		return model.Job{}, err
	}
	var size int64
	found := false
	for _, d := range dbs {
		if d.Name == database || family(t.Kind) == "mysql" && strings.EqualFold(d.Name, database) {
			found, size = true, d.SizeBytes
		}
	}
	if !found {
		return model.Job{}, apperr.BadRequest("database %s does not exist", database)
	}
	return m.startJob(model.JobDBExport, "Exporting", svc, func(h *jobs.Handle) error {
		err := writeAtomic(path, func(w io.Writer) error {
			cw := &countWriter{w: w, report: func(n int64) {
				if size > 0 {
					h.Progress(int(min(n*100/size, 95)), "Exporting")
				}
			}}
			return m.exec(context.Background(), svc.ID, "export "+database+" to "+path, Cmd{
				Bin: bin, Args: dumpArgs(t, database), Env: passwordEnv(t), Stdout: cw,
			})
		})
		if err != nil {
			return fmt.Errorf("exporting %s failed: %v", database, err)
		}
		h.Set(func(j *model.Job) { j.Path = path })
		m.log(svc.ID, "exported "+database+" to "+path)
		return nil
	})
}

// writeAtomic writes path through path+".tmp" (gzip-compressed when path ends
// in .gz) and renames it at the end; on failure the temporary file is removed
// and path is left as it was.
func writeAtomic(path string, fill func(w io.Writer) error) (err error) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	bw := bufio.NewWriterSize(f, 256*1024)
	var w io.Writer = bw
	var gz *gzip.Writer
	if strings.EqualFold(filepath.Ext(path), ".gz") {
		gz = gzip.NewWriter(bw)
		w = gz
	}
	if err = fill(w); err != nil {
		return err
	}
	if gz != nil {
		if err = gz.Close(); err != nil {
			return err
		}
	}
	if err = bw.Flush(); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
