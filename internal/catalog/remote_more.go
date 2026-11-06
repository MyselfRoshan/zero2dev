package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"zero2dev/internal/model"
)

// ---- Node.js: nodejs.org/dist/index.json ---------------------------------------------------

type nodeRelease struct {
	Version string          `json:"version"`
	LTS     json.RawMessage `json:"lts"` // false, or the LTS codename
	Files   []string        `json:"files"`
}

func (r *Remote) fetchNode(ctx context.Context) ([]model.CatalogEntry, error) {
	src := or(r.NodeURL, DefaultNodeURL)
	b, err := r.get(ctx, src)
	if err != nil {
		return nil, err
	}
	base := or(r.NodeBase, DefaultNodeBase)
	if r.NodeBase == "" && r.NodeURL != "" {
		base = src[:strings.LastIndex(src, "/")+1]
	}
	return parseNode(b, base)
}

// parseNode keeps the newest release of each major (v14 and later), for Windows and Linux x64.
func parseNode(b []byte, base string) ([]model.CatalogEntry, error) {
	var rels []nodeRelease
	if err := json.Unmarshal(b, &rels); err != nil {
		return nil, fmt.Errorf("unexpected node index.json: %v", err)
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	best := map[int]nodeRelease{}
	verRE := regexp.MustCompile(`^v(\d+)\.\d+\.\d+$`)
	for _, rel := range rels {
		m := verRE.FindStringSubmatch(rel.Version)
		if m == nil {
			continue
		}
		major := 0
		fmt.Sscan(m[1], &major)
		if major < 14 {
			continue
		}
		if cur, ok := best[major]; !ok || CompareVersions(rel.Version, cur.Version) > 0 {
			best[major] = rel
		}
	}
	majors := make([]int, 0, len(best))
	for m := range best {
		majors = append(majors, m)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(majors)))
	if len(majors) > 10 {
		majors = majors[:10]
	}
	has := func(r nodeRelease, f string) bool {
		for _, x := range r.Files {
			if x == f {
				return true
			}
		}
		return false
	}
	var out []model.CatalogEntry
	for _, major := range majors {
		rel := best[major]
		ver := strings.TrimPrefix(rel.Version, "v")
		ch := ""
		var codename string
		if json.Unmarshal(rel.LTS, &codename) == nil && codename != "" {
			ch = "lts"
		}
		dir := base + rel.Version + "/"
		if !hostOK(dir, "nodejs.org") {
			return nil, errors.New("node download base points to an unexpected host")
		}
		mk := func(osName, file, bin string) model.CatalogEntry {
			return model.CatalogEntry{
				ID: "node-" + ver + "-" + osName + "-amd64", Kind: "node", Version: ver, Name: "Node.js " + ver,
				URL: dir + file, Checksum: model.Checksum{Type: "shasums", URL: dir + "SHASUMS256.txt"},
				BinaryRelPath: bin, StripComponents: 1, OS: osName, Arch: "amd64", Homepage: dir,
				Line: fmt.Sprint(major), Channel: ch, Source: "remote",
			}
		}
		if has(rel, "win-x64-zip") {
			out = append(out, mk("windows", "node-"+rel.Version+"-win-x64.zip", "node.exe"))
		}
		if has(rel, "linux-x64") {
			out = append(out, mk("linux", "node-"+rel.Version+"-linux-x64.tar.gz", "bin/node"))
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no Node.js builds found in index.json")
	}
	return out, nil
}

// ---- Go: go.dev/dl/?mode=json&include=all --------------------------------------------------

type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   []struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		SHA256   string `json:"sha256"`
		Kind     string `json:"kind"`
	} `json:"files"`
}

func (r *Remote) fetchGo(ctx context.Context) ([]model.CatalogEntry, error) {
	b, err := r.getN(ctx, or(r.GoURL, DefaultGoURL), 24<<20)
	if err != nil {
		return nil, err
	}
	return parseGo(b, or(r.GoBase, DefaultGoBase))
}

