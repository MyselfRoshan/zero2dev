package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/dbtools"
	"zero2dev/internal/fsutil"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// Start starts a migrate job.
func (m *Manager) Start(req Request) (model.Job, error) {
	if m.Jobs == nil {
		return model.Job{}, ErrNotImplemented
	}
	if req.Mode != ModeCopy && req.Mode != ModeMove {
		return model.Job{}, apperr.BadRequest("mode must be copy or move")
	}
	p := m.planFor(strings.TrimSpace(req.Source))
	if p == nil {
		return model.Job{}, apperr.NotFound("unknown source %q", req.Source)
	}
	if !slices.Contains(p.src.Modes, req.Mode) {
		return model.Job{}, apperr.BadRequest("%s can only be copied: %s", p.src.Label, p.src.CopyOnly)
	}
	if len(p.src.Blockers) > 0 {
		return model.Job{}, apperr.Conflict("%s", p.src.Blockers[0])
	}
	items, err := pickItems(p, req.Items)
	if err != nil {
		return model.Job{}, err
	}
	rec := &record{Migration: Migration{ID: model.NewID("mig_"), Source: p.src.ID, Program: p.src.Program, Label: p.src.Label,
		Mode: req.Mode, CreatedAt: m.now(), State: StateRunning, RevertHow: []string{}}, Portable: p.portable, Image: p.image}
	var remote *Database
	switch p.src.Program {
	case ProgDatabase:
		db, err := m.prepareDatabase(req.Database)
		if err != nil {
			return model.Job{}, err
		}
		remote = db
		svc, _ := m.service(db.TargetServiceID)
		rec.Remote = &remoteServer{Kind: db.Kind, Host: db.Host, Port: db.Port, User: db.User}
		rec.Databases, rec.ServiceID = db.Databases, db.TargetServiceID
		items[0].From = fmt.Sprintf("%s://%s:%d", db.Kind, db.Host, db.Port)
		items[0].To = svc.Name
		items[0].Exists = true
	case ProgRuntime:
		rt := p.runtime
		rec.RuntimeID = rt.rt.ID
		rec.Runtime = &adoption{RuntimeID: rt.rt.ID, Root: rt.root, OriginalBinary: rt.rt.Binary, Dir: items[0].To}
	}
	for _, it := range items {
		rec.Items = append(rec.Items, it.Item)
		rec.Steps = append(rec.Steps, step{Label: it.Label, Kind: it.Kind, From: it.From, To: it.To, SizeBytes: it.SizeBytes,
			RegPath: it.regPath, Rewrite: it.rewrite, Skip: it.skip})
	}
	h, err := m.Jobs.Start(model.JobMigrate, "migrate:"+p.src.ID, "Preparing", func(j *model.Job) { j.MigrationID = rec.ID })
	if err != nil {
		return model.Job{}, err
	}
	if err := m.save(rec); err != nil {
		h.Fail(err)
		return model.Job{}, apperr.Internal("cannot write the migration record: %v", err)
	}
	m.setActive(rec.ID, true)
	job := h.Job()
	m.goJob(h, rec, func() error { return m.bringIn(h, rec, remote) })
	return job, nil
}

// goJob runs fn for the job of rec and finishes both.
func (m *Manager) goJob(h *jobs.Handle, rec *record, fn func() error) {
	go func() {
		var err error
		defer func() {
			if p := recover(); p != nil {
				err = apperr.Internal("migration crashed: %v", p)
				rec.Error = err.Error()
				_ = m.save(rec)
			}
			t := m.now()
			rec.FinishedAt = &t
			_ = m.save(rec)
			m.setActive(rec.ID, false)
			m.emit("migrations.changed")
			if err != nil {
				h.Fail(err)
			} else {
				h.Done()
			}
		}()
		err = fn()
	}()
}

