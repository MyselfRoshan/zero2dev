package envreg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// useFakeReg swaps the user environment for an in-memory one, so no test can write
// the real registry, and counts the WM_SETTINGCHANGE broadcasts.
func useFakeReg(t *testing.T) (reg *fakeReg, broadcasts *int) {
	t.Helper()
	oldOpen, oldBroadcast := openReg, broadcast
	t.Cleanup(func() { openReg, broadcast = oldOpen, oldBroadcast })
	reg, broadcasts = newFake(), new(int)
	openReg = func() registry { return reg }
	broadcast = func() { *broadcasts++ }
	return reg, broadcasts
}

// mkRuntime puts a runtime's program on disk, as the resolver wants it to be there.
func mkRuntime(t *testing.T, home, id, kind, rel string, def bool) model.Runtime {
	t.Helper()
	bin := filepath.Join(home, "bin", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return model.Runtime{ID: id, Kind: kind, Name: id, Binary: bin, Source: "installed", Default: def}
}

func TestResolveFollowsTheDefaultAndSpecificRuntimes(t *testing.T) {
	home := t.TempDir()
	j17 := mkRuntime(t, home, "rt_j17", "java", "jdk-17/bin/java.exe", false)
	j21 := mkRuntime(t, home, "rt_j21", "java", "jdk-21/bin/java.exe", true)
	node := mkRuntime(t, home, "rt_node", "node", "node-26/node.exe", true)
	d := store.Data{Runtimes: []model.Runtime{j17, j21, node}, Env: model.EnvConfig{
		Path: []model.EnvPath{
			{ID: "envp_shims", Source: model.EnvShims, Enabled: true},
			{ID: "envp_node", Source: model.EnvRuntime, RuntimeID: "rt_node", Enabled: true},
			{ID: "envp_j17", Source: model.EnvRuntime, RuntimeID: "rt_j17"}, // off
			{ID: "envp_tools", Source: model.EnvCustom, Dir: `C:\tools\bin`, Enabled: true},
		},
		Vars: []model.EnvVar{
			{ID: "envv_a", Name: "JAVA_HOME", Source: model.EnvDefault, Kind: "java", Enabled: true},
			{ID: "envv_b", Name: "JAVA17_HOME", Source: model.EnvRuntime, RuntimeID: "rt_j17", Enabled: true},
			{ID: "envv_c", Name: "NODE_HOME", Source: model.EnvRuntime, RuntimeID: "rt_node", Enabled: true},
			{ID: "envv_d", Name: "GOROOT", Source: model.EnvDefault, Kind: "go", Enabled: true},
			{ID: "envv_e", Name: "OFF_ONE", Source: model.EnvCustom, Value: "x"},
			{ID: "envv_f", Name: "MY_FLAG", Source: model.EnvCustom, Value: "1", Enabled: true},
		},
	}}
	plan := Resolve(home, d, fileExists)

	want := []string{ShimsDir(home), filepath.Dir(node.Binary), `C:\tools\bin`}
	if strings.Join(plan.Desired.Dirs, "|") != strings.Join(want, "|") {
		t.Fatalf("dirs = %v, want %v", plan.Desired.Dirs, want)
	}
	wantVars := map[string]string{
		"JAVA_HOME":   filepath.Join(home, "bin", "jdk-21"),
		"JAVA17_HOME": filepath.Join(home, "bin", "jdk-17"),
		"NODE_HOME":   filepath.Dir(node.Binary),
		"MY_FLAG":     "1",
	}
	if len(plan.Desired.Vars) != len(wantVars) {
		t.Fatalf("vars = %v", plan.Desired.Vars)
	}
	for k, v := range wantVars {
		if plan.Desired.Vars[k] != v {
			t.Errorf("%s = %q, want %q", k, plan.Desired.Vars[k], v)
		}
	}
	if r := plan.Vars["envv_d"]; r.Value != "" || !strings.Contains(r.Problem, "No go runtime") {
		t.Errorf("a kind without runtime is a problem, not a value: %+v", r)
	}
	// items that are off still show what they stand for
	if r := plan.Path["envp_j17"]; r.Value != filepath.Dir(j17.Binary) || r.Problem != "" {
		t.Errorf("off item: %+v", r)
	}
	if r := plan.Vars["envv_e"]; r.Value != "x" {
		t.Errorf("off var: %+v", r)
	}
}

func TestResolveSkipsRuntimesThatAreGoneOrNotOnDisk(t *testing.T) {
	home := t.TempDir()
	node := mkRuntime(t, home, "rt_node", "node", "node/node.exe", true)
	d := store.Data{Runtimes: []model.Runtime{node}, Env: model.EnvConfig{
		Path: []model.EnvPath{{ID: "envp_a", Source: model.EnvRuntime, RuntimeID: "rt_removed", Enabled: true}},
		Vars: []model.EnvVar{{ID: "envv_a", Name: "NODE_HOME", Source: model.EnvDefault, Kind: "node", Enabled: true}},
	}}
	if p := Resolve(home, d, fileExists); len(p.Desired.Dirs) != 0 || !strings.Contains(p.Path["envp_a"].Problem, "removed") {
		t.Fatalf("a removed runtime: %+v", p)
	}
	if p := Resolve(home, d, func(string) bool { return false }); len(p.Desired.Vars) != 0 || !strings.Contains(p.Vars["envv_a"].Problem, "not on disk") {
		t.Fatalf("a runtime whose program is gone: %+v", p)
	}
}

func TestValidate(t *testing.T) {
	rts := []model.Runtime{{ID: "rt_1", Kind: "java", Binary: `C:\j\bin\java.exe`}}
	bad := func(name string, cfg model.EnvConfig, want string) {
		t.Helper()
		if _, err := Validate(cfg, rts); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want it to mention %q", name, err, want)
		}
	}
	v := func(name, src string) model.EnvVar {
		return model.EnvVar{Name: name, Source: src, Value: "x", Kind: "java", RuntimeID: "rt_1", Enabled: true}
	}
	bad("reserved", model.EnvConfig{Vars: []model.EnvVar{v("Path", "custom")}}, "cannot be set here")
	bad("reserved case", model.EnvConfig{Vars: []model.EnvVar{v("systemroot", "custom")}}, "cannot be set here")
	bad("name", model.EnvConfig{Vars: []model.EnvVar{v("1ABC", "custom")}}, "not a valid name")
	bad("name chars", model.EnvConfig{Vars: []model.EnvVar{v("A=B", "custom")}}, "not a valid name")
	bad("twice", model.EnvConfig{Vars: []model.EnvVar{v("MY_A", "custom"), v("my_a", "custom")}}, "twice")
	bad("kind", model.EnvConfig{Vars: []model.EnvVar{{Name: "X", Source: "default", Kind: "cobol"}}}, "runtime kind")
	bad("source", model.EnvConfig{Vars: []model.EnvVar{{Name: "X", Source: "magic"}}}, "source must be")
	bad("empty value on", model.EnvConfig{Vars: []model.EnvVar{{Name: "X", Source: "custom", Enabled: true}}}, "enter a value")
	bad("missing runtime on", model.EnvConfig{Vars: []model.EnvVar{{Name: "X", Source: "runtime", RuntimeID: "rt_9", Enabled: true}}}, "not installed")
	bad("relative dir", model.EnvConfig{Path: []model.EnvPath{{Source: "custom", Dir: `tools\bin`}}}, "full path")
	bad("percent dir", model.EnvConfig{Path: []model.EnvPath{{Source: "custom", Dir: `C:\a%b`}}}, "cannot go on PATH")
	bad("semicolon dir", model.EnvConfig{Path: []model.EnvPath{{Source: "custom", Dir: `C:\a;b`}}}, "cannot go on PATH")
	bad("dir twice", model.EnvConfig{Path: []model.EnvPath{{Source: "custom", Dir: `C:\a`}, {Source: "custom", Dir: `c:/A/`}}}, "twice")
	bad("shims twice", model.EnvConfig{Path: []model.EnvPath{{Source: "shims"}, {Source: "shims"}}}, "twice")
	bad("runtime twice", model.EnvConfig{Path: []model.EnvPath{{Source: "runtime", RuntimeID: "rt_1"}, {Source: "runtime", RuntimeID: "rt_1"}}}, "twice")
	bad("id", model.EnvConfig{Path: []model.EnvPath{{ID: "x y", Source: "shims"}}}, "not a valid item id")
	bad("id prefix", model.EnvConfig{Path: []model.EnvPath{{ID: "envv_1", Source: "shims"}}}, "not a valid item id")
	bad("id twice", model.EnvConfig{Path: []model.EnvPath{{ID: "envp_1", Source: "shims"}, {ID: "envp_1", Source: "custom", Dir: `C:\a`}}}, "used twice")

	// a runtime that is gone is fine for an item that is off
	got, err := Validate(model.EnvConfig{
		Path: []model.EnvPath{{Source: " Custom ", Dir: ` C:\tools `, Enabled: true}, {Source: "runtime", RuntimeID: "rt_gone"}, {ID: "envp_shims", Source: "shims", Dir: "ignored"}},
		Vars: []model.EnvVar{{Name: " MY_VAR ", Source: "CUSTOM", Value: "v", Kind: "java", Enabled: true}, {Name: "OLD", Source: "runtime", RuntimeID: "rt_gone"}},
	}, rts)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Seeded || got.Path[0].Dir != `C:\tools` || got.Path[0].Source != "custom" || !strings.HasPrefix(got.Path[0].ID, "envp_") ||
		got.Path[2].ID != "envp_shims" || got.Path[2].Dir != "" ||
		got.Vars[0].Name != "MY_VAR" || got.Vars[0].Kind != "" || !strings.HasPrefix(got.Vars[0].ID, "envv_") || got.Path[0].ID == got.Path[1].ID {
		t.Fatalf("cleaned = %+v", got)
	}
	if empty, err := Validate(model.EnvConfig{}, nil); err != nil || empty.Path == nil || empty.Vars == nil {
		t.Fatalf("an empty config is valid and has empty lists: %+v %v", empty, err)
	}
}

