package envreg

import (
	"errors"
	"testing"
)

// fakeReg is an in-memory HKCU\Environment: no test touches the real registry.
type fakeReg struct {
	vals    map[string]fakeVal
	writes  int
	failSet string // Set of this name fails
}

type fakeVal struct {
	v   string
	typ uint32
}

func newFake() *fakeReg { return &fakeReg{vals: map[string]fakeVal{}} }

func (f *fakeReg) Get(name string) (string, uint32, bool, error) {
	v, ok := f.vals[name]
	return v.v, v.typ, ok, nil
}

func (f *fakeReg) Set(name, value string, typ uint32) error {
	if name == f.failSet {
		return errors.New("access denied")
	}
	f.writes++
	f.vals[name] = fakeVal{value, typ}
	return nil
}

func (f *fakeReg) Delete(name string) error {
	f.writes++
	delete(f.vals, name)
	return nil
}

const shims = `C:\z\shims`

func TestSplitAndJoinKeepTheValueExactly(t *testing.T) {
	for _, s := range []string{
		"", `C:\a`, `C:\a;C:\b`, `C:\a;;C:\b;`, `%SystemRoot%\system32;"C:\Program Files;x\bin";C:\c`,
	} {
		if got := joinPath(splitPath(s)); got != s {
			t.Errorf("roundtrip %q = %q", s, got)
		}
	}
	if got := splitPath(`"C:\a;b";C:\c`); len(got) != 2 {
		t.Errorf("a ; inside quotes does not split: %q", got)
	}
}

func TestAddEntryGoesFirstOnceAndKeepsVars(t *testing.T) {
	path := `%USERPROFILE%\bin;C:\Windows;`
	got, added := addEntry(path, shims)
	if !added || got != shims+";"+path {
		t.Fatalf("got %q %v", got, added)
	}
	for _, same := range []string{shims, `c:\Z\SHIMS\`, `"C:\z\shims"`, `C:/z/shims`} {
		if _, added := addEntry(`C:\x;`+same+`;C:\y`, shims); added {
			t.Errorf("%q counted as absent", same)
		}
	}
	if got, added := addEntry("", shims); !added || got != shims {
		t.Errorf("empty path: %q", got)
	}
	if got, added := addEntry(" ;", shims); !added || got != shims {
		t.Errorf("blank path: %q", got)
	}
	if _, added := addEntry(`C:\z\shims2`, shims); !added {
		t.Error("a folder with a longer name is not ours")
	}
}

func TestRemoveEntryOnlyTakesOurOwn(t *testing.T) {
	path := `%USERPROFILE%\bin;` + shims + `;C:\Windows;`
	got, removed := removeEntry(path, shims)
	if !removed || got != `%USERPROFILE%\bin;C:\Windows;` {
		t.Fatalf("got %q %v", got, removed)
	}
	if got, removed := removeEntry(`C:\a;C:\b`, shims); removed || got != `C:\a;C:\b` {
		t.Fatalf("nothing to remove: %q", got)
	}
	if got, _ := removeEntry(shims, shims); got != "" {
		t.Fatalf("only ours: %q", got)
	}
	orig := `C:\a;%X%\b;`
	added, _ := addEntry(orig, shims)
	if back, _ := removeEntry(added, shims); back != orig {
		t.Fatalf("add then remove must restore: %q", back)
	}
}

func TestDecideVar(t *testing.T) {
	const a, b = `C:\jdk17`, `C:\jdk21`
	cases := []struct {
		name       string
		desired    string
		hasDesired bool
		cur        string
		hasCur     bool
		rec        string
		hasRec     bool
		wantAct    varAction
		wantRecord string
	}{
		{"absent gets set", a, true, "", false, "", false, varSet, a},
		{"user value kept", a, true, `D:\mine`, true, "", false, varKeep, ""},
		{"ours is up to date", a, true, a, true, a, true, varKeep, a},
		{"ours follows Make default", b, true, a, true, a, true, varSet, b},
		{"ours removed when no runtime", "", false, a, true, a, true, varDelete, ""},
		{"ours case-insensitively", `c:\JDK17`, true, a, true, a, true, varKeep, a},
		{"user changed it since: leave", b, true, `D:\mine`, true, a, true, varKeep, ""},
		{"user deleted it: set again", b, true, "", false, a, true, varSet, b},
		{"nothing wanted, nothing there", "", false, "", false, "", false, varKeep, ""},
		{"nothing wanted, user value", "", false, `D:\mine`, true, "", false, varKeep, ""},
	}
	for _, c := range cases {
		act, rec := decideVar(c.desired, c.hasDesired, c.cur, c.hasCur, c.rec, c.hasRec)
		if act != c.wantAct || rec != c.wantRecord {
			t.Errorf("%s: got (%d, %q), want (%d, %q)", c.name, act, rec, c.wantAct, c.wantRecord)
		}
	}
}
