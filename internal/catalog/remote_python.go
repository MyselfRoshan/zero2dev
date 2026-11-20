package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"zero2dev/internal/model"
)

// Python comes from python-build-standalone (github.com/astral-sh/python-build-standalone): one
// release holds every Python line for every platform, so it is not the one-asset-per-release
// shape of parseGitHub. Each release is tagged with its build date (20261026) and carries assets
// like cpython-3.13.18+20261026-x86_64-pc-windows-msvc-install_only.tar.gz. The archive has one
// root folder, python, with python.exe, Scripts\pip.exe and the standard library.

const (
	pythonRepo  = "astral-sh/python-build-standalone"
	pythonLines = 4 // the newest patch of this many minor lines (3.14, 3.13, 3.12, 3.11)
	// A release list answers with every asset of every release (about 2 MB each), so only the newest
	// few are asked for; a line missing from the newest build is taken from the one before.
	pythonReleases = 3
)

// pythonAssetRE matches the plain Windows install_only build: not free-threaded, not "_stripped",
// not a pre-release (3.16.0a1 has letters after the patch number).
var pythonAssetRE = regexp.MustCompile(`^cpython-(\d+)\.(\d+)\.(\d+)\+\d{8}-(x86_64|aarch64)-pc-windows-msvc-install_only\.tar\.gz$`)

var pythonArch = map[string]string{"x86_64": "amd64", "aarch64": "arm64"}

func (r *Remote) fetchPython(ctx context.Context) ([]model.CatalogEntry, error) {
	b, err := r.get(ctx, or(r.GitHubAPI, DefaultGitHubAPI)+pythonRepo+fmt.Sprintf("/releases?per_page=%d", pythonReleases))
	if err != nil {
		return nil, err
	}
	return parsePython(b)
}

// parsePython keeps, for each of the newest minor lines, the newest patch release and its Windows
// builds (amd64 and arm64). GitHub publishes a SHA-256 digest for every asset; an asset without one
// becomes an unverified entry.
func parsePython(b []byte) ([]model.CatalogEntry, error) {
	var rels []ghRelease
	if err := json.Unmarshal(b, &rels); err != nil {
		return nil, fmt.Errorf("unexpected GitHub answer: %v", err)
	}
	type key struct{ ver, arch string }
	best := map[string]string{}           // line -> newest version seen
	found := map[key]model.CatalogEntry{} // first (newest release) entry of a version and arch
	for _, rel := range rels {
		if rel.Draft || rel.Prerelease {
			continue
		}
		for _, a := range rel.Assets {
			m := pythonAssetRE.FindStringSubmatch(a.Name)
			if m == nil {
				continue
			}
			if !hostOK(a.URL, "github.com") {
				return nil, fmt.Errorf("a Python download link points to an unexpected host")
			}
			line, ver, arch := m[1]+"."+m[2], m[1]+"."+m[2]+"."+m[3], pythonArch[m[4]]
			if cur, ok := best[line]; !ok || CompareVersions(ver, cur) > 0 {
				best[line] = ver
			}
			k := key{ver, arch}
			if _, ok := found[k]; ok {
				continue
			}
			e := model.CatalogEntry{
				ID: "python-" + ver + "-windows-" + arch, Kind: "python", Version: ver, Name: "Python " + ver,
				URL: a.URL, BinaryRelPath: "python.exe", StripComponents: 1, OS: "windows", Arch: arch,
				Homepage: "https://github.com/" + pythonRepo + "/releases/tag/" + rel.Tag, Line: line, Source: "remote",
			}
			if d, ok := strings.CutPrefix(a.Digest, "sha256:"); ok && hex64.MatchString(d) {
				e.Checksum = model.Checksum{Type: "sha256", Value: strings.ToLower(d)}
			} else {
				e.Checksum, e.Unverified = model.Checksum{Type: "none"}, true
			}
			found[k] = e
		}
	}
	lines := make([]string, 0, len(best))
	for l := range best {
		lines = append(lines, l)
	}
	sort.Slice(lines, func(i, j int) bool { return CompareVersions(lines[i], lines[j]) > 0 })
	if len(lines) > pythonLines {
		lines = lines[:pythonLines]
	}
	var out []model.CatalogEntry
	for _, l := range lines {
		for _, arch := range []string{"amd64", "arm64"} {
			if e, ok := found[key{best[l], arch}]; ok {
				out = append(out, e)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Windows Python build found in the python-build-standalone releases")
	}
	return out, nil
}
