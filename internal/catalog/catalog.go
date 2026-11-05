// Package catalog loads the list of downloadable runtimes.
package catalog

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"zero2dev/internal/model"
)

//go:embed manifest.json
var defaultManifest []byte

var hex64 = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// Load reads <home>/catalog.json if present, otherwise the embedded manifest.
func Load(home string) ([]model.CatalogEntry, error) {
	b, err := os.ReadFile(filepath.Join(home, "catalog.json"))
	if errors.Is(err, os.ErrNotExist) {
		b = defaultManifest
	} else if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse decodes and validates a manifest (a JSON array of entries).
func Parse(b []byte) ([]model.CatalogEntry, error) {
	var es []model.CatalogEntry
	if err := json.Unmarshal(b, &es); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	seen := map[string]bool{}
	for i := range es {
		if err := validate(&es[i]); err != nil {
			return nil, fmt.Errorf("catalog entry %d (%s): %w", i, es[i].ID, err)
		}
		if seen[es[i].ID] {
			return nil, fmt.Errorf("catalog: duplicate id %s", es[i].ID)
		}
		seen[es[i].ID] = true
		es[i].Installed, es[i].Supported = false, false
		es[i].FileName = FileName(es[i].URL)
	}
	return es, nil
}

// IsAllowedURL: https always; plain http only for loopback (tests/dev mirrors).
func IsAllowedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	h := u.Hostname()
	return u.Scheme == "http" && (h == "127.0.0.1" || h == "localhost" || h == "::1")
}

func validate(e *model.CatalogEntry) error {
	if e.ID == "" || strings.ContainsAny(e.ID, `/\: `) || strings.Contains(e.ID, "..") {
		return errors.New("invalid id")
	}
	if !model.In(e.Kind, model.RuntimeKinds) {
		return errors.New("invalid kind")
	}
	if !IsAllowedURL(e.URL) {
		return errors.New("url must be https")
	}
	n := strings.ToLower(path.Base(strings.Split(e.URL, "?")[0]))
	if !strings.HasSuffix(n, ".zip") && !strings.HasSuffix(n, ".tar.gz") && !strings.HasSuffix(n, ".tgz") {
		return errors.New("archive must be .zip or .tar.gz")
	}
	switch e.Checksum.Type {
	case "sha256":
		if !hex64.MatchString(e.Checksum.Value) {
			return errors.New("sha256 checksum must be 64 hex chars")
		}
	case "shasums":
		if !IsAllowedURL(e.Checksum.URL) {
			return errors.New("shasums url must be https")
		}
	default:
		return errors.New("checksum type must be sha256 or shasums")
	}
	rel := filepath.ToSlash(e.BinaryRelPath)
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") || filepath.VolumeName(e.BinaryRelPath) != "" {
		return errors.New("invalid binaryRelPath")
	}
	if e.StripComponents < 0 || e.StripComponents > 5 {
		return errors.New("invalid stripComponents")
	}
	if e.OS == "" || e.Arch == "" {
		return errors.New("os and arch are required")
	}
	return nil
}

// FileName is the archive file name from a download URL.
func FileName(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	b := path.Base(u.Path)
	if b == "." || b == "/" || b == ".." {
		return ""
	}
	return b
}

// Supported reports whether the entry matches this machine.
func Supported(e model.CatalogEntry) bool { return e.OS == runtime.GOOS && e.Arch == runtime.GOARCH }

// Find returns the entry with the given id.
func Find(es []model.CatalogEntry, id string) (model.CatalogEntry, bool) {
	for _, e := range es {
		if e.ID == id {
			return e, true
		}
	}
	return model.CatalogEntry{}, false
}

// Annotate sets Supported and Installed (an installed runtime whose binary is
// the entry's binary inside <home>/runtimes/<id>).
func Annotate(es []model.CatalogEntry, home string, rts []model.Runtime) []model.CatalogEntry {
	out := make([]model.CatalogEntry, len(es))
	for i, e := range es {
		e.Supported = Supported(e)
		e.FileName = FileName(e.URL)
		want := filepath.Join(home, "runtimes", e.ID, filepath.FromSlash(e.BinaryRelPath))
		for _, r := range rts {
			if r.Binary == want {
				e.Installed = true
			}
		}
		out[i] = e
	}
	return out
}
