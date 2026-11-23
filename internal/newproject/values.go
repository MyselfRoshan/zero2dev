package newproject

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"zero2dev/internal/frameworks"
)

// Values are what the placeholders of a framework stand for (see the package frameworks). Nothing
// is ever joined into a shell line: the expanded strings become program arguments, file contents
// or environment values as they are.
type Values struct {
	Name, Slug, Dir, Parent, Domain string
	Port                            int
	// Database values; empty when the project has no database.
	DBKind, DB, DBUser, DBPassword, DBHost string
	DBPort                                 int
	// Venv is the Python virtual environment of the project's service, Home the data folder.
	Venv, Home string

	// Rand supplies random bytes (nil: crypto/rand). Tests replace it.
	Rand func(n int) []byte
}

var (
	// A project name is shown in a page title, a Go or Python string and a JSON file, so it is
	// limited to letters, digits and a few harmless characters.
	nameRE  = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._()-]{0,63}$`)
	slugRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	passRE  = regexp.MustCompile(`^[A-Za-z0-9]{8,64}$`)
	identRE = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
)

// CheckName reports why a project name cannot be used, "" when it can.
func CheckName(name string) string {
	switch {
	case strings.TrimSpace(name) == "":
		return "Give the project a name."
	case name != strings.TrimSpace(name):
		return "The name starts or ends with a space."
	case !nameRE.MatchString(name):
		return "The name may use letters, digits, spaces and . _ - ( ) only, up to 64 characters, and starts with a letter or digit."
	}
	return ""
}

// Slugify makes the folder and host name of a project: lower case letters and digits, with a
// single - between words, at most 40 characters ("My Shop" -> "my-shop"). "" when the name has
// no ASCII letter or digit.
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	s := b.String()
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	return s
}

// reserved are names Windows does not allow for a file or folder.
var reserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}

// ReservedName reports whether slug is a Windows device name (con, nul, com1 ...).
func ReservedName(slug string) bool {
	if reserved[slug] {
		return true
	}
	return (strings.HasPrefix(slug, "com") || strings.HasPrefix(slug, "lpt")) && len(slug) == 4 && slug[3] >= '1' && slug[3] <= '9'
}

// Check validates every value that is set: no control characters anywhere, a slug of letters,
// digits, - and _, a database password of letters and digits, a name that passes CheckName.
func (v Values) Check() error {
	for k, s := range map[string]string{"name": v.Name, "slug": v.Slug, "dir": v.Dir, "parent": v.Parent, "domain": v.Domain,
		"db": v.DB, "dbUser": v.DBUser, "dbPassword": v.DBPassword, "dbHost": v.DBHost, "venv": v.Venv, "home": v.Home} {
		if strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fmt.Errorf("the value of {%s} has a control character", k)
		}
	}
	if v.Name != "" {
		if msg := CheckName(v.Name); msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}
	if v.Slug != "" && !slugRE.MatchString(v.Slug) {
		return fmt.Errorf("the slug %q may only have letters, digits, - and _", v.Slug)
	}
	if v.DBPassword != "" && !passRE.MatchString(v.DBPassword) {
		return fmt.Errorf("the database password must be 8-64 letters and digits")
	}
	for k, s := range map[string]string{"db": v.DB, "dbUser": v.DBUser} {
		if s != "" && !identRE.MatchString(s) {
			return fmt.Errorf("the value of {%s} may only have letters, digits and _", k)
		}
	}
	return nil
}

func (v Values) bytes(n int) []byte {
	if v.Rand != nil {
		return v.Rand(n)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// DBURL is the connection URL of the database: mysql://user:password@host:port/name or
// postgresql://..., "" without a database.
func (v Values) DBURL() string {
	if v.DB == "" {
		return ""
	}
	scheme := "mysql"
	if v.DBKind == "postgres" {
		scheme = "postgresql"
	}
	return fmt.Sprintf("%s://%s@%s:%d/%s", scheme, url.UserPassword(v.DBUser, v.DBPassword).String(), v.DBHost, v.DBPort, v.DB)
}

// Expand replaces the placeholders of s. An unknown placeholder is an error; a value is never
// expanded again (a name that contains "{port}" stays as typed). {appKey} and {random} give a new
// value each time they appear.
func (v Values) Expand(s string) (string, error) {
	var err error
	out := frameworks.PlaceholderRE.ReplaceAllStringFunc(s, func(m string) string {
		switch name := m[1 : len(m)-1]; name {
		case "name":
			return v.Name
		case "slug":
			return v.Slug
		case "dir":
			return v.Dir
		case "parent":
			return v.Parent
		case "port":
			if v.Port == 0 {
				return ""
			}
			return strconv.Itoa(v.Port)
		case "domain":
			return v.Domain
		case "db":
			return v.DB
		case "dbUser":
			return v.DBUser
		case "dbPassword":
			return v.DBPassword
		case "dbHost":
			return v.DBHost
		case "dbPort":
			if v.DBPort == 0 {
				return ""
			}
			return strconv.Itoa(v.DBPort)
		case "dbUrl":
			return v.DBURL()
		case "appKey":
			return base64.StdEncoding.EncodeToString(v.bytes(32))
		case "random":
			return hex.EncodeToString(v.bytes(8))
		case "venv":
			return v.Venv
		case "home":
			return v.Home
		default:
			err = fmt.Errorf("unknown placeholder %s", m)
			return m
		}
	})
	return out, err
}

// ExpandAll expands every string of list.
func (v Values) ExpandAll(list []string) ([]string, error) {
	out := make([]string, len(list))
	for i, s := range list {
		e, err := v.Expand(s)
		if err != nil {
			return nil, err
		}
		out[i] = e
	}
	return out, nil
}

// ExpandMap expands every value of m.
func (v Values) ExpandMap(m map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(m))
	for k, s := range m {
		e, err := v.Expand(s)
		if err != nil {
			return nil, err
		}
		out[k] = e
	}
	return out, nil
}

// randomPassword is 24 letters and digits (safe in a .env file, a PHP string and a SQL literal).
func randomPassword(v Values) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	b := v.bytes(24)
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = alphabet[int(c)%len(alphabet)]
	}
	return string(out)
}
