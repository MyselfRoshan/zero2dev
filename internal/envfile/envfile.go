// Package envfile edits a .env file: it sets keys or adds the ones that are missing and keeps every
// other line (comments, blank lines, order and line endings) as it was. New project uses it to put
// the database connection into the file the framework's generator wrote.
package envfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zero2dev/internal/fsutil"
)

// Line is one KEY=value to write.
type Line struct {
	Key   string
	Value string
	// IfMissing writes the line only when the file has no (uncommented) line for the key; an
	// existing value is never changed then.
	IfMissing bool
}

// Apply writes lines into the file at path (created, with its folder, when it does not exist).
// A key that is already in the file is replaced where it stands (a line that starts with
// "export " keeps that prefix); a new key is added at the end. Commented lines are not keys. It
// reports the keys it wrote.
func Apply(path string, lines []Line) (written []string, err error) {
	for _, l := range lines {
		if err := checkKey(l.Key); err != nil {
			return nil, err
		}
		if strings.ContainsAny(l.Value, "\x00\r\n") {
			return nil, fmt.Errorf("the value of %s has a NUL or a line break", l.Key)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	eol := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		eol = "\r\n"
	}
	text := string(raw)
	var out []string
	if text != "" {
		out = strings.Split(strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n")
	}
	index := map[string]int{} // key -> index of its last line in out
	for i, l := range out {
		if k, ok := keyOf(l); ok {
			index[k] = i
		}
	}
	for _, l := range lines {
		i, have := index[l.Key]
		if have && l.IfMissing {
			continue
		}
		line := l.Key + "=" + Quote(l.Value)
		if have {
			if strings.HasPrefix(strings.TrimLeft(out[i], " \t"), "export ") {
				line = "export " + line
			}
			out[i] = line
		} else {
			out = append(out, line)
			index[l.Key] = len(out) - 1
		}
		written = append(written, l.Key)
	}
	if len(written) == 0 && err == nil && len(raw) > 0 {
		return nil, nil // nothing to change: leave the file untouched
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	body := strings.Join(out, eol)
	if body != "" {
		body += eol
	}
	return written, fsutil.WriteFileAtomic(path, []byte(body), 0o644)
}

// Get returns the value of key in the file, as written (not unquoted), and whether it is there.
func Get(path, key string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	val, found := "", false
	for _, l := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if k, ok := keyOf(l); ok && k == key {
			_, v, _ := strings.Cut(l, "=")
			val, found = strings.TrimSpace(v), true
		}
	}
	return val, found
}

// keyOf returns the key of an assignment line ("KEY=...", "export KEY=...").
func keyOf(line string) (string, bool) {
	s := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(s, "#") {
		return "", false
	}
	s = strings.TrimPrefix(s, "export ")
	k, _, ok := strings.Cut(s, "=")
	k = strings.TrimSpace(k)
	if !ok || checkKey(k) != nil {
		return "", false
	}
	return k, true
}

func checkKey(k string) error {
	if k == "" {
		return fmt.Errorf("an env key is empty")
	}
	for i, r := range k {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', i > 0 && r >= '0' && r <= '9', i > 0 && (r == '.' || r == '-'):
		default:
			return fmt.Errorf("%q is not a valid env key", k)
		}
	}
	return nil
}

// Quote returns v as it is written after the "=": as it is when it only has safe characters, else
// in double quotes with \ and " escaped.
func Quote(v string) string {
	safe := true
	for _, r := range v {
		if !(r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '@' || r == '+' || r == ',' || r == '=' || r == '%' || r == '~' ||
			r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			safe = false
			break
		}
	}
	if safe {
		return v
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
}