func TestRefreshAppliesOnlyWhatIsSwitchedOn(t *testing.T) {
	reg, broadcasts := useFakeReg(t)
	home := t.TempDir()
	reg.vals["Path"] = fakeVal{`C:\Tools`, regExpandSZ}
	j := mkRuntime(t, home, "rt_j", "java", "jdk/bin/java.exe", true)
	g := mkRuntime(t, home, "rt_g", "go", "go/bin/go.exe", true)
	d := store.Data{Runtimes: []model.Runtime{j, g}, Env: model.EnvConfig{
		Path: []model.EnvPath{{ID: "envp_shims", Source: model.EnvShims}},
		Vars: []model.EnvVar{
			{ID: "envv_j", Name: "JAVA_HOME", Source: model.EnvDefault, Kind: "java"},
			{ID: "envv_g", Name: "GOROOT", Source: model.EnvDefault, Kind: "go"},
		},
	}}

	// nothing on: shims are written, the registry is not touched
	if err := Refresh(home, d); err != nil || reg.writes != 0 || *broadcasts != 0 {
		t.Fatalf("all off: err=%v writes=%d", err, reg.writes)
	}
	if _, err := os.Stat(filepath.Join(ShimsDir(home), "java.cmd")); err != nil {
		t.Fatalf("shims are always written: %v", err)
	}

	// only JAVA_HOME on
	d.Env.Vars[0].Enabled = true
	if err := Apply(home, d); err != nil {
		t.Fatal(err)
	}
	if reg.vals["JAVA_HOME"].v != filepath.Join(home, "bin", "jdk") {
		t.Fatalf("JAVA_HOME = %+v", reg.vals["JAVA_HOME"])
	}
	if _, ok := reg.vals["GOROOT"]; ok || reg.vals["Path"].v != `C:\Tools` {
		t.Fatalf("only what is on is written: %+v", reg.vals)
	}
	if *broadcasts != 1 {
		t.Fatalf("broadcasts = %d", *broadcasts)
	}

	// the shims folder joins, then GOROOT, one at a time
	d.Env.Path[0].Enabled = true
	if err := Apply(home, d); err != nil || !strings.HasPrefix(reg.vals["Path"].v, ShimsDir(home)+";") {
		t.Fatalf("shims on: err=%v Path=%+v", err, reg.vals["Path"])
	}
	d.Env.Vars[1].Enabled = true
	if err := Apply(home, d); err != nil || reg.vals["GOROOT"].v != filepath.Join(home, "bin", "go") {
		t.Fatalf("GOROOT on: err=%v", err)
	}

	// nothing changed: no writes, no broadcast
	writes, b := reg.writes, *broadcasts
	if err := Refresh(home, d); err != nil || reg.writes != writes || *broadcasts != b {
		t.Fatalf("no-op refresh: err=%v", err)
	}

	// switching one off removes only it
	d.Env.Vars[0].Enabled = false
	if err := Apply(home, d); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.vals["JAVA_HOME"]; ok || reg.vals["GOROOT"].v == "" || !strings.HasPrefix(reg.vals["Path"].v, ShimsDir(home)) {
		t.Fatalf("only JAVA_HOME may go: %+v", reg.vals)
	}

	// everything off, via Refresh: Zero2Dev still owns things, so it gives them back
	d.Env.Vars[1].Enabled, d.Env.Path[0].Enabled = false, false
	if err := Refresh(home, d); err != nil {
		t.Fatal(err)
	}
	if reg.vals["Path"].v != `C:\Tools` || len(reg.vals) != 1 {
		t.Fatalf("back to what it was: %+v", reg.vals)
	}
	if _, err := os.Stat(BackupFile(home)); !os.IsNotExist(err) {
		t.Fatal("no backup stays when Zero2Dev owns nothing")
	}
}

