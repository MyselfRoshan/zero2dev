package supervisor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"zero2dev/internal/apperr"
	"zero2dev/internal/fsutil"
)

// maxEnvFile is the largest .env file that is read (a mistake guard, not a limit anyone hits).
const maxEnvFile = 1 << 20

// envVar is one KEY=VALUE pair.
type envVar struct{ Key, Value string }

// parseEnvFile reads the .env format:
//
//	KEY=VALUE            a plain value; an inline comment starts at whitespace + #
//	export KEY=VALUE     the export prefix is ignored
//	KEY="a b\n\"c\""     double quotes: \n \r \t \" \\ are escapes, other backslashes stay
//	KEY='a b # c'        single quotes: literal, no escapes
//	# comment, blank lines
//
// Quoted values may span lines. There is no variable expansion ($X stays $X).
// A key that is set twice keeps the last value. Lines that cannot be read are
// skipped and reported in problems (line numbers only, never values).
func parseEnvFile(data []byte) (vars []envVar, problems []string) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	index := map[string]int{}
	set := func(k, v string) {
		if i, ok := index[k]; ok {
			vars[i].Value = v
			return
		}
		index[k] = len(vars)
		vars = append(vars, envVar{k, v})
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimLeft(lines[i], " \t")
		if line == "" || line[0] == '#' {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			line = strings.TrimLeft(rest, " \t")
		}
		key, val, ok := strings.Cut(line, "=")
		key = strings.TrimRight(key, " \t")
		if !ok || !validEnvKey(key) {
			problems = append(problems, fmt.Sprintf("line %d is not KEY=VALUE and was skipped", i+1))
			continue
		}
		rest := strings.TrimLeft(val, " \t")
		spaced := len(rest) != len(val)
		switch {
		case rest != "" && (rest[0] == '"' || rest[0] == '\''):
			v, endLine, closed := readQuoted(lines, i, rest)
			if !closed {
				problems = append(problems, fmt.Sprintf("line %d: the quote is never closed, the rest of the line was used", i+1))
				v = strings.TrimSpace(rest[1:])
			} else {
				i = endLine
			}
			set(key, v)
		default:
			if rest != "" && rest[0] == '#' && spaced {
				rest = "" // KEY= # comment
			}
			set(key, trimInlineComment(rest))
		}
	}
	return vars, problems
}

// validEnvKey: letters, digits, _ . - and not starting with a digit.
func validEnvKey(k string) bool {
	if k == "" {
		return false
	}
	for i, c := range k {
		switch {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '.' || c == '-'):
		default:
			return false
		}
	}
	return true
}

// trimInlineComment cuts an unquoted value at the first whitespace+# and trims it.
func trimInlineComment(v string) string {
	for i := 1; i < len(v); i++ {
		if v[i] == '#' && (v[i-1] == ' ' || v[i-1] == '\t') {
			v = v[:i-1]
			break
		}
	}
	return strings.TrimRight(v, " \t")
}

// readQuoted reads the quoted value starting at rest (rest[0] is the quote),
// which is the text of lines[at] after the "=". It returns the value, the
// index of the line the closing quote is on, and whether it was closed.
func readQuoted(lines []string, at int, rest string) (string, int, bool) {
	q := rest[0]
	var b strings.Builder
	cur := rest[1:]
	for n := at; ; {
		for i := 0; i < len(cur); i++ {
			c := cur[i]
			switch {
			case c == q:
				return b.String(), n, true // whatever follows (a comment) is ignored
			case c == '\\' && q == '"' && i+1 < len(cur):
				i++
				switch cur[i] {
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				case '"', '\\':
					b.WriteByte(cur[i])
				default:
					b.WriteByte('\\')
					b.WriteByte(cur[i])
				}
			default:
				b.WriteByte(c)
			}
		}
		n++
		if n >= len(lines) {
			return "", at, false
		}
		b.WriteByte('\n')
		cur = lines[n]
	}
}

// envFilePath is the file EnvFile names, inside cwd. It refuses an absolute
// path or one that leaves cwd (the API already checks this; this is the second guard).
func envFilePath(cwd, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || fsutil.HasWindowsDrivePrefix(name) || strings.HasPrefix(name, `\`) || strings.HasPrefix(clean, string(filepath.Separator)) ||
		clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", apperr.BadRequest("the .env file must be a path inside the service folder")
	}
	if cwd == "" {
		return "", nil
	}
	return filepath.Join(cwd, clean), nil
}

// loadEnvFile reads the service's .env file. A file that is missing or
// unreadable is only a warning (the service still starts); a path that escapes
// the folder is an error. vars is nil when there is nothing to add.
func loadEnvFile(cwd, name string) (vars []envVar, warnings []string, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil, nil
	}
	path, err := envFilePath(cwd, name)
	if err != nil {
		return nil, nil, err
	}
	if path == "" {
		return nil, []string{fmt.Sprintf(".env file %s is not used: the service has no folder", name)}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, []string{fmt.Sprintf(".env file %s not found, no variables were added from it", path)}, nil
		}
		return nil, []string{fmt.Sprintf("cannot read .env file %s: %v", path, err)}, nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxEnvFile+1))
	if err != nil {
		return nil, []string{fmt.Sprintf("cannot read .env file %s: %v", path, err)}, nil
	}
	if len(data) > maxEnvFile {
		return nil, []string{fmt.Sprintf(".env file %s is larger than 1 MB and was not used", path)}, nil
	}
	vars, problems := parseEnvFile(data)
	for _, p := range problems {
		warnings = append(warnings, ".env "+p)
	}
	return vars, warnings, nil
}
