package envreg

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func backupPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "shims", ".registry-backup.json")
}

func apply(reg *fakeReg, bk string, dirs []string, vars map[string]string) (bool, error) {
	return applyUserEnv(reg, Desired{Dirs: dirs, Vars: vars}, bk, shims)
}

func TestApplyAddsPathAndVarsKeepingTheType(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	reg.vals["Path"] = fakeVal{`%USERPROFILE%\bin;C:\Tools`, regExpandSZ}
	vars := map[string]string{"JAVA_HOME": `C:\z\jdk`, "GOROOT": `C:\z\go`}
	changed, err := apply(reg, bk, []string{shims}, vars)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got := reg.vals["Path"]; got.v != shims+`;%USERPROFILE%\bin;C:\Tools` || got.typ != regExpandSZ {
		t.Fatalf("Path = %+v", got)
	}
	if reg.vals["JAVA_HOME"].v != `C:\z\jdk` || reg.vals["GOROOT"].v != `C:\z\go` {
		t.Fatalf("vars = %+v", reg.vals)
	}
	b, ok := loadBackup(bk, shims)
	if !ok || b.Path.Value != `%USERPROFILE%\bin;C:\Tools` || b.Path.Type != regExpandSZ || !b.Path.Existed ||
		!reflect.DeepEqual(b.Path.Owned, []string{shims}) || !reflect.DeepEqual(b.Vars, vars) {
		t.Fatalf("backup = %+v", b)
	}
}