func TestRefreshFollowsMakeDefaultAndRemovedRuntimes(t *testing.T) {
	reg, _ := useFakeReg(t)
	home := t.TempDir()
	j17 := mkRuntime(t, home, "rt_17", "java", "jdk-17/bin/java.exe", true)
	j21 := mkRuntime(t, home, "rt_21", "java", "jdk-21/bin/java.exe", false)
	d := store.Data{Runtimes: []model.Runtime{j17, j21}, Env: model.EnvConfig{Vars: []model.EnvVar{
		{ID: "envv_a", Name: "JAVA_HOME", Source: model.EnvDefault, Kind: "java", Enabled: true},
		{ID: "envv_b", Name: "JAVA21_HOME", Source: model.EnvRuntime, RuntimeID: "rt_21", Enabled: true},
	}}}
	if err := Refresh(home, d); err != nil {
		t.Fatal(err)
	}
	if reg.vals["JAVA_HOME"].v != filepath.Join(home, "bin", "jdk-17") {
		t.Fatalf("JAVA_HOME = %+v", reg.vals["JAVA_HOME"])
	}
	// Make default on 21: the default-following variable moves, the pinned one does not
	d.Runtimes[0].Default, d.Runtimes[1].Default = false, true
	if err := Refresh(home, d); err != nil {
		t.Fatal(err)
	}
	if reg.vals["JAVA_HOME"].v != filepath.Join(home, "bin", "jdk-21") || reg.vals["JAVA21_HOME"].v != filepath.Join(home, "bin", "jdk-21") {
		t.Fatalf("after Make default: %+v", reg.vals)
	}
	// the pinned runtime is deleted: its variable leaves the environment
	d.Runtimes = d.Runtimes[:1]
	d.Runtimes[0].Default = true
	if err := Refresh(home, d); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.vals["JAVA21_HOME"]; ok || reg.vals["JAVA_HOME"].v != filepath.Join(home, "bin", "jdk-17") {
		t.Fatalf("after delete: %+v", reg.vals)
	}
}

