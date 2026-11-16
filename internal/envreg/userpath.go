package envreg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"zero2dev/internal/fsutil"
)

// The user environment logic, free of any OS call: the Windows file only provides
// the registry (see userpath_windows.go) and the tests provide an in-memory one.

// Registry value types the logic cares about.
const (
	regSZ       uint32 = 1
	regExpandSZ uint32 = 2
)

// registry is HKCU\Environment: the user's own environment variables.
type registry interface {
	// Get reads a value as stored (a REG_EXPAND_SZ keeps its %VARS%). ok is
	// false when there is no such value.
	Get(name string) (value string, typ uint32, ok bool, err error)
	Set(name, value string, typ uint32) error
	Delete(name string) error
}

// Desired is what the user's environment should hold from Zero2Dev: folders on the
// user Path (the first one ends up first) and variables (name -> value).
type Desired struct {
	Dirs []string
	Vars map[string]string
}

// Backup is <home>\shims\.registry-backup.json: what the user Path held before
// Zero2Dev changed it and which folders and variables Zero2Dev owns. It is the only
// record of what Zero2Dev may take back: without it nothing is ever removed. It is
// written before the first change and removed when Zero2Dev owns nothing any more.
type Backup struct {
	Version int        `json:"version"`
	Path    PathBackup `json:"path"`
	// Vars are the variables Zero2Dev set (name -> value). Zero2Dev only ever sets
	// a variable the user did not have, so their previous value is "absent".
	Vars map[string]string `json:"vars"`
}

// PathBackup is the user Path as it was before Zero2Dev added its first folder.
type PathBackup struct {
	Existed bool   `json:"existed"`
	Type    uint32 `json:"type"` // 1 REG_SZ, 2 REG_EXPAND_SZ
	Value   string `json:"value"`
	// Owned are the folders Zero2Dev added (and so may remove again).
	Owned []string `json:"owned"`
	// Added is the version 1 form: Zero2Dev added its one shims folder.
	Added bool `json:"added,omitempty"`
}

const backupVersion = 2

// loadBackup reads the backup; a missing or damaged file is "no backup". A version 1
// file, which could only own the shims folder, is read as owning legacyDir.
func loadBackup(file, legacyDir string) (Backup, bool) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return Backup{}, false
	}
	var b Backup
	if json.Unmarshal(raw, &b) != nil || (b.Version != 1 && b.Version != 2) {
		return Backup{}, false
	}
	if b.Version == 1 && b.Path.Added && len(b.Path.Owned) == 0 && legacyDir != "" {
		b.Path.Owned = []string{legacyDir}
	}
	b.Path.Added = false
	if b.Path.Owned == nil {
		b.Path.Owned = []string{}
	}
	if b.Vars == nil {
		b.Vars = map[string]string{}
	}
	return b, true
}