// parseGo keeps the newest stable patch of each of the last eight Go lines (1.23, 1.22 ...).
func parseGo(b []byte, base string) ([]model.CatalogEntry, error) {
	var rels []goRelease
	if err := json.Unmarshal(b, &rels); err != nil {
		return nil, fmt.Errorf("unexpected go.dev answer: %v", err)
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	lineRE := regexp.MustCompile(`^go(\d+\.\d+)(?:\.\d+)?$`)
	best := map[string]goRelease{}
	for _, rel := range rels {
		m := lineRE.FindStringSubmatch(rel.Version)
		if m == nil || !rel.Stable {
			continue
		}
		if cur, ok := best[m[1]]; !ok || CompareVersions(rel.Version, cur.Version) > 0 {
			best[m[1]] = rel
		}
	}
	lines := make([]string, 0, len(best))
	for l := range best {
		lines = append(lines, l)
	}
	sort.Slice(lines, func(i, j int) bool { return CompareVersions(lines[i], lines[j]) > 0 })
	if len(lines) > 8 {
		lines = lines[:8]
	}
	var out []model.CatalogEntry
	for _, line := range lines {
		rel := best[line]
		ver := strings.TrimPrefix(rel.Version, "go")
		for _, f := range rel.Files {
			if f.Kind != "archive" || f.Arch != "amd64" || (f.OS != "windows" && f.OS != "linux") || !hex64.MatchString(f.SHA256) {
				continue
			}
			link := base + f.Filename
			if !hostOK(link, "go.dev", "golang.org", "dl.google.com") {
				return nil, errors.New("go download base points to an unexpected host")
			}
			bin := "bin/go"
			if f.OS == "windows" {
				bin = "bin/go.exe"
			}
			out = append(out, model.CatalogEntry{
				ID: "go-" + ver + "-" + f.OS + "-amd64", Kind: "go", Version: ver, Name: "Go " + ver,
				URL: link, Checksum: model.Checksum{Type: "sha256", Value: strings.ToLower(f.SHA256)},
				BinaryRelPath: bin, StripComponents: 1, OS: f.OS, Arch: "amd64", Homepage: "https://go.dev/dl/",
				Line: line, Source: "remote",
			})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no Go archives found")
	}
	return out, nil
}

// ---- Caddy: GitHub releases ----------------------------------------------------------------

type ghRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"` // "sha256:<hex>" (GitHub adds it to every asset)
	} `json:"assets"`
}

func (r *Remote) fetchCaddy(ctx context.Context) ([]model.CatalogEntry, error) {
	// GitHub release lists carry every asset of every release (about 100 KB each), so keep the page small.
	b, err := r.get(ctx, or(r.CaddyURL, DefaultCaddyURL))
	if err != nil {
		return nil, err
	}
	return parseCaddy(b)
}

// parseCaddy keeps the newest stable patch of each of the last six Caddy lines (2.8, 2.7 ...).
// Archives are verified against the release's checksum file (SHA-512), like the built-in entry.
func parseCaddy(b []byte) ([]model.CatalogEntry, error) {
	var rels []ghRelease
	if err := json.Unmarshal(b, &rels); err != nil {
		return nil, fmt.Errorf("unexpected GitHub answer: %v", err)
	}
	tagRE := regexp.MustCompile(`^v(\d+\.\d+)\.\d+$`)
	best := map[string]ghRelease{}
	for _, rel := range rels {
		m := tagRE.FindStringSubmatch(rel.Tag)
		if m == nil || rel.Draft || rel.Prerelease {
			continue
		}
		if cur, ok := best[m[1]]; !ok || CompareVersions(rel.Tag, cur.Tag) > 0 {
			best[m[1]] = rel
		}
	}
	lines := make([]string, 0, len(best))
	for l := range best {
		lines = append(lines, l)
	}
	sort.Slice(lines, func(i, j int) bool { return CompareVersions(lines[i], lines[j]) > 0 })
	if len(lines) > 6 {
		lines = lines[:6]
	}
	var out []model.CatalogEntry
	for _, line := range lines {
		rel := best[line]
		ver := strings.TrimPrefix(rel.Tag, "v")
		urls := map[string]string{}
		for _, a := range rel.Assets {
			urls[a.Name] = a.URL
		}
		sums := urls["caddy_"+ver+"_checksums.txt"]
		for _, t := range []struct{ os, file, bin string }{
			{"windows", "caddy_" + ver + "_windows_amd64.zip", "caddy.exe"},
			{"linux", "caddy_" + ver + "_linux_amd64.tar.gz", "caddy"},
		} {
			link := urls[t.file]
			if link == "" || sums == "" {
				continue
			}
			if !hostOK(link, "github.com") || !hostOK(sums, "github.com") {
				return nil, errors.New("a Caddy download link points to an unexpected host")
			}
			out = append(out, model.CatalogEntry{
				ID: "caddy-" + ver + "-" + t.os + "-amd64", Kind: "caddy", Version: ver, Name: "Caddy " + ver,
				URL: link, Checksum: model.Checksum{Type: "shasums", URL: sums},
				BinaryRelPath: t.bin, StripComponents: 0, OS: t.os, Arch: "amd64",
				Homepage: "https://github.com/caddyserver/caddy/releases/tag/" + rel.Tag, Line: line, Source: "remote",
			})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no Caddy builds found in the GitHub releases")
	}
	return out, nil
}

// ---- PHP: older lines from windows.php.net/downloads/releases/archives ----------------------

var phpArchiveRE = regexp.MustCompile(`(?i)href="[^"]*?(php-(\d+\.\d+\.\d+)-nts-Win32-(?:vc|vs)\d+-x64\.zip)"`)
var hexTokRE = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)

