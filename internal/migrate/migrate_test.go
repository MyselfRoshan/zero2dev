package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"zero2dev/internal/apperr"
	"zero2dev/internal/dbtools"
	"zero2dev/internal/jobs"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// ---------- fakes ----------

type fakeReg struct {
	mu   sync.Mutex
	keys map[string][]RegValue
}

func (f *fakeReg) ReadTree(path string) ([]RegValue, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.keys[path]
	return append([]RegValue(nil), v...), ok, nil
}

func (f *fakeReg) WriteValues(path string, vals []RegValue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur := f.keys[path]
	for _, v := range vals {
		i := slices.IndexFunc(cur, func(c RegValue) bool { return c.Key == v.Key && c.Name == v.Name })
		if i >= 0 {
			cur[i] = v
		} else {
			cur = append(cur, v)
		}
	}
	f.keys[path] = cur
	return nil
}

func (f *fakeReg) DeleteTree(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.keys, path)
	return nil
}

type fakeCopier struct {
	mu      sync.Mutex
	remote  []dbtools.Database
	local   map[string][]dbtools.Database // by port
	copied  []string
	dropped []string
	src     dbtools.Target
	fail    error
}

func (f *fakeCopier) ListRemote(ctx context.Context, t dbtools.Target) ([]dbtools.Database, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.Host == "127.0.0.1" {
		return f.local[itoa(t.Port)], nil
	}
	return f.remote, nil
}

func (f *fakeCopier) CopyFromServer(ctx context.Context, src dbtools.Target, dbs []string, dst string, progress func(int, string)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.src = src
	if f.fail != nil {
		return f.fail
	}
	f.copied = append(f.copied, dbs...)
	if progress != nil {
		progress(50, "Copying "+dbs[0])
	}
	return nil
}

func (f *fakeCopier) DropDatabases(ctx context.Context, svc string, dbs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropped = append(f.dropped, dbs...)
	return nil
}

// ---------- setup ----------

type tenv struct {
	t       *testing.T
	m       *Manager
	st      *store.Store
	reg     *fakeReg
	db      *fakeCopier
	running map[string]bool
	root    string
}

func setup(t *testing.T) *tenv {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "data")
	st, err := store.Open(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	e := &tenv{t: t, st: st, root: root, reg: &fakeReg{keys: map[string][]RegValue{}}, db: &fakeCopier{local: map[string][]dbtools.Database{}}, running: map[string]bool{}}
	e.m = &Manager{
		Home: home, Bin: filepath.Join(root, "bin"), St: st, Jobs: jobs.New(nil), DB: e.db,
		Env: Env{AppData: filepath.Join(root, "Roaming"), LocalAppData: filepath.Join(root, "Local"), UserProfile: filepath.Join(root, "me")},
		Reg: e.reg, Running: func(img string) bool { return e.running[strings.ToLower(img)] },
		SameVolume: func(a, b string) bool { return true },
	}
	return e
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func (e *tenv) install(kind string) {
	e.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_" + kind, Kind: kind, Version: "1", Source: "installed", Binary: filepath.Join(e.m.Bin, kind, kind+".exe")})
		return nil
	})
}