// pickItems keeps the items named in labels (all when empty) that exist.
func pickItems(p *plan, labels []string) ([]planItem, error) {
	var out []planItem
	if len(labels) == 0 {
		out = p.items
	} else {
		for _, l := range labels {
			i := slices.IndexFunc(p.items, func(it planItem) bool { return strings.EqualFold(it.Label, strings.TrimSpace(l)) })
			if i < 0 {
				names := []string{}
				for _, it := range p.items {
					names = append(names, it.Label)
				}
				return nil, apperr.BadRequest("%s has no item %q (it has %s)", p.src.Label, l, strings.Join(names, ", "))
			}
			if !slices.ContainsFunc(out, func(it planItem) bool { return it.Label == p.items[i].Label }) {
				out = append(out, p.items[i])
			}
		}
	}
	if p.src.Program == ProgDatabase {
		return []planItem{p.items[0]}, nil
	}
	var found []planItem
	for _, it := range out {
		if it.Exists {
			found = append(found, it)
		}
	}
	if len(found) == 0 {
		return nil, apperr.Conflict("nothing of %s was found on this PC", p.src.Label)
	}
	return found, nil
}

func revertOptions(rec *record) []string {
	switch {
	case rec.Program == ProgRuntime || rec.Program == ProgDatabase:
		return []string{RevertDiscard}
	case rec.Mode == ModeMove:
		return []string{RevertMoveBack}
	default:
		return []string{RevertDiscard, RevertCopyBack}
	}
}

// undoHow is how a failed migration is rolled back.
func undoHow(rec *record) string {
	if rec.Mode == ModeMove {
		return RevertMoveBack
	}
	return RevertDiscard
}

// progress turns bytes into the job's percent.
type progress struct {
	h           *jobs.Handle
	total, done int64
}

func (p *progress) add(step string) func(int64) {
	return func(n int64) {
		p.done += n
		pct := 0
		if p.total > 0 {
			pct = int(p.done * 100 / p.total)
		}
		p.h.Progress(pct, step)
	}
}

