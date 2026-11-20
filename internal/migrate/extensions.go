package migrate

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// rewriteExtensions points the entries of VS Code's extensions.json (in dir,
// the extensions folder) at newDir. VS Code keeps each extension's absolute
// location there (fsPath, path, external); an entry is rewritten when it has
// a relativeLocation or when its location is inside oldDir, others (an
// extension installed from somewhere else) are left alone. A missing or
// unreadable file is left as it is: VS Code rebuilds it by scanning.
func rewriteExtensions(dir, oldDir, newDir string) error {
	file := filepath.Join(dir, "extensions.json")
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil
	}
	changed := false
	for _, e := range entries {
		rel, _ := e["relativeLocation"].(string)
		if rel == "" {
			old := locationPath(e["location"])
			if old == "" || !inside(oldDir, old) {
				continue
			}
			r, err := filepath.Rel(strings.ToLower(filepath.Clean(oldDir)), strings.ToLower(filepath.Clean(old)))
			if err != nil || r == "." {
				continue
			}
			rel = filepath.Base(old)
		}
		p := filepath.Join(newDir, filepath.FromSlash(rel))
		switch loc := e["location"].(type) {
		case map[string]any:
			loc["fsPath"] = fsPath(p)
			loc["path"] = uriPath(p)
			loc["scheme"] = "file"
			if _, ok := loc["external"]; ok {
				loc["external"] = fileURI(p)
			}
		case string:
			e["location"] = fileURI(p)
		default:
			continue
		}
		changed = true
	}
	if !changed {
		return nil
	}
	out, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return os.WriteFile(file, out, 0o644)
}

// locationPath is the local path of an entry's location.
func locationPath(loc any) string {
	switch l := loc.(type) {
	case map[string]any:
		if s, _ := l["fsPath"].(string); s != "" {
			return filepath.FromSlash(s)
		}
		if s, _ := l["path"].(string); s != "" {
			return pathFromURI(s)
		}
	case string:
		if u, err := url.Parse(l); err == nil && u.Scheme == "file" {
			return pathFromURI(u.Path)
		}
	}
	return ""
}

// pathFromURI turns "/c:/Users/me/x" into "c:\Users\me\x".
func pathFromURI(p string) string {
	if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// fsPath is how VS Code writes a path: the drive letter in lower case.
func fsPath(p string) string {
	if len(p) >= 2 && p[1] == ':' {
		p = strings.ToLower(p[:1]) + p[1:]
	}
	return p
}

// uriPath is the path part of a file URI: "/c:/Users/me/x".
func uriPath(p string) string {
	s := filepath.ToSlash(fsPath(p))
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

// fileURI is VS Code's encoded form: "file:///c%3A/Users/me/x".
func fileURI(p string) string {
	parts := strings.Split(uriPath(p), "/")
	for i, s := range parts {
		parts[i] = strings.ReplaceAll(url.PathEscape(s), ":", "%3A")
	}
	return "file://" + strings.Join(parts, "/")
}