func (e *tenv) source(id string) Source {
	e.t.Helper()
	list, err := e.m.Sources(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	for _, s := range list {
		if s.ID == id {
			return s
		}
	}
	e.t.Fatalf("no source %s", id)
	return Source{}
}

func (e *tenv) wait(j model.Job) model.Job {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := e.m.Jobs.Get(j.ID)
		if got.State != "running" && !e.m.isActive(j.MigrationID) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.t.Fatalf("job %s did not finish", j.ID)
	return model.Job{}
}

func (e *tenv) run(req Request) Migration {
	e.t.Helper()
	j, err := e.m.Start(req)
	if err != nil {
		e.t.Fatalf("start %+v: %v", req, err)
	}
	if j.Type != model.JobMigrate || j.MigrationID == "" {
		e.t.Fatalf("job %+v", j)
	}
	if got := e.wait(j); got.State != "done" {
		e.t.Fatalf("job failed: %+v", got)
	}
	return e.migration(j.MigrationID)
}

func (e *tenv) revert(id, how string) Migration {
	e.t.Helper()
	j, err := e.m.Revert(id, how)
	if err != nil {
		e.t.Fatalf("revert %s %s: %v", id, how, err)
	}
	if got := e.wait(j); got.State != "done" || got.Type != model.JobMigrateRevert {
		e.t.Fatalf("revert job: %+v", got)
	}
	mg := e.migration(id)
	if mg.State != StateReverted || mg.RevertedAt == nil {
		e.t.Fatalf("after revert: %+v", mg)
	}
	return mg
}

func (e *tenv) migration(id string) Migration {
	e.t.Helper()
	list, err := e.m.List()
	if err != nil {
		e.t.Fatal(err)
	}
	for _, mg := range list {
		if mg.ID == id {
			return mg
		}
	}
	e.t.Fatalf("migration %s not listed", id)
	return Migration{}
}

func status(err error) int {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

// ---------- editors ----------

func (e *tenv) vscodeTree() (user, ext string) {
	user = filepath.Join(e.m.Env.AppData, "Code", "User")
	ext = filepath.Join(e.m.Env.UserProfile, ".vscode", "extensions")
	write(e.t, filepath.Join(user, "settings.json"), `{"editor.fontSize": 14}`)
	extDir := filepath.Join(ext, "ms-python.python-1.0.0")
	write(e.t, filepath.Join(extDir, "package.json"), `{}`)
	entries := []map[string]any{
		{"identifier": map[string]any{"id": "ms-python.python"}, "version": "1.0.0",
			"location": map[string]any{"$mid": 1, "fsPath": extDir, "external": "file:///x", "path": "/" + filepath.ToSlash(extDir), "scheme": "file"}},
		{"identifier": map[string]any{"id": "a.b"}, "relativeLocation": "a.b-2.0.0", "location": map[string]any{"$mid": 1, "fsPath": filepath.Join(ext, "a.b-2.0.0"), "path": "/x", "scheme": "file"}},
	}
	raw, _ := json.Marshal(entries)
	write(e.t, filepath.Join(ext, "extensions.json"), string(raw))
	return user, ext
}

func TestVSCodeCopyAndDiscard(t *testing.T) {
	e := setup(t)
	user, ext := e.vscodeTree()
	src := e.source(ProgVSCode)
	if !src.Found || !slices.Contains(src.Blockers, "Install VS Code in Zero2Dev first") {
		t.Fatalf("before install: %+v", src)
	}
	if _, err := e.m.Start(Request{Source: ProgVSCode, Mode: ModeCopy}); status(err) != http.StatusConflict {
		t.Fatalf("blocked start: %v", err)
	}
	e.install("vscode")
	target := filepath.Join(e.m.Home, "code", "user-data", "User")
	write(t, filepath.Join(target, "old.json"), "old")
	src = e.source(ProgVSCode)
	if len(src.Blockers) != 0 || !src.TargetHas || len(src.Items) != 2 || src.Items[0].SizeBytes == 0 || !slices.Equal(src.Modes, []string{ModeCopy, ModeMove}) {
		t.Fatalf("source: %+v", src)
	}

	mg := e.run(Request{Source: ProgVSCode, Mode: ModeCopy})
	if mg.State != StateDone || !slices.Equal(mg.RevertHow, []string{RevertDiscard, RevertCopyBack}) || !strings.HasPrefix(mg.Description, "Copied VS Code: Settings, Extensions") {
		t.Fatalf("migration: %+v", mg)
	}
	if read(t, filepath.Join(target, "settings.json")) == "" || exists(filepath.Join(target, "old.json")) {
		t.Fatal("target must hold exactly the copy")
	}
	if !exists(filepath.Join(user, "settings.json")) {
		t.Fatal("a copy keeps the original")
	}
	backups, _ := filepath.Glob(filepath.Join(e.m.Home, "migrations", mg.ID, "backup-target", "*", "User", "old.json"))
	if len(backups) != 1 {
		t.Fatalf("target backup: %v", backups)
	}
	// extensions.json points at the new folder.
	newExt := filepath.Join(e.m.Home, "code", "extensions")
	var entries []map[string]any
	if err := json.Unmarshal([]byte(read(t, filepath.Join(newExt, "extensions.json"))), &entries); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"ms-python.python-1.0.0", "a.b-2.0.0"} {
		loc := entries[i]["location"].(map[string]any)
		if !strings.EqualFold(loc["fsPath"].(string), filepath.Join(newExt, want)) || !strings.HasSuffix(loc["path"].(string), "/"+want) {
			t.Errorf("entry %d location %v", i, loc)
		}
	}
	if ext0 := entries[0]["location"].(map[string]any)["external"].(string); !strings.HasPrefix(ext0, "file://") || strings.Contains(ext0, `\`) {
		t.Errorf("external %q", ext0)
	}
	if strings.Contains(read(t, filepath.Join(ext, "extensions.json")), filepath.ToSlash(newExt)) {
		t.Error("the original extensions.json must not change")
	}
	if src := e.source(ProgVSCode); !slices.ContainsFunc(src.Blockers, func(b string) bool { return strings.Contains(b, "already imported") }) {
		t.Fatalf("second migration must be blocked: %+v", src.Blockers)
	}

	e.revert(mg.ID, RevertDiscard)
	if read(t, filepath.Join(target, "old.json")) != "old" || exists(filepath.Join(target, "settings.json")) {
		t.Fatal("discard must bring back what the target held")
	}
	if exists(newExt) {
		t.Fatal("discard removes the copied extensions")
	}
	if _, err := e.m.Revert(mg.ID, RevertDiscard); status(err) != http.StatusConflict {
		t.Fatalf("second revert: %v", err)
	}
}

func TestCopyBackSavesOriginals(t *testing.T) {
	e := setup(t)
	e.install("notepadpp")
	orig := filepath.Join(e.m.Env.AppData, "Notepad++")
	write(t, filepath.Join(orig, "config.xml"), "v1")
	mg := e.run(Request{Source: ProgNotepadPP, Mode: ModeCopy, Items: []string{"settings"}})
	write(t, filepath.Join(e.m.Home, "notepadpp", "config.xml"), "v2")
	write(t, filepath.Join(orig, "config.xml"), "v1b")
	if _, err := e.m.Revert(mg.ID, RevertMoveBack); status(err) != http.StatusBadRequest {
		t.Fatalf("move-back of a copy: %v", err)
	}
	e.revert(mg.ID, RevertCopyBack)
	if read(t, filepath.Join(orig, "config.xml")) != "v2" {
		t.Fatal("copy-back must copy the portable data over the original")
	}
	saved, _ := filepath.Glob(filepath.Join(e.m.Home, "migrations", mg.ID, "backup-original", "*", "Notepad++", "config.xml"))
	if len(saved) != 1 || read(t, saved[0]) != "v1b" {
		t.Fatalf("the original is saved first: %v", saved)
	}
	if read(t, filepath.Join(e.m.Home, "notepadpp", "config.xml")) != "v2" {
		t.Fatal("copy-back keeps the portable copy")
	}
}

func TestMoveAcrossVolumesAndMoveBack(t *testing.T) {
	e := setup(t)
	e.install("dbeaver")
	e.m.SameVolume = func(a, b string) bool { return false }
	orig := filepath.Join(e.m.Env.AppData, "DBeaverData", "workspace6")
	write(t, filepath.Join(orig, "General", "data-sources.json"), "conns")
	mg := e.run(Request{Source: ProgDBeaver, Mode: ModeMove})
	if exists(orig) {
		t.Fatal("a move removes the original after the checked copy")
	}
	ws := filepath.Join(e.m.Home, "dbeaver", "workspace")
	if read(t, filepath.Join(ws, "General", "data-sources.json")) != "conns" || !slices.Equal(mg.RevertHow, []string{RevertMoveBack}) {
		t.Fatalf("after move: %+v", mg)
	}
	write(t, filepath.Join(orig, "new.txt"), "recreated") // DBeaver started again meanwhile
	e.revert(mg.ID, RevertMoveBack)
	if read(t, filepath.Join(orig, "General", "data-sources.json")) != "conns" || exists(filepath.Join(orig, "new.txt")) {
		t.Fatal("move-back puts the items back")
	}
	saved, _ := filepath.Glob(filepath.Join(e.m.Home, "migrations", mg.ID, "backup-original", "*", "workspace6", "new.txt"))
	if len(saved) != 1 {
		t.Fatal("what the original place held by then is saved first")
	}
	if exists(ws) {
		t.Fatal("the portable place is empty again")
	}
}

func TestMoveStrategy(t *testing.T) {
	e := setup(t)
	e.m.SameVolume = func(a, b string) bool { return true }
	if e.m.moveStrategy(`C:\a`, `C:\b`) != moveRename {
		t.Fatal("same volume: rename")
	}
	e.m.SameVolume = func(a, b string) bool { return false }
	if e.m.moveStrategy(`C:\a`, `D:\b`) != moveCopy {
		t.Fatal("other volume: copy, check, delete")
	}
	if !defaultSameVolume(`C:\x\y`, `c:\z`) || defaultSameVolume(`C:\x`, `D:\x`) && filepath.VolumeName(`C:\x`) != "" {
		t.Fatal("defaultSameVolume")
	}
	src := filepath.Join(e.root, "src")
	write(t, filepath.Join(src, "a", "b.txt"), "hello")
	var got int64
	if err := e.m.movePath(src, filepath.Join(e.root, "dst"), func(n int64) { got += n }); err != nil {
		t.Fatal(err)
	}
	if exists(src) || read(t, filepath.Join(e.root, "dst", "a", "b.txt")) != "hello" || got != 5 {
		t.Fatalf("copy move: progress %d", got)
	}
}

func TestBlockersAndCopyOnly(t *testing.T) {
	e := setup(t)
	e.install("vscode")
	e.vscodeTree()
	e.running["code.exe"] = true
	src := e.source(ProgVSCode)
	if !slices.Contains(src.Blockers, "Close VS Code first") {
		t.Fatalf("blockers %v", src.Blockers)
	}
	_, err := e.m.Start(Request{Source: ProgVSCode, Mode: ModeCopy})
	if status(err) != http.StatusConflict || err.Error() != "Close VS Code first" {
		t.Fatalf("start while running: %v", err)
	}
	if _, err := e.m.Start(Request{Source: "nope", Mode: ModeCopy}); status(err) != http.StatusNotFound {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := e.m.Start(Request{Source: ProgVSCode, Mode: "steal"}); status(err) != http.StatusBadRequest {
		t.Fatalf("bad mode: %v", err)
	}
	e.running = map[string]bool{}
	if _, err := e.m.Start(Request{Source: ProgVSCode, Mode: ModeCopy, Items: []string{"Nope"}}); status(err) != http.StatusBadRequest {
		t.Fatalf("unknown item: %v", err)
	}
	if _, err := e.m.Start(Request{Source: ProgComposer, Mode: ModeCopy}); status(err) != http.StatusConflict {
		t.Fatalf("nothing found: %v", err)
	}
	db := e.source(ProgDatabase)
	if !slices.Equal(db.Modes, []string{ModeCopy}) || db.CopyOnly == "" {
		t.Fatalf("database source: %+v", db)
	}
	if _, err := e.m.Start(Request{Source: ProgDatabase, Mode: ModeMove}); status(err) != http.StatusBadRequest {
		t.Fatalf("move of a copy-only source: %v", err)
	}
}

// ---------- HeidiSQL ----------

func heidiValues() []RegValue {
	bin := RegValue{Key: `Servers\Local`, Name: "Blob", Type: regBinary, Data: []byte{1, 2, 3}}
	return []RegValue{
		DWordValue("", "MainWinMaximized", 1),
		StringValue(`Servers\Local`, "Host", "127.0.0.1"),
		StringValue(`Servers\Local`, "StartupScript", "line1\r\nline2"),
		DWordValue(`Servers\Local`, "Port", 3306),
		DWordValue(`Servers\Local`, "Minus", 0xFFFFFFFF),
		bin,
	}
}

func TestHeidiFormatRoundTrip(t *testing.T) {
	data, skipped := encodePortable(heidiValues())
	if skipped != 1 {
		t.Fatalf("skipped %d", skipped)
	}
	want := "MainWinMaximized<|||>3<|||>1\r\n" +
		`Servers\Local\Host<|||>1<|||>127.0.0.1` + "\r\n" +
		`Servers\Local\StartupScript<|||>1<|||>line1<{{{><}}}>line2` + "\r\n" +
		`Servers\Local\Port<|||>3<|||>3306` + "\r\n" +
		`Servers\Local\Minus<|||>3<|||>-1` + "\r\n"
	if string(data) != want {
		t.Fatalf("got\n%s\nwant\n%s", data, want)
	}
	back, err := decodePortable(append([]byte{0xEF, 0xBB, 0xBF}, data...))
	if err != nil || len(back) != 5 {
		t.Fatalf("decode: %v %d", err, len(back))
	}
	if utf16String(back[2].Data) != "line1\r\nline2" || back[4].Type != regDWORD || string(back[4].Data) != "\xff\xff\xff\xff" {
		t.Fatalf("decoded %+v", back)
	}
	if _, err := decodePortable([]byte("broken line\r\n")); err == nil {
		t.Fatal("a line without 3 parts is an error")
	}
	reg := regFile(heidiKey, heidiValues())
	if reg[0] != 0xFF || reg[1] != 0xFE || !strings.Contains(utf16String(append(reg[2:], 0, 0)), `[HKEY_CURRENT_USER\Software\HeidiSQL\Servers\Local]`) {
		t.Fatal(".reg header")
	}
}

func TestHeidiCopyAndMove(t *testing.T) {
	e := setup(t)
	e.install("heidisql")
	if e.source(ProgHeidiSQL).Found {
		t.Fatal("no key: not found")
	}
	e.reg.keys[heidiKey] = heidiValues()
	target := filepath.Join(e.m.Home, "heidisql", "portable_settings.txt")
	write(t, target, "") // what Zero2Dev's launch creates
	src := e.source(ProgHeidiSQL)
	if !src.Found || src.TargetHas || src.Items[0].Kind != KindRegistry || src.Items[0].SizeBytes == 0 {
		t.Fatalf("source %+v", src)
	}
	mg := e.run(Request{Source: ProgHeidiSQL, Mode: ModeCopy})
	if !strings.Contains(read(t, target), `Servers\Local\Port<|||>3<|||>3306`) {
		t.Fatal("portable_settings.txt")
	}
	if _, ok := e.reg.keys[heidiKey]; !ok {
		t.Fatal("copy keeps the key")
	}
	e.revert(mg.ID, RevertDiscard)
	if read(t, target) != "" {
		t.Fatal("discard brings back the empty file")
	}

	mg = e.run(Request{Source: ProgHeidiSQL, Mode: ModeMove})
	if _, ok := e.reg.keys[heidiKey]; ok {
		t.Fatal("move deletes the key")
	}
	dir := filepath.Join(e.m.Home, "migrations", mg.ID, "backup-original")
	if m, _ := filepath.Glob(filepath.Join(dir, "*", "registry.json")); len(m) != 1 {
		t.Fatal("key backup (json)")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*", "HeidiSQL.reg")); len(m) != 1 {
		t.Fatal("key backup (.reg)")
	}
	// HeidiSQL (portable) changed a setting in the file meanwhile.
	write(t, target, strings.Replace(read(t, target), "127.0.0.1", "db.local", 1))
	e.revert(mg.ID, RevertMoveBack)
	vals := e.reg.keys[heidiKey]
	if len(vals) != 6 {
		t.Fatalf("restored %d values", len(vals))
	}
	for _, v := range vals {
		if v.Name == "Host" && utf16String(v.Data) != "db.local" {
			t.Fatal("move-back writes the portable file's values")
		}
		if v.Name == "Blob" && v.Type != regBinary {
			t.Fatal("values the file cannot hold come back from the backup")
		}
	}
	if read(t, target) != "" {
		t.Fatal("the portable place is as before")
	}
}

// ---------- runtimes ----------

func (e *tenv) registered(kind, version string, files map[string]string, binRel string) model.Runtime {
	dir := filepath.Join(e.root, "Program Files", kind)
	for rel, c := range files {
		write(e.t, filepath.Join(dir, filepath.FromSlash(rel)), c)
	}
	rt := model.Runtime{ID: "rt_" + kind + "1", Kind: kind, Version: version, Name: kind + " " + version, Source: "registered", Binary: filepath.Join(dir, filepath.FromSlash(binRel))}
	e.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, rt)
		d.Services = append(d.Services, model.Service{ID: "svc_" + kind, Name: "app", Kind: kind, RuntimeID: rt.ID})
		return nil
	})
	return rt
}

func (e *tenv) runtime(id string) model.Runtime {
	for _, r := range e.st.Snapshot().Runtimes {
		if r.ID == id {
			return r
		}
	}
	e.t.Fatalf("runtime %s gone", id)
	return model.Runtime{}
}

func TestRuntimeAdoptionKeepsIDAndRevert(t *testing.T) {
	e := setup(t)
	rt := e.registered("node", "22.1.0", map[string]string{"node.exe": "exe", "node_modules/npm/a.js": "npm"}, "node.exe")
	id := "runtime:" + rt.ID
	src := e.source(id)
	if src.Program != ProgRuntime || src.RuntimeID != rt.ID || !slices.Equal(src.Modes, []string{ModeCopy}) || src.CopyOnly == "" || len(src.Blockers) != 0 {
		t.Fatalf("source %+v", src)
	}
	if _, err := e.m.Start(Request{Source: id, Mode: ModeMove}); status(err) != http.StatusBadRequest {
		t.Fatalf("move: %v", err)
	}
	mg := e.run(Request{Source: id, Mode: ModeCopy})
	got := e.runtime(rt.ID)
	wantBin := filepath.Join(e.m.Bin, "adopted", "node-22.1.0", "node.exe")
	if got.Source != "installed" || got.Binary != wantBin || mg.RuntimeID != rt.ID || !slices.Equal(mg.RevertHow, []string{RevertDiscard}) {
		t.Fatalf("adopted %+v / %+v", got, mg)
	}
	if read(t, filepath.Join(e.m.Bin, "adopted", "node-22.1.0", "node_modules", "npm", "a.js")) != "npm" || !exists(rt.Binary) {
		t.Fatal("the whole folder is copied and the original stays")
	}
	for _, s := range e.st.Snapshot().Services {
		if s.ID == "svc_node" && s.RuntimeID != rt.ID {
			t.Fatal("services keep the runtime id")
		}
	}
	if _, err := e.m.Sources(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.revert(mg.ID, RevertDiscard)
	if got := e.runtime(rt.ID); got.Source != "registered" || got.Binary != rt.Binary {
		t.Fatalf("after revert %+v", got)
	}
	if exists(filepath.Join(e.m.Bin, "adopted", "node-22.1.0")) {
		t.Fatal("the copy is removed")
	}
}

func TestRuntimeAdoptionLeavesDatabaseDataOut(t *testing.T) {
	e := setup(t)
	rt := e.registered("mariadb", "11.4", map[string]string{"bin/mariadbd.exe": "exe", "share/x.sql": "s", "data/ibdata1": "big"}, "bin/mariadbd.exe")
	e.run(Request{Source: "runtime:" + rt.ID, Mode: ModeCopy})
	dir := filepath.Join(e.m.Bin, "adopted", "mariadb-11.4")
	if !exists(filepath.Join(dir, "bin", "mariadbd.exe")) || !exists(filepath.Join(dir, "share", "x.sql")) || exists(filepath.Join(dir, "data")) {
		t.Fatal("root is the folder above bin, without the server's data")
	}
	if got := e.runtime(rt.ID); got.Binary != filepath.Join(dir, "bin", "mariadbd.exe") {
		t.Fatalf("binary %s", got.Binary)
	}
}

// ---------- databases ----------

func TestDatabaseCopyAndRevert(t *testing.T) {
	e := setup(t)
	e.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_m", Kind: "mariadb", Source: "installed", Binary: filepath.Join(e.m.Bin, "mariadb", "bin", "mariadbd.exe")})
		d.Services = append(d.Services, model.Service{ID: "svc_db", Name: "Shop DB", Kind: "mariadb", Port: 3307, AdminPassword: "pw"})
		return nil
	})
	e.db.remote = []dbtools.Database{{Name: "shop"}, {Name: "blog"}}
	list, err := e.m.ListRemoteDatabases(context.Background(), Database{Kind: "mysql", Host: "10.0.0.5", Port: 3306, User: "root"})
	if err != nil || len(list) != 2 {
		t.Fatalf("list %v %v", list, err)
	}
	if _, err := e.m.ListRemoteDatabases(context.Background(), Database{Kind: "redis", Host: "h", Port: 1, User: "u"}); status(err) != 400 {
		t.Fatalf("redis: %v", err)
	}
	req := Request{Source: ProgDatabase, Mode: ModeCopy, Database: &Database{Kind: "mysql", Host: "10.0.0.5", Port: 3306, User: "root", Password: "secret", Databases: []string{"shop"}, TargetServiceID: "svc_db"}}
	bad := *req.Database
	bad.Databases = []string{"mysql"}
	if _, err := e.m.Start(Request{Source: ProgDatabase, Mode: ModeCopy, Database: &bad}); status(err) != 400 {
		t.Fatalf("system database: %v", err)
	}
	e.db.local["3307"] = []dbtools.Database{{Name: "shop"}}
	if _, err := e.m.Start(req); status(err) != http.StatusConflict {
		t.Fatalf("existing database: %v", err)
	}
	e.db.local["3307"] = nil
	mg := e.run(req)
	if !slices.Equal(e.db.copied, []string{"shop"}) || e.db.src.Password != "secret" || e.db.src.ClientDir != filepath.Join(e.m.Bin, "mariadb", "bin") {
		t.Fatalf("copy %v %+v", e.db.copied, e.db.src)
	}
	if mg.ServiceID != "svc_db" || !slices.Equal(mg.Databases, []string{"shop"}) || !strings.Contains(mg.Description, "into Shop DB") {
		t.Fatalf("migration %+v", mg)
	}
	if strings.Contains(read(t, filepath.Join(e.m.Home, "migrations", mg.ID, "migration.json")), "secret") {
		t.Fatal("the password must not be written")
	}
	e.revert(mg.ID, RevertDiscard)
	if !slices.Equal(e.db.dropped, []string{"shop"}) {
		t.Fatalf("dropped %v", e.db.dropped)
	}
}

func TestDatabaseFailureIsRolledBack(t *testing.T) {
	e := setup(t)
	e.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_p", Kind: "postgres", Source: "installed", Binary: filepath.Join(e.m.Bin, "pg", "bin", "postgres.exe")})
		d.Services = append(d.Services, model.Service{ID: "svc_pg", Name: "PG", Kind: "postgres", Port: 5433})
		return nil
	})
	e.db.fail = errors.New("pg_dump: access denied")
	j, err := e.m.Start(Request{Source: ProgDatabase, Mode: ModeCopy, Database: &Database{Kind: "postgres", Host: "db", Port: 5432, User: "postgres", Databases: []string{"app"}, TargetServiceID: "svc_pg"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := e.wait(j); got.State != "failed" || !strings.Contains(got.Error, "access denied") {
		t.Fatalf("job %+v", got)
	}
	mg := e.migration(j.MigrationID)
	if mg.State != StateFailed || len(mg.RevertHow) != 0 || !slices.Equal(e.db.dropped, []string{"app"}) {
		t.Fatalf("rolled back: %+v dropped %v", mg, e.db.dropped)
	}
}

// ---------- portable switches ----------

func TestPortableSwitchedByMigrationAndRevert(t *testing.T) {
	e := setup(t)
	write(t, filepath.Join(e.m.Env.AppData, "npm", "tsc.cmd"), "x")
	write(t, filepath.Join(e.m.Env.UserProfile, "go", "pkg", "mod", "cache", "a"), "m")
	write(t, filepath.Join(e.m.Env.LocalAppData, "go-build", "00", "b"), "b")
	npm := e.run(Request{Source: ProgNpmGlobal, Mode: ModeMove})
	if !e.st.Snapshot().Settings.Portable.NpmGlobal || !exists(filepath.Join(e.m.Home, "node", "global", "tsc.cmd")) {
		t.Fatal("npm-global switches NpmGlobal on")
	}
	gm := e.run(Request{Source: ProgGoCache, Mode: ModeCopy, Items: []string{"Build cache"}})
	p := e.st.Snapshot().Settings.Portable
	if !p.GoCache || p.Composer || !exists(filepath.Join(e.m.Home, "go", "cache", "00", "b")) || exists(filepath.Join(e.m.Home, "go", "path")) {
		t.Fatalf("go-cache: %+v", p)
	}
	if len(gm.Items) != 1 {
		t.Fatalf("only the chosen item: %+v", gm.Items)
	}
	e.revert(npm.ID, RevertMoveBack)
	if e.st.Snapshot().Settings.Portable.NpmGlobal || !exists(filepath.Join(e.m.Env.AppData, "npm", "tsc.cmd")) {
		t.Fatal("revert switches NpmGlobal off and puts the folder back")
	}
	if list, _ := e.m.List(); len(list) != 2 || list[0].ID != gm.ID && !list[0].CreatedAt.Equal(list[1].CreatedAt) {
		t.Fatalf("list newest first: %+v", list)
	}
}

func TestProductionDefaultsOnlyWithoutEnv(t *testing.T) {
	m := &Manager{Env: Env{AppData: "x"}}
	if m.production() {
		t.Fatal("an Env set in a test is not production")
	}
	if _, ok := m.reg().(emptyRegistry); !ok {
		t.Fatal("tests without a fake registry get an empty one, never the real HKCU")
	}
	if m.running("Code.exe") {
		t.Fatal("no process check in tests")
	}
}