func saveBackup(file string, b Backup) error {
	b.Version = backupVersion
	if b.Vars == nil {
		b.Vars = map[string]string{}
	}
	if b.Path.Owned == nil {
		b.Path.Owned = []string{}
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(file, append(raw, '\n'), 0o644)
}

// splitPath splits a PATH value on ";" outside double quotes, keeping every
// piece (empty ones too) so that joinPath(splitPath(s)) == s.
func splitPath(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	var b strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			b.WriteRune(r)
		case r == ';' && !quoted:
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	return append(out, b.String())
}

func joinPath(entries []string) string { return strings.Join(entries, ";") }

// normEntry makes two spellings of the same folder compare equal: no quotes,
// blanks or trailing separator, one separator style, case-insensitive. %VARS%
// are not expanded: only a literal spelling counts as the same entry.
func normEntry(e string) string {
	e = strings.Trim(strings.TrimSpace(e), `"`)
	e = strings.ReplaceAll(e, "/", `\`)
	e = strings.TrimRight(e, `\`)
	return strings.ToLower(e)
}

func hasEntry(path, dir string) bool {
	want := normEntry(dir)
	for _, e := range splitPath(path) {
		if normEntry(e) == want {
			return true
		}
	}
	return false
}

// addEntry puts dir at the start of path (so its tools win), unless it is
// already somewhere in it. The rest of the value is kept exactly as it was.
func addEntry(path, dir string) (string, bool) {
	if hasEntry(path, dir) {
		return path, false
	}
	if strings.Trim(path, "; ") == "" {
		return dir, true
	}
	return dir + ";" + path, true
}

// removeEntry takes dir out of path (every spelling of it) and leaves the other
// entries untouched.
func removeEntry(path, dir string) (string, bool) {
	want := normEntry(dir)
	var kept []string
	removed := false
	for _, e := range splitPath(path) {
		if normEntry(e) == want {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	if !removed {
		return path, false
	}
	out := joinPath(kept)
	if strings.Trim(out, "; ") == "" {
		out = ""
	}
	return out, true
}

type varAction int

const (
	varKeep varAction = iota
	varSet
	varDelete
)

// decideVar decides what to do with one variable:
//   - Zero2Dev owns it when it still holds the value Zero2Dev recorded: it is then
//     kept in step with desired (updated, or deleted when there is no desired value);
//   - an absent variable gets desired and the value is recorded;
//   - anything else is the user's: never touched, and no longer recorded.
//
// It returns the action and the value to record (record == "" means none).
func decideVar(desired string, hasDesired bool, cur string, hasCur bool, recorded string, hasRecorded bool) (varAction, string) {
	if hasRecorded && hasCur && strings.EqualFold(cur, recorded) {
		switch {
		case !hasDesired:
			return varDelete, ""
		case strings.EqualFold(desired, cur):
			return varKeep, recorded
		default:
			return varSet, desired
		}
	}
	if !hasCur && hasDesired {
		return varSet, desired
	}
	return varKeep, ""
}

// regOp is one registry write with what is needed to undo it.
type regOp struct {
	name    string
	del     bool
	value   string
	typ     uint32
	prevOK  bool
	prevVal string
	prevTyp uint32
}

func (o regOp) do(reg registry) error {
	if o.del {
		return reg.Delete(o.name)
	}
	return reg.Set(o.name, o.value, o.typ)
}

func (o regOp) undo(reg registry) {
	if o.prevOK {
		_ = reg.Set(o.name, o.prevVal, o.prevTyp)
	} else {
		_ = reg.Delete(o.name)
	}
}

// run performs the writes; if one fails the ones already done are undone.
func runOps(reg registry, ops []regOp) error {
	for i, op := range ops {
		if err := op.do(reg); err != nil {
			for j := i - 1; j >= 0; j-- {
				ops[j].undo(reg)
			}
			return fmt.Errorf("cannot write %s: %w", op.name, err)
		}
	}
	return nil
}

func isString(typ uint32) bool { return typ == regSZ || typ == regExpandSZ }

func checkDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("a folder is empty")
	}
	if strings.ContainsAny(dir, `%;"`) {
		return fmt.Errorf("the folder %q has a %%, ; or \" in its name and cannot go on PATH", dir)
	}
	return nil
}

func varNames(ms ...map[string]string) []string {
	set := map[string]bool{}
	for _, m := range ms {
		for k := range m {
			set[k] = true
		}
	}
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// placeDirs works out the user Path: Zero2Dev's own folders (owned) are taken out and
// the wanted ones go back as one block, in the order wanted, where the first of
// Zero2Dev's folders was (at the start when there was none). So the order the user
// chose wins, and moving the block elsewhere in the Path by hand is kept. A wanted
// folder the user already has on the Path is theirs: it stays where it is and is not
// added again. It returns the new value (cur itself when nothing changes) and the
// folders Zero2Dev owns afterwards.
func placeDirs(cur string, ownedBefore, wanted []string) (string, []string) {
	wasOwned := map[string]bool{}
	for _, dir := range ownedBefore {
		wasOwned[normEntry(dir)] = true
	}
	insertAt := -1
	var kept []string
	for _, e := range splitPath(cur) {
		if wasOwned[normEntry(e)] {
			if insertAt < 0 {
				insertAt = len(kept)
			}
			continue
		}
		kept = append(kept, e)
	}
	if insertAt < 0 {
		insertAt = 0
	}
	userHas := map[string]bool{}
	for _, e := range kept {
		userHas[normEntry(e)] = true
	}
	var block []string
	inBlock := map[string]bool{}
	for _, dir := range wanted {
		k := normEntry(dir)
		if userHas[k] || inBlock[k] {
			continue
		}
		inBlock[k] = true
		block = append(block, dir)
	}
	removed := len(kept) < len(splitPath(cur))
	if !removed && len(block) == 0 {
		return cur, block // nothing of Zero2Dev's there and nothing wanted: leave the value as it is
	}
	if strings.Trim(joinPath(kept), "; ") == "" {
		kept, insertAt = nil, 0
	}
	entries := append(append(append([]string{}, kept[:insertAt]...), block...), kept[insertAt:]...)
	return joinPath(entries), block
}

// applyUserEnv brings the user environment in line with want, touching only what
// Zero2Dev owns:
//   - folders in want.Dirs that are not on the user Path are added at the start (the
//     value keeps its type and %VARS%) and become Zero2Dev's; a folder the user
//     already had is theirs and stays when it is no longer wanted;
//   - folders Zero2Dev added earlier that are no longer wanted leave the Path;
//   - variables follow decideVar.
//
// It is idempotent: when nothing needs to change it writes nothing and reports
// false. The backup is saved before the first registry write, and removed when
// Zero2Dev owns nothing any more. legacyDir is the folder a version 1 backup owns.
func applyUserEnv(reg registry, want Desired, backupFile, legacyDir string) (bool, error) {
	for _, dir := range want.Dirs {
		if err := checkDir(dir); err != nil {
			return false, err
		}
	}
	old, _ := loadBackup(backupFile, legacyDir)
	if old.Vars == nil {
		old.Vars = map[string]string{}
	}
	if old.Path.Owned == nil {
		old.Path.Owned = []string{}
	}
	next := Backup{Version: backupVersion, Path: PathBackup{Existed: old.Path.Existed, Type: old.Path.Type, Value: old.Path.Value, Owned: []string{}}, Vars: map[string]string{}}
	var ops []regOp

	cur, typ, ok, err := reg.Get("Path")
	if err != nil {
		return false, fmt.Errorf("cannot read the user Path: %w", err)
	}
	if ok && !isString(typ) {
		return false, fmt.Errorf("the user Path has an unexpected registry type (%d); it was left alone", typ)
	}

	np, owned := placeDirs(cur, old.Path.Owned, want.Dirs)
	if len(owned) > 0 {
		next.Path.Owned = owned
		if len(old.Path.Owned) == 0 { // the first folder: remember the Path as it was
			next.Path.Existed, next.Path.Type, next.Path.Value = ok, typ, cur
		}
	} else {
		next.Path = PathBackup{Owned: []string{}}
	}
	if np != cur {
		t := typ
		if !ok {
			t = regExpandSZ
		}
		// Taking the last folder off a Path Zero2Dev created leaves nothing worth keeping.
		ops = append(ops, regOp{name: "Path", value: np, typ: t, del: np == "" && !old.Path.Existed, prevOK: ok, prevVal: cur, prevTyp: typ})
	}

	for _, name := range varNames(want.Vars, old.Vars) {
		w, hasWant := want.Vars[name]
		hasWant = hasWant && w != ""
		v, vt, hasCur, err := reg.Get(name)
		if err != nil {
			return false, fmt.Errorf("cannot read %s: %w", name, err)
		}
		if hasCur && !isString(vt) {
			v = "" // not ours to judge: kept
		}
		rec, hasRec := old.Vars[name]
		act, record := decideVar(w, hasWant, v, hasCur, rec, hasRec)
		if record != "" {
			next.Vars[name] = record
		}
		switch act {
		case varSet:
			ops = append(ops, regOp{name: name, value: w, typ: regSZ, prevOK: hasCur, prevVal: v, prevTyp: vt})
		case varDelete:
			ops = append(ops, regOp{name: name, del: true, prevOK: hasCur, prevVal: v, prevTyp: vt})
		}
	}

	empty := len(next.Path.Owned) == 0 && len(next.Vars) == 0
	old.Version = backupVersion
	if !empty && !reflect.DeepEqual(next, old) {
		if err := saveBackup(backupFile, next); err != nil {
			return false, fmt.Errorf("cannot save the backup: %w", err)
		}
	}
	if err := runOps(reg, ops); err != nil {
		return false, err
	}
	if empty {
		if err := os.Remove(backupFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return len(ops) > 0, fmt.Errorf("cannot remove the backup: %w", err)
		}
	}
	return len(ops) > 0, nil
}
