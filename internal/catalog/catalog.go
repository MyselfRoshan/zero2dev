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
	"sort"
	"strconv"
	"strings"

	"zero2dev/internal/model"
)

//go:embed manifest.json
var defaultManifest []byte

var (
	hex64 = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	hex32 = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

// Load reads <home>/catalog.json if present, otherwise the embedded manifest.
func Load(home string) ([]model.CatalogEntry, error) {
	b, err := os.ReadFile(filepath.Join(home, "catalog.json"))
	if errors.Is(err, os.ErrNotExist) {
		b = defaultManifest
	} else if err != nil {
		return nil, err
	}
	es, err := Parse(b)
	if err != nil {
		return nil, err
	}
	return mergeRemote(es, loadCache(home).Entries), nil
}

// mergeRemote appends valid cached entries and fills missing manifest metadata.
// The manifest retains precedence on an id clash.
func mergeRemote(es, remote []model.CatalogEntry) []model.CatalogEntry {
	seen := map[string]int{}
	for i, e := range es {
		seen[e.ID] = i
	}
	manifestLen := len(es)
	for i := range remote {
		e := remote[i]
		if validate(&e) != nil {
			continue
		}
		if index, ok := seen[e.ID]; ok {
			if index < manifestLen {
				if es[index].Line == "" {
					es[index].Line = e.Line
				}
				if es[index].Channel == "" {
					es[index].Channel = e.Channel
				}
			}
			continue
		}
		e.Source = "remote"
		e.Installed, e.Supported = false, false
		e.FileName = FileName(e.URL)
		seen[e.ID] = len(es)
		es = append(es, e)
	}
	return es
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
		if es[i].Source == "" {
			es[i].Source = "builtin"
		}
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
	case "md5":
		// Weaker than SHA-256, but it is all some publishers (MySQL) offer; it
		// still catches damaged or swapped downloads.
		if !hex32.MatchString(e.Checksum.Value) {
			return errors.New("md5 checksum must be 32 hex chars")
		}
	case "shasums":
		if !IsAllowedURL(e.Checksum.URL) {
			return errors.New("shasums url must be https")
		}
	case "none":
		if !e.Unverified {
			return errors.New("checksum type none needs unverified:true")
		}
	default:
		return errors.New("checksum type must be sha256, md5, shasums or none")
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

// Annotate sets Supported, Installed and Latest (an installed runtime whose
// binary is the entry's binary inside <bin>/<id>), and orders the
// list by kind, then newest version first.
func Annotate(es []model.CatalogEntry, bin string, rts []model.Runtime) []model.CatalogEntry {
	out := make([]model.CatalogEntry, len(es))
	for i, e := range es {
		e.Supported = Supported(e)
		e.FileName = FileName(e.URL)
		want := filepath.Join(bin, e.ID, filepath.FromSlash(e.BinaryRelPath))
		for _, r := range rts {
			if r.Binary == want {
				e.Installed = true
			}
		}
		out[i] = e
	}
	best := map[string]string{}
	key := func(e model.CatalogEntry) string { return e.Kind + "|" + e.OS + "|" + e.Arch + "|" + e.Line }
	for _, e := range out {
		if v, ok := best[key(e)]; !ok || CompareVersions(e.Version, v) > 0 {
			best[key(e)] = e.Version
		}
	}
	for i := range out {
		out[i].Latest = CompareVersions(out[i].Version, best[key(out[i])]) == 0
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].OS != out[j].OS {
			return out[i].OS > out[j].OS // windows before linux
		}
		return CompareVersions(out[i].Version, out[j].Version) > 0
	})
	return out
}

var numRE = regexp.MustCompile(`\d+`)

// CompareVersions compares dotted versions numerically ("21.0.5+11" > "21.0.4+7",
// "1.26.10" > "1.26.9"). Non-numeric parts are ignored.
func CompareVersions(a, b string) int {
	x, y := numRE.FindAllString(a, -1), numRE.FindAllString(b, -1)
	for i := 0; i < len(x) || i < len(y); i++ {
		var p, q int
		if i < len(x) {
			p, _ = strconv.Atoi(x[i])
		}
		if i < len(y) {
			q, _ = strconv.Atoi(y[i])
		}
		if p != q {
			if p > q {
				return 1
			}
			return -1
		}
	}
	return 0
}