func (r *Remote) fetchPHPArchive(ctx context.Context, current []model.CatalogEntry) ([]model.CatalogEntry, error) {
	base := or(r.PHPArchiveURL, DefaultPHPArchive)
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	page, err := r.get(ctx, base)
	if err != nil {
		return nil, err
	}
	var sums map[string]string
	if b, err := r.get(ctx, base+"sha256sum.txt"); err == nil {
		sums = parsePHPSums(string(b))
	}
	have := map[string]bool{}
	for _, e := range current {
		have[e.Line] = true
	}
	return parsePHPArchive(string(page), base, sums, have)
}

// parsePHPSums reads lines such as "<sha256> *php-7.4.33-nts-Win32-vc15-x64.zip" (either order).
func parsePHPSums(body string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		h := hexTokRE.FindString(line)
		if h == "" {
			continue
		}
		for _, f := range strings.Fields(strings.ReplaceAll(line, "*", " ")) {
			if strings.HasSuffix(strings.ToLower(f), ".zip") {
				out[f] = strings.ToLower(h)
			}
		}
	}
	return out
}

// parsePHPArchive lists the newest NTS x64 build of each line not already known, verified
// when the archive publishes its SHA-256 and flagged unverified when it does not.
func parsePHPArchive(html, base string, sums map[string]string, have map[string]bool) ([]model.CatalogEntry, error) {
	type found struct{ ver, file string }
	best := map[string]found{}
	for _, m := range phpArchiveRE.FindAllStringSubmatch(html, -1) {
		parts := strings.Split(m[2], ".")
		line := parts[0] + "." + parts[1]
		if have[line] || CompareVersions(line, "5.6") < 0 {
			continue
		}
		if cur, ok := best[line]; !ok || CompareVersions(m[2], cur.ver) > 0 {
			best[line] = found{m[2], m[1]}
		}
	}
	var out []model.CatalogEntry
	for line, f := range best {
		link := base + f.file
		if !hostOK(link, "windows.php.net", "php.net") {
			return nil, errors.New("archive points to an unexpected host")
		}
		e := model.CatalogEntry{
			ID: "php-" + f.ver + "-windows-amd64", Kind: "php", Version: f.ver, Name: "PHP " + f.ver + " (NTS)",
			URL: link, BinaryRelPath: "php.exe", StripComponents: 0, OS: "windows", Arch: "amd64",
			Homepage: "https://windows.php.net/downloads/releases/archives/", Line: line, Source: "remote",
		}
		if h := sums[f.file]; h != "" {
			e.Checksum = model.Checksum{Type: "sha256", Value: h}
		} else {
			e.Checksum, e.Unverified = model.Checksum{Type: "none"}, true
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return CompareVersions(out[i].Version, out[j].Version) > 0 })
	return out, nil
}