func TestApplyOffWindows(t *testing.T) {
	oldOpen := openReg
	t.Cleanup(func() { openReg = oldOpen })
	openReg = func() registry { return nil }
	home := t.TempDir()
	d := store.Data{Env: model.EnvConfig{Path: []model.EnvPath{{ID: "envp_shims", Source: model.EnvShims}}}}
	if err := Apply(home, d); err != nil {
		t.Fatalf("nothing on, nothing to do: %v", err)
	}
	d.Env.Path[0].Enabled = true
	if err := Apply(home, d); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
	if err := Refresh(home, d); err != nil {
		t.Fatalf("Refresh does not nag where this is not possible: %v", err)
	}
	if rep := Inspect(home, d); rep.Supported || len(rep.Path) != 1 || rep.Path[0].Dir != ShimsDir(home) {
		t.Fatalf("report = %+v", rep)
	}
}

func TestInspectShowsWhatTheUserHasNext(t *testing.T) {
	reg, _ := useFakeReg(t)
	home := t.TempDir()
	j := mkRuntime(t, home, "rt_j", "java", "jdk/bin/java.exe", true)
	g := mkRuntime(t, home, "rt_g", "go", "go/bin/go.exe", true)
	reg.vals["GOROOT"] = fakeVal{`D:\my\go`, regSZ} // the user's own
	d := store.Data{Runtimes: []model.Runtime{j, g}, Env: model.EnvConfig{
		Path: []model.EnvPath{{ID: "envp_shims", Source: model.EnvShims, Enabled: true}, {ID: "envp_t", Source: model.EnvCustom, Dir: `C:\Tools`}},
		Vars: []model.EnvVar{
			{ID: "envv_j", Name: "JAVA_HOME", Source: model.EnvDefault, Kind: "java", Enabled: true},
			{ID: "envv_g", Name: "GOROOT", Source: model.EnvDefault, Kind: "go", Enabled: true},
			{ID: "envv_n", Name: "NODE_HOME", Source: model.EnvDefault, Kind: "node", Enabled: true},
		},
	}}
	reg.vals["Path"] = fakeVal{`C:\Tools`, regExpandSZ}
	if err := Apply(home, d); err != nil {
		t.Fatal(err)
	}
	rep := Inspect(home, d)
	if !rep.Supported || rep.ShimsDir != ShimsDir(home) || rep.Error != "" {
		t.Fatalf("report = %+v", rep)
	}
	shimsItem, tools := rep.Path[0], rep.Path[1]
	if !shimsItem.Present || !shimsItem.Owned {
		t.Errorf("the shims folder is ours: %+v", shimsItem)
	}
	if !tools.Present || tools.Owned {
		t.Errorf("a folder the user already had is present but not ours: %+v", tools)
	}
	jv, gv, nv := rep.Vars[0], rep.Vars[1], rep.Vars[2]
	if !jv.HasCurrent || !jv.Owned || jv.Current != jv.Value {
		t.Errorf("JAVA_HOME was set by Zero2Dev: %+v", jv)
	}
	if !gv.HasCurrent || gv.Owned || gv.Current != `D:\my\go` || gv.Value == gv.Current {
		t.Errorf("GOROOT is the user's, left alone: %+v", gv)
	}
	if nv.HasCurrent || nv.Problem == "" {
		t.Errorf("no node runtime: %+v", nv)
	}
}