func (m *Manager) bringIn(h *jobs.Handle, rec *record, remote *Database) error {
	pr := &progress{h: h}
	for _, s := range rec.Steps {
		pr.total += s.SizeBytes
	}
	verb := "Copying"
	if rec.Mode == ModeMove {
		verb = "Moving"
	}
	var err error
	for i := range rec.Steps {
		s := &rec.Steps[i]
		label := verb + " " + s.Label
		h.Progress(pctOf(pr), label)
		switch s.Kind {
		case KindDir, KindFile:
			err = m.bringPath(rec, i, pr.add(label))
		case KindRegistry:
			err = m.bringRegistry(rec, i, pr.add(label))
		case KindRuntime:
			err = m.adopt(rec, i, pr.add("Copying "+rec.Label))
		case KindDatabases:
			err = m.copyDatabases(h, rec, i, remote)
		}
		if serr := m.save(rec); err == nil && serr != nil {
			err = serr
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		h.Progress(pctOf(pr), "Putting things back")
		rerr := m.undoAll(nil, rec, undoHow(rec))
		rec.State, rec.Error = StateFailed, err.Error()
		if rerr != nil {
			rec.RevertHow = revertOptions(rec) // let the user finish the clean-up
			err = fmt.Errorf("%v; putting things back failed too: %v", err, rerr)
			rec.Error = err.Error()
		} else {
			rec.RevertHow = []string{}
			if rec.Portable != "" {
				_ = m.portableFlag(rec.Portable, false)
			}
		}
		_ = m.save(rec)
		return err
	}
	if err := m.portableFlag(rec.Portable, true); err != nil {
		return err
	}
	rec.State, rec.RevertHow = StateDone, revertOptions(rec)
	rec.Description = describe(rec)
	return m.save(rec)
}

func pctOf(p *progress) int {
	if p.total <= 0 {
		return 0
	}
	return int(p.done * 100 / p.total)
}

func describe(rec *record) string {
	switch rec.Program {
	case ProgRuntime:
		if rec.Runtime != nil {
			return "Adopted " + rec.Label + " into " + rec.Runtime.Dir
		}
	case ProgDatabase:
		n := len(rec.Databases)
		word := "databases"
		if n == 1 {
			word = "database"
		}
		from := ""
		if rec.Remote != nil {
			from = fmt.Sprintf(" from %s:%d", rec.Remote.Host, rec.Remote.Port)
		}
		to := ""
		if len(rec.Items) > 0 && rec.Items[0].To != "" {
			to = " into " + rec.Items[0].To
		}
		return fmt.Sprintf("Copied %d %s (%s)%s%s", n, word, strings.Join(rec.Databases, ", "), from, to)
	}
	verb := "Copied"
	if rec.Mode == ModeMove {
		verb = "Moved"
	}
	var labels []string
	var size int64
	for _, s := range rec.Steps {
		labels = append(labels, s.Label)
		size += s.SizeBytes
	}
	return fmt.Sprintf("%s %s: %s (%s)", verb, rec.Label, strings.Join(labels, ", "), humanSize(size))
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// saveTarget moves whatever the portable place holds into backup-target\.
func (m *Manager) saveTarget(rec *record, i int) error {
	s := &rec.Steps[i]
	if !exists(s.To) {
		return nil
	}
	abs, rel := m.backupPath(rec, "backup-target", i, s.Label, filepath.Base(s.To))
	if err := m.movePath(s.To, abs, nil); err != nil {
		return fmt.Errorf("cannot back up %s: %w", s.To, err)
	}
	s.TargetBackup = rel
	return m.save(rec)
}

// restoreTarget empties the portable place and puts its backup back.
func (m *Manager) restoreTarget(rec *record, i int) error {
	s := &rec.Steps[i]
	if err := os.RemoveAll(s.To); err != nil {
		return fmt.Errorf("cannot remove %s: %w", s.To, err)
	}
	if s.TargetBackup != "" {
		if err := m.movePath(m.abs(rec, s.TargetBackup), s.To, nil); err != nil {
			return fmt.Errorf("cannot put back what %s held: %w", s.To, err)
		}
		_ = os.Remove(filepath.Dir(m.abs(rec, s.TargetBackup)))
		s.TargetBackup = ""
	}
	return nil
}

// saveOriginal moves whatever is at the original place into backup-original\.
func (m *Manager) saveOriginal(rec *record, i int) error {
	s := &rec.Steps[i]
	if !exists(s.From) {
		return nil
	}
	abs, rel := m.backupPath(rec, "backup-original", i, s.Label, filepath.Base(s.From))
	if err := m.movePath(s.From, abs, nil); err != nil {
		return fmt.Errorf("cannot back up %s: %w", s.From, err)
	}
	s.RevertBackups = append(s.RevertBackups, rel)
	return m.save(rec)
}

func (m *Manager) bringPath(rec *record, i int, prog func(int64)) error {
	s := &rec.Steps[i]
	if err := m.saveTarget(rec, i); err != nil {
		return err
	}
	s.Started = true
	if err := m.save(rec); err != nil {
		return err
	}
	if rec.Mode == ModeMove {
		if err := m.movePath(s.From, s.To, prog); err != nil {
			return err
		}
		s.Moved = true
	} else {
		if err := copyPath(s.From, s.To, nil, prog); err != nil {
			return fmt.Errorf("cannot copy %s: %w", s.From, err)
		}
		if err := verify(s.From, s.To, nil); err != nil {
			return err
		}
	}
	if s.Rewrite {
		if err := rewriteExtensions(s.To, s.From, s.To); err != nil {
			return err
		}
	}
	s.Done = true
	return nil
}

func (m *Manager) bringRegistry(rec *record, i int, prog func(int64)) error {
	s := &rec.Steps[i]
	reg := m.reg()
	vals, ok, err := reg.ReadTree(s.RegPath)
	if err != nil {
		return fmt.Errorf("cannot read HKCU\\%s: %w", s.RegPath, err)
	}
	if !ok {
		return apperr.Conflict("HKCU\\%s does not exist any more", s.RegPath)
	}
	if err := m.saveTarget(rec, i); err != nil {
		return err
	}
	s.Started = true
	data, _ := encodePortable(vals)
	if err := fsutil.WriteFileAtomic(s.To, data, 0o644); err != nil {
		return err
	}
	if err := checkPortable(s.To, vals); err != nil {
		return err
	}
	if rec.Mode == ModeMove {
		rel, err := m.saveKey(rec, i, "backup-original", vals)
		if err != nil {
			return err
		}
		s.OriginalBackup = rel
		if err := m.save(rec); err != nil {
			return err
		}
		if err := reg.DeleteTree(s.RegPath); err != nil {
			return fmt.Errorf("cannot remove HKCU\\%s: %w", s.RegPath, err)
		}
		s.Moved = true
	}
	prog(int64(len(data)))
	s.Done = true
	return nil
}

// checkPortable reads the written file back and compares it with vals.
func checkPortable(file string, vals []RegValue) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	got, err := decodePortable(raw)
	if err != nil {
		return err
	}
	var want []RegValue
	for _, v := range vals {
		if exportable(v) {
			if v.Type == regExpandSZ {
				v.Type = regSZ
			}
			if v.Type == regDWORD {
				v.Data = v.Data[:4]
			}
			v.Key = strings.Trim(v.Key, `\`)
			want = append(want, v)
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("%s holds %d values instead of %d", file, len(got), len(want))
	}
	for i := range got {
		g, w := got[i], want[i]
		same := g.Key == w.Key && g.Name == w.Name && g.Type == w.Type
		if same && g.Type == regSZ {
			same = utf16String(g.Data) == utf16String(w.Data)
		} else if same {
			same = string(g.Data) == string(w.Data)
		}
		if !same {
			return fmt.Errorf("%s does not match the registry at %s\\%s", file, got[i].Key, got[i].Name)
		}
	}
	return nil
}

// saveKey writes vals as registry.json (everything, for Zero2Dev) and
// HeidiSQL.reg (to restore by double-click) in a new backup folder.
func (m *Manager) saveKey(rec *record, i int, kind string, vals []RegValue) (string, error) {
	s := rec.Steps[i]
	abs, rel := m.backupPath(rec, kind, i, s.Label, "registry.json")
	raw, err := json.MarshalIndent(vals, "", "  ")
	if err != nil {
		return "", err
	}
	if err := fsutil.WriteFileAtomic(abs, raw, 0o644); err != nil {
		return "", err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(filepath.Dir(abs), "HeidiSQL.reg"), regFile(s.RegPath, vals), 0o644); err != nil {
		return "", err
	}
	return rel, nil
}

func (m *Manager) readKeyBackup(rec *record, rel string) ([]RegValue, error) {
	raw, err := os.ReadFile(m.abs(rec, rel))
	if err != nil {
		return nil, err
	}
	var vals []RegValue
	return vals, json.Unmarshal(raw, &vals)
}

// Revert starts a migrate-revert job; how is one of the record's RevertHow.
func (m *Manager) Revert(id, how string) (model.Job, error) {
	if m.Jobs == nil {
		return model.Job{}, ErrNotImplemented
	}
	rec, err := m.load(id)
	if err != nil {
		return model.Job{}, err
	}
	if m.isActive(id) {
		return model.Job{}, apperr.Conflict("migration %s is still running", id)
	}
	if rec.State == StateReverted {
		return model.Job{}, apperr.Conflict("migration %s was already reverted", id)
	}
	if len(rec.RevertHow) == 0 {
		return model.Job{}, apperr.Conflict("migration %s has nothing to revert", id)
	}
	if !slices.Contains(rec.RevertHow, how) {
		return model.Job{}, apperr.BadRequest("how must be %s", strings.Join(rec.RevertHow, " or "))
	}
	if m.running(rec.Image) {
		return model.Job{}, apperr.Conflict("Close %s first", rec.Label)
	}
	if a := rec.Runtime; a != nil && a.Binary != "" && m.adoptedRuntime(a) != nil {
		if st, err := os.Stat(a.OriginalBinary); err != nil || st.IsDir() {
			return model.Job{}, apperr.Conflict("the original program %s is gone; keep the adopted copy", a.OriginalBinary)
		}
	}
	if rec.Program == ProgDatabase && m.DB == nil {
		return model.Job{}, ErrNotImplemented
	}
	h, err := m.Jobs.Start(model.JobMigrateRevert, "migrate:"+rec.Source, "Reverting", func(j *model.Job) {
		j.MigrationID = rec.ID
		if rec.Program == ProgDatabase {
			j.ServiceID = rec.ServiceID
		}
		if rec.Program == ProgRuntime {
			j.RuntimeID = rec.RuntimeID
		}
	})
	if err != nil {
		return model.Job{}, err
	}
	m.setActive(rec.ID, true)
	job := h.Job()
	m.goJob(h, rec, func() error {
		if err := m.undoAll(h, rec, how); err != nil {
			_ = m.save(rec)
			return err
		}
		t := m.now()
		rec.State, rec.RevertedAt, rec.RevertHow = StateReverted, &t, []string{}
		if err := m.portableFlag(rec.Portable, false); err != nil {
			return err
		}
		return m.save(rec)
	})
	return job, nil
}

// undoAll reverts the steps in reverse order. h (may be nil) gets progress.
func (m *Manager) undoAll(h *jobs.Handle, rec *record, how string) error {
	n := len(rec.Steps)
	for k := n - 1; k >= 0; k-- {
		s := &rec.Steps[k]
		if h != nil {
			h.Progress((n-1-k)*100/max(n, 1), "Putting back "+s.Label)
		}
		var err error
		switch s.Kind {
		case KindDir, KindFile:
			err = m.undoPath(rec, k, how)
		case KindRegistry:
			err = m.undoRegistry(rec, k, how)
		case KindRuntime:
			err = m.unadopt(rec, k)
		case KindDatabases:
			err = m.dropDatabases(rec, k)
		}
		if serr := m.save(rec); err == nil && serr != nil {
			err = serr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) undoPath(rec *record, i int, how string) error {
	s := &rec.Steps[i]
	switch how {
	case RevertCopyBack:
		if !s.Done {
			return nil
		}
		if !exists(s.To) {
			return apperr.Conflict("%s is gone; there is nothing to copy back", s.To)
		}
		if err := m.saveOriginal(rec, i); err != nil {
			return err
		}
		if err := copyPath(s.To, s.From, nil, nil); err != nil {
			return fmt.Errorf("cannot copy %s back: %w", s.To, err)
		}
		if err := verify(s.To, s.From, nil); err != nil {
			return err
		}
		if s.Rewrite {
			return rewriteExtensions(s.From, s.To, s.From)
		}
		return nil
	case RevertMoveBack:
		if s.Moved {
			if !exists(s.To) {
				return apperr.Conflict("%s is gone; there is nothing to move back", s.To)
			}
			if err := m.saveOriginal(rec, i); err != nil {
				return err
			}
			if err := m.movePath(s.To, s.From, nil); err != nil {
				return err
			}
			s.Moved = false
			if s.Rewrite {
				if err := rewriteExtensions(s.From, s.To, s.From); err != nil {
					return err
				}
			}
		}
	}
	// discard, and the rest of move-back: the portable place as it was.
	if s.Started || s.TargetBackup != "" {
		if err := m.restoreTarget(rec, i); err != nil {
			return err
		}
	}
	s.Started, s.Done = false, false
	return nil
}

func (m *Manager) undoRegistry(rec *record, i int, how string) error {
	s := &rec.Steps[i]
	reg := m.reg()
	// saveCurrent backs up what the key holds now, before it is written.
	saveCurrent := func() error {
		cur, ok, err := reg.ReadTree(s.RegPath)
		if err != nil {
			return err
		}
		if ok {
			rel, err := m.saveKey(rec, i, "backup-original", cur)
			if err != nil {
				return err
			}
			s.RevertBackups = append(s.RevertBackups, rel)
		}
		return nil
	}
	portable := func() ([]RegValue, error) {
		raw, err := os.ReadFile(s.To)
		if err != nil {
			return nil, apperr.Conflict("%s is gone; there is nothing to put back", s.To)
		}
		return decodePortable(raw)
	}
	switch how {
	case RevertCopyBack:
		if !s.Done {
			return nil
		}
		vals, err := portable()
		if err != nil {
			return err
		}
		if err := saveCurrent(); err != nil {
			return err
		}
		return reg.WriteValues(s.RegPath, vals)
	case RevertMoveBack:
		if s.Moved {
			vals, err := portable()
			if err != nil {
				return err
			}
			// Values HeidiSQL's file cannot hold come from the backup of the key.
			if s.OriginalBackup != "" {
				if old, err := m.readKeyBackup(rec, s.OriginalBackup); err == nil {
					for _, v := range old {
						if !exportable(v) {
							vals = append(vals, v)
						}
					}
				}
			}
			if err := saveCurrent(); err != nil {
				return err
			}
			if err := reg.WriteValues(s.RegPath, vals); err != nil {
				return err
			}
			s.Moved = false
		}
	}
	if s.Started || s.TargetBackup != "" {
		if err := m.restoreTarget(rec, i); err != nil {
			return err
		}
	}
	s.Started, s.Done = false, false
	return nil
}

// ---------- runtimes ----------

func (m *Manager) adopt(rec *record, i int, prog func(int64)) error {
	s := &rec.Steps[i]
	a := rec.Runtime
	if a == nil {
		return apperr.Internal("runtime migration without a runtime")
	}
	if exists(s.To) {
		return apperr.Conflict("%s already exists", s.To)
	}
	bin := a.OriginalBinary
	if r, err := filepath.EvalSymlinks(bin); err == nil {
		bin = r
	}
	rel, err := filepath.Rel(a.Root, bin)
	if err != nil || strings.HasPrefix(rel, "..") {
		return apperr.Conflict("%s is not inside %s", bin, a.Root)
	}
	s.Started = true
	if err := m.save(rec); err != nil {
		return err
	}
	if err := copyPath(a.Root, s.To, s.Skip, prog); err != nil {
		return fmt.Errorf("cannot copy %s: %w", a.Root, err)
	}
	if err := verify(a.Root, s.To, s.Skip); err != nil {
		return err
	}
	newBin := filepath.Join(s.To, rel)
	if st, err := os.Stat(newBin); err != nil || st.IsDir() {
		return apperr.Internal("the copy has no %s", newBin)
	}
	err = m.St.Update(func(d *store.Data) error {
		for k := range d.Runtimes {
			r := &d.Runtimes[k]
			if r.ID != a.RuntimeID {
				continue
			}
			if r.Source != "registered" || r.Binary != a.OriginalBinary {
				return apperr.Conflict("%s changed while it was being copied", rec.Label)
			}
			r.Binary, r.Source = newBin, "installed"
			return nil
		}
		return apperr.Conflict("runtime %s was deleted while it was being copied", a.RuntimeID)
	})
	if err != nil {
		return err
	}
	a.Binary = newBin
	s.Done = true
	m.emit("runtimes.changed")
	return nil
}

// adoptedRuntime is the runtime as it is now, when it still runs from the copy.
func (m *Manager) adoptedRuntime(a *adoption) *model.Runtime {
	if m.St == nil {
		return nil
	}
	for _, r := range m.St.Snapshot().Runtimes {
		if r.ID == a.RuntimeID && strings.EqualFold(r.Binary, a.Binary) {
			return &r
		}
	}
	return nil
}

func (m *Manager) unadopt(rec *record, i int) error {
	s := &rec.Steps[i]
	a := rec.Runtime
	if a == nil {
		return nil
	}
	if a.Binary != "" {
		err := m.St.Update(func(d *store.Data) error {
			for k := range d.Runtimes {
				r := &d.Runtimes[k]
				if r.ID == a.RuntimeID && strings.EqualFold(r.Binary, a.Binary) {
					if st, err := os.Stat(a.OriginalBinary); err != nil || st.IsDir() {
						return apperr.Conflict("the original program %s is gone; keep the adopted copy", a.OriginalBinary)
					}
					r.Binary, r.Source = a.OriginalBinary, "registered"
				}
			}
			return nil // a deleted runtime has nothing to put back
		})
		if err != nil {
			return err
		}
		a.Binary = ""
		m.emit("runtimes.changed")
	}
	if s.Started && inside(filepath.Join(m.Bin, "adopted"), s.To) {
		if err := os.RemoveAll(s.To); err != nil {
			return fmt.Errorf("%s runs from its own folder again, but the copy %s could not be removed (is it still running?): %w", rec.Label, s.To, err)
		}
	}
	s.Started, s.Done = false, false
	return nil
}

// ---------- databases ----------

var dbNameOK = func(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

var systemDatabases = []string{"mysql", "information_schema", "performance_schema", "sys", "postgres", "template0", "template1"}

func validateServer(src Database) error {
	switch src.Kind {
	case "mariadb", "mysql", "postgres":
	default:
		return apperr.BadRequest("kind must be mariadb, mysql or postgres")
	}
	h := strings.TrimSpace(src.Host)
	if h == "" || len(h) > 255 || strings.ContainsAny(h, " \t\r\n/\\") {
		return apperr.BadRequest("host is required (a name or an IP address)")
	}
	if src.Port < 1 || src.Port > 65535 {
		return apperr.BadRequest("port must be 1-65535")
	}
	if strings.TrimSpace(src.User) == "" || strings.ContainsAny(src.User, "\r\n") {
		return apperr.BadRequest("user is required")
	}
	if strings.ContainsAny(src.Password, "\r\n\x00") {
		return apperr.BadRequest("password must not contain line breaks")
	}
	return nil
}

func compatible(srcKind, svcKind string) bool {
	if srcKind == "postgres" {
		return svcKind == "postgres"
	}
	return svcKind == "mariadb" || svcKind == "mysql"
}

func (m *Manager) service(id string) (model.Service, bool) {
	if m.St == nil {
		return model.Service{}, false
	}
	for _, s := range m.St.Snapshot().Services {
		if s.ID == id {
			return s, true
		}
	}
	return model.Service{}, false
}

// clientDir is the folder of the client programs: the runtime of the service
// serviceID, or else a runtime of a kind that speaks to a kind server.
func (m *Manager) clientDir(kind, serviceID string) (string, error) {
	var d store.Data
	if m.St != nil {
		d = m.St.Snapshot()
	}
	pick := func(k string) string {
		first := ""
		for _, r := range d.Runtimes {
			if r.Kind != k {
				continue
			}
			if r.Default {
				return filepath.Dir(r.Binary)
			}
			if first == "" {
				first = filepath.Dir(r.Binary)
			}
		}
		return first
	}
	if serviceID != "" {
		if svc, ok := m.service(serviceID); ok {
			if svc.RuntimeID != "" {
				for _, r := range d.Runtimes {
					if r.ID == svc.RuntimeID {
						return filepath.Dir(r.Binary), nil
					}
				}
			}
			if dir := pick(svc.Kind); dir != "" {
				return dir, nil
			}
		}
	}
	kinds := []string{kind}
	switch kind {
	case "mariadb":
		kinds = append(kinds, "mysql")
	case "mysql":
		kinds = append(kinds, "mariadb")
	}
	for _, k := range kinds {
		if dir := pick(k); dir != "" {
			return dir, nil
		}
	}
	if kind == "postgres" {
		return "", apperr.Conflict("Install PostgreSQL in Zero2Dev first: its client programs are needed")
	}
	return "", apperr.Conflict("Install MariaDB or MySQL in Zero2Dev first: its client programs are needed")
}

func adminUser(kind string) string {
	if kind == "postgres" {
		return "postgres"
	}
	return "root"
}

// prepareDatabase checks the database source's input and that none of the
// databases exists in the target service yet (a revert drops them again).
func (m *Manager) prepareDatabase(in *Database) (*Database, error) {
	if in == nil {
		return nil, apperr.BadRequest("database is required for this source")
	}
	db := *in
	db.Host = strings.TrimSpace(db.Host)
	if err := validateServer(db); err != nil {
		return nil, err
	}
	var names []string
	for _, n := range db.Databases {
		n = strings.TrimSpace(n)
		if !dbNameOK(n) {
			return nil, apperr.BadRequest("database name must be 1-64 letters, digits or _")
		}
		if slices.Contains(systemDatabases, strings.ToLower(n)) {
			return nil, apperr.BadRequest("%s is a system database", n)
		}
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, apperr.BadRequest("choose at least one database")
	}
	db.Databases = names
	svc, ok := m.service(db.TargetServiceID)
	if !ok {
		return nil, apperr.NotFound("service %s not found", db.TargetServiceID)
	}
	if !compatible(db.Kind, svc.Kind) {
		want := "a MariaDB or MySQL service"
		if db.Kind == "postgres" {
			want = "a PostgreSQL service"
		}
		return nil, apperr.BadRequest("%s is not %s", svc.Name, want)
	}
	if (db.Host == "127.0.0.1" || strings.EqualFold(db.Host, "localhost") || db.Host == "::1") && db.Port == svc.Port {
		return nil, apperr.BadRequest("that server is %s itself", svc.Name)
	}
	dir, err := m.clientDir(svc.Kind, svc.ID)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	have, err := m.DB.ListRemote(ctx, dbtarget(svc, dir))
	if err != nil {
		return nil, apperr.Conflict("cannot reach %s; start it first (%v)", svc.Name, err)
	}
	for _, h := range have {
		for _, n := range names {
			if strings.EqualFold(h.Name, n) {
				return nil, apperr.Conflict("%s already has a database %s", svc.Name, n)
			}
		}
	}
	return &db, nil
}

func dbtarget(svc model.Service, clientDir string) dbtools.Target {
	return dbtools.Target{Kind: svc.Kind, Host: "127.0.0.1", Port: svc.Port, User: adminUser(svc.Kind), Password: svc.AdminPassword, ClientDir: clientDir}
}

func (m *Manager) copyDatabases(h *jobs.Handle, rec *record, i int, remote *Database) error {
	s := &rec.Steps[i]
	if remote == nil || m.DB == nil {
		return apperr.Internal("database migration without a server")
	}
	dir, err := m.clientDir(remote.Kind, rec.ServiceID)
	if err != nil {
		return err
	}
	s.Started = true
	if err := m.save(rec); err != nil {
		return err
	}
	err = m.DB.CopyFromServer(context.Background(), remoteTarget(*remote, dir), rec.Databases, rec.ServiceID, func(p int, step string) {
		h.Progress(p, step)
	})
	if err != nil {
		return err
	}
	s.Done = true
	return nil
}

func (m *Manager) dropDatabases(rec *record, i int) error {
	s := &rec.Steps[i]
	if !s.Started {
		return nil
	}
	if m.DB == nil {
		return ErrNotImplemented
	}
	if _, ok := m.service(rec.ServiceID); ok && len(rec.Databases) > 0 {
		if err := m.DB.DropDatabases(context.Background(), rec.ServiceID, rec.Databases); err != nil {
			var ae *apperr.Error
			if errors.As(err, &ae) {
				return err
			}
			return fmt.Errorf("cannot drop %s: %w", strings.Join(rec.Databases, ", "), err)
		}
	}
	s.Started, s.Done = false, false
	return nil
}