func TestApplyKeepsARegSZPathAndCreatesAMissingOne(t *testing.T) {
	reg := newFake()
	reg.vals["Path"] = fakeVal{`C:\Tools`, regSZ}
	if _, err := apply(reg, backupPath(t), []string{shims}, nil); err != nil {
		t.Fatal(err)
	}
	if got := reg.vals["Path"]; got.typ != regSZ || got.v != shims+`;C:\Tools` {
		t.Fatalf("Path = %+v", got)
	}
	empty := newFake()
	if _, err := apply(empty, backupPath(t), []string{shims}, nil); err != nil {
		t.Fatal(err)
	}
	if got := empty.vals["Path"]; got.typ != regExpandSZ || got.v != shims {
		t.Fatalf("new Path = %+v", got)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	reg.vals["Path"] = fakeVal{`C:\Tools`, regExpandSZ}
	vars := map[string]string{"JAVA_HOME": `C:\z\jdk`}
	if _, err := apply(reg, bk, []string{shims}, vars); err != nil {
		t.Fatal(err)
	}
	writes := reg.writes
	before, _ := os.ReadFile(bk)
	changed, err := apply(reg, bk, []string{shims}, vars)
	if err != nil || changed || reg.writes != writes {
		t.Fatalf("second apply: changed=%v err=%v writes %d -> %d", changed, err, writes, reg.writes)
	}
	if after, _ := os.ReadFile(bk); string(after) != string(before) {
		t.Fatal("the backup changed on a no-op")
	}
}

func TestApplyDoesNotTouchVariablesTheUserHas(t *testing.T) {
	reg := newFake()
	reg.vals["JAVA_HOME"] = fakeVal{`D:\my\jdk`, regSZ}
	if _, err := apply(reg, backupPath(t), nil, map[string]string{"JAVA_HOME": `C:\z\jdk`, "GOROOT": `C:\z\go`}); err != nil {
		t.Fatal(err)
	}
	if reg.vals["JAVA_HOME"].v != `D:\my\jdk` {
		t.Fatalf("JAVA_HOME was overwritten: %+v", reg.vals["JAVA_HOME"])
	}
	if reg.vals["GOROOT"].v != `C:\z\go` {
		t.Fatalf("an absent GOROOT must be set: %+v", reg.vals)
	}
}

func TestApplyFollowsMakeDefaultOnlyForOurOwnValue(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	if _, err := apply(reg, bk, nil, map[string]string{"JAVA_HOME": `C:\z\jdk17`}); err != nil {
		t.Fatal(err)
	}
	changed, err := apply(reg, bk, nil, map[string]string{"JAVA_HOME": `C:\z\jdk21`})
	if err != nil || !changed || reg.vals["JAVA_HOME"].v != `C:\z\jdk21` {
		t.Fatalf("make default: changed=%v err=%v val=%+v", changed, err, reg.vals["JAVA_HOME"])
	}
	// The user takes the variable over: Zero2Dev stops following it.
	reg.vals["JAVA_HOME"] = fakeVal{`D:\mine`, regSZ}
	changed, err = apply(reg, bk, nil, map[string]string{"JAVA_HOME": `C:\z\jdk17`})
	if err != nil || changed || reg.vals["JAVA_HOME"].v != `D:\mine` {
		t.Fatalf("user value: changed=%v err=%v val=%+v", changed, err, reg.vals["JAVA_HOME"])
	}
	if _, err := os.Stat(bk); !os.IsNotExist(err) {
		t.Fatalf("Zero2Dev owns nothing any more, so no backup stays: %v", err)
	}
	// And switching everything off leaves it.
	if _, err := apply(reg, bk, nil, nil); err != nil || reg.vals["JAVA_HOME"].v != `D:\mine` {
		t.Fatalf("off: err=%v val=%+v", err, reg.vals["JAVA_HOME"])
	}
}

func TestApplyDropsOurVariableWhenItIsNoLongerWanted(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	if _, err := apply(reg, bk, nil, map[string]string{"GOROOT": `C:\z\go`, "NODE_HOME": `C:\z\node`}); err != nil {
		t.Fatal(err)
	}
	// only GOROOT is switched off: NODE_HOME stays
	if _, err := apply(reg, bk, nil, map[string]string{"NODE_HOME": `C:\z\node`}); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.vals["GOROOT"]; ok {
		t.Fatal("a switched-off variable must go")
	}
	if reg.vals["NODE_HOME"].v != `C:\z\node` {
		t.Fatal("the others stay")
	}
	if b, _ := loadBackup(bk, shims); len(b.Vars) != 1 {
		t.Fatalf("backup = %+v", b)
	}
}

func TestApplyRefusesWhatItCannotDoSafely(t *testing.T) {
	reg := newFake()
	reg.vals["Path"] = fakeVal{"x", 7} // REG_MULTI_SZ
	if _, err := apply(reg, backupPath(t), []string{shims}, nil); err == nil {
		t.Fatal("an unexpected Path type must be an error")
	}
	if reg.writes != 0 {
		t.Fatal("nothing may be written")
	}
	for _, dir := range []string{"", `C:\50%\shims`, `C:\a;b`} {
		if _, err := apply(newFake(), backupPath(t), []string{dir}, nil); err == nil {
			t.Errorf("folder %q must be refused", dir)
		}
	}
}

func TestApplyUndoesEarlierWritesWhenOneFails(t *testing.T) {
	reg := newFake()
	reg.vals["Path"] = fakeVal{`C:\Tools`, regExpandSZ}
	reg.failSet = "JAVA_HOME"
	if _, err := apply(reg, backupPath(t), []string{shims}, map[string]string{"JAVA_HOME": `C:\z\jdk`}); err == nil {
		t.Fatal("want an error")
	}
	if got := reg.vals["Path"]; got.v != `C:\Tools` || got.typ != regExpandSZ {
		t.Fatalf("Path not restored: %+v", got)
	}
}

// Each folder is chosen on its own: the first wanted one ends up first, and turning
// one off takes only that one away.
func TestApplyManyFoldersOneByOne(t *testing.T) {
	const node, jdk = `C:\z\node-26`, `C:\z\jdk-21\bin`
	reg, bk := newFake(), backupPath(t)
	reg.vals["Path"] = fakeVal{`C:\Tools`, regExpandSZ}
	if _, err := apply(reg, bk, []string{node, jdk, shims}, nil); err != nil {
		t.Fatal(err)
	}
	if got := reg.vals["Path"].v; got != node+";"+jdk+";"+shims+`;C:\Tools` {
		t.Fatalf("Path = %q", got)
	}
	// the Java folder goes off, the others stay
	if _, err := apply(reg, bk, []string{node, shims}, nil); err != nil {
		t.Fatal(err)
	}
	if got := reg.vals["Path"].v; got != node+";"+shims+`;C:\Tools` {
		t.Fatalf("Path = %q", got)
	}
	if b, _ := loadBackup(bk, shims); !reflect.DeepEqual(b.Path.Owned, []string{node, shims}) || b.Path.Value != `C:\Tools` {
		t.Fatalf("backup = %+v", b)
	}
	// everything off: only ours leave, and the file goes
	if _, err := apply(reg, bk, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := reg.vals["Path"].v; got != `C:\Tools` {
		t.Fatalf("Path = %q", got)
	}
	if _, err := os.Stat(bk); !os.IsNotExist(err) {
		t.Fatal("the backup must be removed")
	}
}

// The order of the list is the order on PATH, so the user decides which node wins; a
// block the user moved by hand stays where they put it.
func TestApplyFollowsTheChosenOrder(t *testing.T) {
	const a, b = `C:\z\node-22`, `C:\z\node-26`
	reg, bk := newFake(), backupPath(t)
	reg.vals["Path"] = fakeVal{`C:\Tools`, regExpandSZ}
	if _, err := apply(reg, bk, []string{a, b, shims}, nil); err != nil {
		t.Fatal(err)
	}
	changed, err := apply(reg, bk, []string{b, a, shims}, nil)
	if err != nil || !changed {
		t.Fatalf("reorder: changed=%v err=%v", changed, err)
	}
	if got := reg.vals["Path"].v; got != b+";"+a+";"+shims+`;C:\Tools` {
		t.Fatalf("Path = %q", got)
	}
	// unchanged order: no write
	writes := reg.writes
	if changed, err := apply(reg, bk, []string{b, a, shims}, nil); err != nil || changed || reg.writes != writes {
		t.Fatalf("same order: changed=%v err=%v", changed, err)
	}
	// the user moved Zero2Dev's folders after their own: the block stays there
	reg.vals["Path"] = fakeVal{`C:\Tools;` + b + ";" + a + ";" + shims, regExpandSZ}
	if _, err := apply(reg, bk, []string{a, b, shims}, nil); err != nil {
		t.Fatal(err)
	}
	if got := reg.vals["Path"].v; got != `C:\Tools;`+a+";"+b+";"+shims {
		t.Fatalf("Path = %q", got)
	}
}

func TestApplyLeavesAnOddPathAloneWhenNothingIsOurs(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	reg.vals["Path"] = fakeVal{` ;`, regExpandSZ}
	if changed, err := apply(reg, bk, nil, nil); err != nil || changed || reg.writes != 0 {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if _, err := apply(reg, bk, []string{shims}, nil); err != nil || reg.vals["Path"].v != shims {
		t.Fatalf("a blank Path becomes just our folder: %+v %v", reg.vals["Path"], err)
	}
}

func TestApplyNeverTakesAFolderTheUserHad(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	reg.vals["Path"] = fakeVal{`C:\mine;C:\Tools`, regExpandSZ}
	if _, err := apply(reg, bk, []string{`c:\MINE`, shims}, nil); err != nil {
		t.Fatal(err)
	}
	if got := reg.vals["Path"].v; got != shims+`;C:\mine;C:\Tools` {
		t.Fatalf("an existing folder is not added twice: %q", got)
	}
	if _, err := apply(reg, bk, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := reg.vals["Path"].v; got != `C:\mine;C:\Tools` {
		t.Fatalf("the user's own folder must stay: %q", got)
	}
}

func TestSwitchingEverythingOffRestoresWhatWasThereAndKeepsLaterChanges(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	orig := `%USERPROFILE%\bin;C:\Tools`
	reg.vals["Path"] = fakeVal{orig, regExpandSZ}
	vars := map[string]string{"JAVA_HOME": `C:\z\jdk`}
	if _, err := apply(reg, bk, []string{shims}, vars); err != nil {
		t.Fatal(err)
	}
	// The user (or an installer) appends something meanwhile.
	p := reg.vals["Path"]
	reg.vals["Path"] = fakeVal{p.v + `;C:\Later`, p.typ}
	changed, err := apply(reg, bk, nil, nil)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if got := reg.vals["Path"]; got.v != orig+`;C:\Later` || got.typ != regExpandSZ {
		t.Fatalf("Path = %+v", got)
	}
	if _, ok := reg.vals["JAVA_HOME"]; ok {
		t.Fatal("JAVA_HOME must be removed")
	}
	if _, err := os.Stat(bk); !os.IsNotExist(err) {
		t.Fatal("the backup must be removed")
	}
	// Doing it again is a no-op.
	writes := reg.writes
	if changed, err := apply(reg, bk, nil, nil); err != nil || changed || reg.writes != writes {
		t.Fatalf("second off: changed=%v err=%v", changed, err)
	}
}

func TestSwitchingOffDeletesAPathZero2DevCreated(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	if _, err := apply(reg, bk, []string{shims}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := apply(reg, bk, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.vals["Path"]; ok {
		t.Fatalf("the Path value did not exist before: %+v", reg.vals["Path"])
	}
}

// Ownership lives in the backup alone: without a usable one Zero2Dev takes nothing back,
// whatever the user environment looks like.
func TestWithoutABackupNothingIsTakenBack(t *testing.T) {
	for _, content := range []string{"", "{not json", `{"version":9}`} {
		reg, bk := newFake(), backupPath(t)
		reg.vals["Path"] = fakeVal{shims + `;C:\Tools`, regExpandSZ}
		reg.vals["GOROOT"] = fakeVal{`C:\z\go`, regSZ}
		if content != "" {
			if err := os.MkdirAll(filepath.Dir(bk), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(bk, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if changed, err := apply(reg, bk, nil, nil); err != nil || changed {
			t.Fatalf("%q: changed=%v err=%v", content, changed, err)
		}
		if reg.vals["Path"].v != shims+`;C:\Tools` || reg.vals["GOROOT"].v != `C:\z\go` {
			t.Errorf("%q: the user environment changed: %+v", content, reg.vals)
		}
	}
}

func TestApplyWithACorruptBackupStillWorks(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	if err := os.MkdirAll(filepath.Dir(bk), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bk, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg.vals["Path"] = fakeVal{`C:\Tools`, regExpandSZ}
	if _, err := apply(reg, bk, nil, map[string]string{"GOROOT": `C:\z\go`}); err != nil {
		t.Fatal(err)
	}
	if b, ok := loadBackup(bk, shims); !ok || b.Vars["GOROOT"] != `C:\z\go` {
		t.Fatalf("the backup must be rewritten: %+v %v", b, ok)
	}
}

// A 0.0.4 development build wrote version 1 backups that owned the shims folder only.
func TestAVersion1BackupStillOwnsTheShimsFolder(t *testing.T) {
	reg, bk := newFake(), backupPath(t)
	reg.vals["Path"] = fakeVal{shims + `;C:\Tools`, regExpandSZ}
	reg.vals["JAVA_HOME"] = fakeVal{`C:\z\jdk`, regSZ}
	if err := os.MkdirAll(filepath.Dir(bk), 0o755); err != nil {
		t.Fatal(err)
	}
	v1 := `{"version":1,"path":{"existed":true,"type":2,"value":"C:\\Tools","added":true},"vars":{"JAVA_HOME":"C:\\z\\jdk"}}`
	if err := os.WriteFile(bk, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := apply(reg, bk, nil, nil); err != nil {
		t.Fatal(err)
	}
	if reg.vals["Path"].v != `C:\Tools` {
		t.Fatalf("Path = %+v", reg.vals["Path"])
	}
	if _, ok := reg.vals["JAVA_HOME"]; ok {
		t.Fatal("JAVA_HOME was ours")
	}
}