func TestFolderCommandsSayWhatEachFolderGives(t *testing.T) {
	home := t.TempDir()
	node := mkRuntime(t, home, "rt_n", "node", "node-26/node.exe", true)
	for _, f := range []string{"npm.cmd", "npx.cmd"} {
		if err := os.WriteFile(filepath.Join(filepath.Dir(node.Binary), f), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mine := filepath.Join(home, "tools")
	if err := os.MkdirAll(filepath.Join(mine, "sub.exe"), 0o755); err != nil { // a folder, not a command
		t.Fatal(err)
	}
	for _, f := range []string{"Rg.exe", "fd.cmd", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(mine, f), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	d := store.Data{Runtimes: []model.Runtime{node}}
	got := func(p model.EnvPath) string {
		return strings.Join(folderCommands(home, d, p, fileExists, os.ReadDir), ",")
	}
	if c := got(model.EnvPath{Source: model.EnvShims}); c != "node,npm,npx" {
		t.Errorf("shims = %s", c)
	}
	if c := got(model.EnvPath{Source: model.EnvRuntime, RuntimeID: "rt_n"}); c != "node,npm,npx" {
		t.Errorf("runtime = %s", c)
	}
	if c := got(model.EnvPath{Source: model.EnvRuntime, RuntimeID: "rt_gone"}); c != "" {
		t.Errorf("gone = %s", c)
	}
	if c := got(model.EnvPath{Source: model.EnvCustom, Dir: mine}); c != "fd,rg" {
		t.Errorf("custom = %s", c)
	}
	if c := got(model.EnvPath{Source: model.EnvCustom, Dir: filepath.Join(home, "missing")}); c != "" {
		t.Errorf("missing = %s", c)
	}
}
