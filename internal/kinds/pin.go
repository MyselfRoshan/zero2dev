package kinds

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// NodePin is the Node version a project folder asks for.
type NodePin struct {
	Spec   string // as written: "22.18.0", ">=22", "^22.18.0 || ^24.11.0"
	Source string // where: ".node-version", "devEngines.runtime", "engines.node", ".nvmrc"
}

// ReadNodePin reads what the folder pins, in the order Vite+ looks: .node-version,
// package.json devEngines.runtime, package.json engines.node, .nvmrc. ok is false when none
// of them is there.
func ReadNodePin(dir string) (NodePin, bool) {
	if dir == "" {
		return NodePin{}, false
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(b) > 1<<20 {
			return ""
		}
		return string(b)
	}
	firstLine := func(s string) string {
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				return l
			}
		}
		return ""
	}
	if v := firstLine(read(".node-version")); v != "" {
		return NodePin{v, ".node-version"}, true
	}
	var pkg struct {
		DevEngines struct {
			Runtime json.RawMessage `json:"runtime"`
		} `json:"devEngines"`
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if raw := read("package.json"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &pkg)
		// devEngines.runtime is one object or a list of them: the one named node counts.
		type rt struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		var many []rt
		var one rt
		if json.Unmarshal(pkg.DevEngines.Runtime, &many) != nil {
			if json.Unmarshal(pkg.DevEngines.Runtime, &one) == nil {
				many = []rt{one}
			}
		}
		for _, r := range many {
			if strings.EqualFold(r.Name, "node") && strings.TrimSpace(r.Version) != "" {
				return NodePin{strings.TrimSpace(r.Version), "devEngines.runtime"}, true
			}
		}
		if v := strings.TrimSpace(pkg.Engines.Node); v != "" {
			return NodePin{v, "engines.node"}, true
		}
	}
	if v := firstLine(read(".nvmrc")); v != "" {
		return NodePin{v, ".nvmrc"}, true
	}
	return NodePin{}, false
}

// semver is a version, or a partial one (22, 22.1): n is how many numbers were given.
type semver struct {
	n       int
	v       [3]int
	invalid bool
}

func parsePartial(s string) semver {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "v"), "=")
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i] // 22.1.0-rc.1, 22.1.0+build
	}
	var out semver
	if s == "" {
		out.invalid = true
		return out
	}
	for i, part := range strings.Split(s, ".") {
		if i > 2 {
			out.invalid = true
			return out
		}
		if part == "x" || part == "X" || part == "*" {
			return out // the rest is a wildcard
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			out.invalid = true
			return out
		}
		out.v[i], out.n = n, i+1
	}
	return out
}

func cmpSemver(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// bump is the first version above everything that starts with the first n numbers of v.
func bump(v [3]int, n int) [3]int {
	switch {
	case n <= 0:
		return [3]int{1 << 30, 0, 0}
	case n == 1:
		return [3]int{v[0] + 1, 0, 0}
	case n == 2:
		return [3]int{v[0], v[1] + 1, 0}
	}
	return [3]int{v[0], v[1], v[2] + 1}
}

// comparator checks one term ("^22.18.0", ">=22", "22.x") against version h. ok is false when the
// term is not understood.
func comparator(term string, h [3]int) (match, ok bool) {
	op := ""
	for _, o := range []string{">=", "<=", ">", "<", "^", "~", "="} {
		if strings.HasPrefix(term, o) {
			op, term = o, strings.TrimSpace(term[len(o):])
			break
		}
	}
	p := parsePartial(term)
	if p.invalid {
		return false, false
	}
	lo := p.v // missing numbers are 0
	switch op {
	case ">=":
		return cmpSemver(h, lo) >= 0, true
	case ">":
		return cmpSemver(h, bump(p.v, p.n)) >= 0, true
	case "<":
		return cmpSemver(h, lo) < 0, true
	case "<=":
		return cmpSemver(h, bump(p.v, p.n)) < 0, true
	case "~":
		n := min(max(p.n, 1), 2)
		if p.n == 0 {
			return true, true
		}
		return cmpSemver(h, lo) >= 0 && cmpSemver(h, bump(p.v, n)) < 0, true
	case "^":
		if p.n == 0 {
			return true, true
		}
		// Changes that keep the leftmost non-zero number are allowed.
		upto := 1
		for upto < 3 && upto < p.n && p.v[upto-1] == 0 {
			upto++
		}
		if p.v[0] == 0 && p.n == 1 {
			upto = 1
		}
		return cmpSemver(h, lo) >= 0 && cmpSemver(h, bump(p.v, upto)) < 0, true
	}
	// no operator (or =): the numbers given, the rest any
	if p.n == 0 {
		return true, true
	}
	return cmpSemver(h, lo) >= 0 && cmpSemver(h, bump(p.v, p.n)) < 0, true
}

// SatisfiesNode reports whether the Node version have meets the pinned spec: exact (22.18.0, v22),
// partial (22, 22.x, 22.1), comparators (>=22, >=20 <25), ^ and ~ ranges and "||" alternatives.
// known is false when the spec is something this does not understand (lts/*, a hyphen range, a
// tag): then nothing is said about it.
func SatisfiesNode(spec, have string) (ok, known bool) {
	h := parsePartial(have)
	if h.invalid || h.n == 0 || strings.TrimSpace(spec) == "" {
		return true, false
	}
	any := false
	for _, alt := range strings.Split(spec, "||") {
		terms := strings.Fields(alt)
		// ">= 22" written with a space: join an operator that stands alone with the next word.
		var joined []string
		for i := 0; i < len(terms); i++ {
			if strings.Trim(terms[i], "<>=^~") == "" && i+1 < len(terms) {
				terms[i+1] = terms[i] + terms[i+1]
				continue
			}
			joined = append(joined, terms[i])
		}
		if len(joined) == 0 {
			return true, false
		}
		all := true
		for _, t := range joined {
			if t == "-" {
				return true, false // a hyphen range
			}
			m, understood := comparator(t, h.v)
			if !understood {
				return true, false
			}
			all = all && m
		}
		any = any || all
	}
	return any, true
}
