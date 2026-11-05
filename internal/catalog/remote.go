package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"zero2dev/internal/model"
)

// Default publisher endpoints. Tests and mirrors can override them on Remote.
const (
	DefaultPHPURL      = "https://windows.php.net/downloads/releases/releases.json"
	DefaultAdoptiumURL = "https://api.adoptium.net"
	DefaultNginxPage   = "https://nginx.org/en/download.html"
	DefaultNginxBase   = "https://nginx.org/download/"
	DefaultNodeURL     = "https://nodejs.org/dist/index.json"
	DefaultNodeBase    = "https://nodejs.org/dist/"
	DefaultGoURL       = "https://go.dev/dl/?mode=json&include=all"
	DefaultGoBase      = "https://go.dev/dl/"
	DefaultCaddyURL    = "https://api.github.com/repos/caddyserver/caddy/releases?per_page=40"
	DefaultPHPArchive  = "https://windows.php.net/downloads/releases/archives/"
)

// SourceStatus is the outcome of checking one publisher.
type SourceStatus struct {
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	OK        bool      `json:"ok"`
	Count     int       `json:"count"`
	Error     string    `json:"error"`
	CheckedAt time.Time `json:"checkedAt"`
}

// Status is what GET /api/catalog/status returns.
type Status struct {
	CheckedAt *time.Time     `json:"checkedAt"`
	Sources   []SourceStatus `json:"sources"`
}

type cacheFile struct {
	Status  Status               `json:"status"`
	Entries []model.CatalogEntry `json:"entries"`
}

func cachePath(home string) string { return filepath.Join(home, "cache", "remote-catalog.json") }

// loadCache reads the cached remote catalog; a missing or damaged file is an empty cache.
func loadCache(home string) cacheFile {
	var c cacheFile
	b, err := os.ReadFile(cachePath(home))
	if err != nil || json.Unmarshal(b, &c) != nil {
		return cacheFile{Status: Status{Sources: []SourceStatus{}}}
	}
	if c.Status.Sources == nil {
		c.Status.Sources = []SourceStatus{}
	}
	return c
}

func saveCache(home string, c cacheFile) error {
	p := cachePath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Remote discovers newer versions of PHP, Java and nginx on the publishers' sites.
type Remote struct {
	Home   string
	Client func() *http.Client // honours the download proxy setting
	// Overrides (tests, mirrors). Empty means the default publisher URL.
	PHPURL, AdoptiumURL, NginxPage, NginxBase string
	NodeURL, NodeBase, GoURL, GoBase          string
	CaddyURL, PHPArchiveURL                   string
	Version                                   string

	mu sync.Mutex
}

func or(v, d string) string {
	if v != "" {
		return v
	}
	return d
}

// Status returns the last refresh result.
func (r *Remote) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return loadCache(r.Home).Status
}

// Refresh asks every publisher for its version list and caches the result.
// A failing source keeps its previously cached entries. It returns the new
// status and the ids that were not in the catalog before.
func (r *Remote) Refresh(ctx context.Context) (Status, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old := loadCache(r.Home)
	known := map[string]bool{}
	if es, err := Parse(defaultManifest); err == nil {
		for _, e := range es {
			known[e.ID] = true
		}
	}
	for _, e := range old.Entries {
		known[e.ID] = true
	}

	type result struct {
		st      SourceStatus
		entries []model.CatalogEntry
	}
	fetchers := []struct {
		kind, name string
		fn         func(context.Context) ([]model.CatalogEntry, error)
	}{
		{"php", hostLabel(or(r.PHPURL, DefaultPHPURL)), r.fetchPHP},
		{"java", hostLabel(or(r.AdoptiumURL, DefaultAdoptiumURL)), r.fetchJava},
		{"nginx", hostLabel(or(r.NginxPage, DefaultNginxPage)), r.fetchNginx},
		{"node", hostLabel(or(r.NodeURL, DefaultNodeURL)), r.fetchNode},
		{"go", hostLabel(or(r.GoURL, DefaultGoURL)), r.fetchGo},
		{"caddy", hostLabel(or(r.CaddyURL, DefaultCaddyURL)), r.fetchCaddy},
	}
	res := make([]result, len(fetchers))
	var wg sync.WaitGroup
	for i, f := range fetchers {
		wg.Add(1)
		go func(i int, kind, name string, fn func(context.Context) ([]model.CatalogEntry, error)) {
			defer wg.Done()
			es, err := fn(ctx)
			st := SourceStatus{Kind: kind, Name: name, CheckedAt: time.Now().UTC().Truncate(time.Second)}
			if err != nil {
				st.Error = err.Error()
			}
			st.OK, st.Count = len(es) > 0 && (err == nil || len(es) > 0), len(es)
			res[i] = result{st: st, entries: es}
		}(i, f.kind, f.name, f.fn)
	}
	wg.Wait()

	next := cacheFile{Status: Status{Sources: []SourceStatus{}}}
	anyOK := false
	for i, f := range fetchers {
		next.Status.Sources = append(next.Status.Sources, res[i].st)
		if res[i].st.OK {
			anyOK = true
			next.Entries = append(next.Entries, res[i].entries...)
			continue
		}
		for _, e := range old.Entries { // keep what we knew
			if e.Kind == f.kind {
				next.Entries = append(next.Entries, e)
			}
		}
	}
	for _, e := range old.Entries { // kinds without a fetcher are not expected; keep anyway
		if e.Kind != "php" && e.Kind != "java" && e.Kind != "nginx" {
			next.Entries = append(next.Entries, e)
		}
	}
	if anyOK {
		now := time.Now().UTC().Truncate(time.Second)
		next.Status.CheckedAt = &now
	} else {
		next.Status.CheckedAt = old.Status.CheckedAt
	}
	if err := saveCache(r.Home, next); err != nil {
		return next.Status, nil, fmt.Errorf("saving cache: %w", err)
	}
	added := []string{}
	for _, e := range next.Entries {
		if !known[e.ID] {
			added = append(added, e.ID)
		}
	}
	sort.Strings(added)
	return next.Status, added, nil
}

func hostLabel(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// hostOK allows https from the publisher's hosts (and loopback http for tests).
func hostOK(raw string, hosts ...string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if h := u.Hostname(); u.Scheme == "http" && (h == "127.0.0.1" || h == "localhost" || h == "::1") {
		return true
	}
	if u.Scheme != "https" {
		return false
	}
	for _, h := range hosts {
		if u.Hostname() == h || strings.HasSuffix(u.Hostname(), "."+h) {
			return true
		}
	}
	return false
}

func (r *Remote) get(ctx context.Context, rawURL string) ([]byte, error) {
	return r.getN(ctx, rawURL, 24<<20)
}

func (r *Remote) getN(ctx context.Context, rawURL string, max int64) ([]byte, error) {
	if !IsAllowedURL(rawURL) {
		return nil, errors.New("url not allowed")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Zero2Dev/"+or(r.Version, "0"))
	c := http.DefaultClient
	if r.Client != nil {
		c = r.Client()
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %v", hostLabel(rawURL), errText(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", hostLabel(rawURL), resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %v", hostLabel(rawURL), errText(err))
	}
	if int64(len(b)) > max {
		// Never hand a silently cut-off document to a JSON parser.
		return nil, fmt.Errorf("%s answered with more than %d MB", hostLabel(rawURL), max>>20)
	}
	return b, nil
}

// errText drops the quoted URL net/http puts in errors (it may hold credentials in a proxy URL).
func errText(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

// ---- PHP: windows.php.net releases.json ------------------------------------------------

var phpKeyRE = regexp.MustCompile(`^nts-v[cs](\d+)-x64$`)

type phpFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func (r *Remote) fetchPHP(ctx context.Context) ([]model.CatalogEntry, error) {
	src := or(r.PHPURL, DefaultPHPURL)
	b, err := r.get(ctx, src)
	if err != nil {
		return nil, err
	}
	base := src[:strings.LastIndex(src, "/")+1]
	es, err := parsePHP(b, base)
	if err != nil {
		return nil, err
	}
	// Older lines (7.x, 8.0 ...) only exist in the archive. Failing here keeps the current releases.
	more, aerr := r.fetchPHPArchive(ctx, es)
	if aerr != nil {
		return es, fmt.Errorf("older PHP versions: %v", aerr)
	}
	return append(es, more...), nil
}

// parsePHP turns releases.json into one entry per supported branch (8.x and newer,
// non-thread-safe x64 build, which is what the CLI and the built-in server use).
func parsePHP(b []byte, base string) ([]model.CatalogEntry, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, fmt.Errorf("unexpected releases.json: %v", err)
	}
	var out []model.CatalogEntry
	for branch, raw := range top {
		if !regexp.MustCompile(`^\d+\.\d+$`).MatchString(branch) || CompareVersions(branch, "8.0") < 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			continue
		}
		var version string
		if json.Unmarshal(fields["version"], &version) != nil || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(version) {
			continue
		}
		bestKey, bestN := "", -1
		for k := range fields {
			if m := phpKeyRE.FindStringSubmatch(k); m != nil {
				n := 0
				fmt.Sscan(m[1], &n)
				if n > bestN {
					bestKey, bestN = k, n
				}
			}
		}
		if bestKey == "" {
			continue
		}
		var build struct {
			Zip phpFile `json:"zip"`
		}
		if json.Unmarshal(fields[bestKey], &build) != nil || build.Zip.Path == "" || !hex64.MatchString(build.Zip.SHA256) {
			continue
		}
		if path.Base(build.Zip.Path) != build.Zip.Path || !strings.HasSuffix(build.Zip.Path, ".zip") {
			continue
		}
		link := base + build.Zip.Path
		if !hostOK(link, "windows.php.net", "php.net") {
			return nil, errors.New("releases.json points to an unexpected host")
		}
		out = append(out, model.CatalogEntry{
			ID: "php-" + version + "-windows-amd64", Kind: "php", Version: version, Name: "PHP " + version + " (NTS)",
			URL: link, Checksum: model.Checksum{Type: "sha256", Value: strings.ToLower(build.Zip.SHA256)},
			BinaryRelPath: "php.exe", StripComponents: 0, OS: "windows", Arch: "amd64",
			Homepage: "https://windows.php.net/download/", Line: branch, Source: "remote",
		})
	}
	if len(out) == 0 {
		return nil, errors.New("releases.json has no PHP 8+ Windows x64 build")
	}
	sort.Slice(out, func(i, j int) bool { return CompareVersions(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

// ---- Java: Adoptium (Eclipse Temurin) ---------------------------------------------------

type adoptAsset struct {
	Binary struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		ImageType    string `json:"image_type"`
		Package      struct {
			Link     string `json:"link"`
			Checksum string `json:"checksum"`
			Name     string `json:"name"`
		} `json:"package"`
	} `json:"binary"`
	Version struct {
		Major  int    `json:"major"`
		Semver string `json:"semver"`
	} `json:"version"`
}

var semverRE = regexp.MustCompile(`^(\d+(?:\.\d+)*)(?:\+(\d+))?`)

func (r *Remote) fetchJava(ctx context.Context) ([]model.CatalogEntry, error) {
	base := strings.TrimRight(or(r.AdoptiumURL, DefaultAdoptiumURL), "/")
	b, err := r.get(ctx, base+"/v3/info/available_releases")
	if err != nil {
		return nil, err
	}
	var rel struct {
		LTS    []int `json:"available_lts_releases"`
		Recent int   `json:"most_recent_feature_release"`
	}
	if err := json.Unmarshal(b, &rel); err != nil || len(rel.LTS) == 0 {
		return nil, errors.New("unexpected answer from the Adoptium API (available_releases)")
	}
	isLTS := map[int]bool{}
	feats := []int{}
	for _, v := range rel.LTS {
		isLTS[v] = true
		if v >= 8 {
			feats = append(feats, v)
		}
	}
	if rel.Recent > 0 && !isLTS[rel.Recent] {
		feats = append(feats, rel.Recent)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(feats)))
	if len(feats) > 6 {
		feats = feats[:6]
	}

	type job struct {
		feat int
		os   string
	}
	var jobs []job
	for _, f := range feats {
		jobs = append(jobs, job{f, "windows"}, job{f, "linux"})
	}
	var (
		mu       sync.Mutex
		out      []model.CatalogEntry
		firstErr error
		wg       sync.WaitGroup
		sem      = make(chan struct{}, 4)
	)
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			u := fmt.Sprintf("%s/v3/assets/latest/%d/hotspot?architecture=x64&image_type=jdk&os=%s", base, j.feat, j.os)
			body, err := r.get(ctx, u)
			var e model.CatalogEntry
			if err == nil {
				e, err = parseAdoptium(body, j.feat, j.os, isLTS[j.feat])
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("Java %d (%s): %v", j.feat, j.os, err)
				}
				return
			}
			out = append(out, e)
		}(j)
	}
	wg.Wait()
	if len(out) == 0 {
		if firstErr == nil {
			firstErr = errors.New("no Java builds found")
		}
		return nil, firstErr
	}
	sort.Slice(out, func(i, j int) bool {
		if c := CompareVersions(out[i].Version, out[j].Version); c != 0 {
			return c > 0
		}
		return out[i].OS > out[j].OS
	})
	return out, firstErr // partial results are kept; the error is shown as a warning
}

func parseAdoptium(body []byte, feat int, osName string, lts bool) (model.CatalogEntry, error) {
	var assets []adoptAsset
	if err := json.Unmarshal(body, &assets); err != nil {
		return model.CatalogEntry{}, errors.New("unexpected answer from the Adoptium API")
	}
	for _, a := range assets {
		p := a.Binary.Package
		if a.Binary.ImageType != "jdk" || a.Binary.OS != osName || a.Binary.Architecture != "x64" || p.Link == "" {
			continue
		}
		m := semverRE.FindStringSubmatch(a.Version.Semver)
		if m == nil || !hex64.MatchString(p.Checksum) {
			continue
		}
		ver, idv := m[1], m[1]
		if m[2] != "" {
			ver, idv = m[1]+"+"+m[2], m[1]+"-b"+m[2]
		}
		if !hostOK(p.Link, "github.com", "githubusercontent.com", "adoptium.net") {
			return model.CatalogEntry{}, errors.New("download link points to an unexpected host")
		}
		bin, name := "bin/java", "Java "+ver+" (Temurin"
		if osName == "windows" {
			bin = "bin/java.exe"
		}
		ch := ""
		if lts {
			ch, name = "lts", name+" LTS"
		}
		return model.CatalogEntry{
			ID: "java-" + idv + "-" + osName + "-amd64", Kind: "java", Version: ver, Name: name + ")",
			URL: p.Link, Checksum: model.Checksum{Type: "sha256", Value: strings.ToLower(p.Checksum)},
			BinaryRelPath: bin, StripComponents: 1, OS: osName, Arch: "amd64",
			Homepage: fmt.Sprintf("https://adoptium.net/temurin/releases/?version=%d", feat),
			Line:     fmt.Sprint(feat), Channel: ch, Source: "remote",
		}, nil
	}
	return model.CatalogEntry{}, errors.New("no matching JDK build in the answer")
}

// ---- nginx: nginx.org download page -----------------------------------------------------

var nginxZipRE = regexp.MustCompile(`href="(?:https?://nginx\.org)?/download/nginx-(\d+\.\d+\.\d+)\.zip"`)

func (r *Remote) fetchNginx(ctx context.Context) ([]model.CatalogEntry, error) {
	b, err := r.get(ctx, or(r.NginxPage, DefaultNginxPage))
	if err != nil {
		return nil, err
	}
	return parseNginx(string(b), or(r.NginxBase, DefaultNginxBase))
}

// parseNginx lists the Windows zips linked from the download page. nginx.org
// publishes no checksums, so the entries are marked unverified.
func parseNginx(html, base string) ([]model.CatalogEntry, error) {
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	seen := map[string]bool{}
	var out []model.CatalogEntry
	for _, m := range nginxZipRE.FindAllStringSubmatch(html, -1) {
		v := m[1]
		if seen[v] {
			continue
		}
		seen[v] = true
		parts := strings.Split(v, ".")
		minor := 0
		fmt.Sscan(parts[1], &minor)
		ch := "stable"
		if minor%2 == 1 {
			ch = "mainline"
		}
		link := base + "nginx-" + v + ".zip"
		if !hostOK(link, "nginx.org") {
			return nil, errors.New("download base points to an unexpected host")
		}
		out = append(out, model.CatalogEntry{
			ID: "nginx-" + v + "-windows-amd64", Kind: "nginx", Version: v, Name: "nginx " + v + " (" + ch + ")",
			URL: link, Checksum: model.Checksum{Type: "none"}, Unverified: true,
			BinaryRelPath: "nginx.exe", StripComponents: 1, OS: "windows", Arch: "amd64",
			Homepage: "https://nginx.org/en/download.html", Line: parts[0] + "." + parts[1], Channel: ch,
			Source: "remote",
		})
	}
	if len(out) == 0 {
		return nil, errors.New("no Windows downloads found on the nginx download page")
	}
	sort.Slice(out, func(i, j int) bool { return CompareVersions(out[i].Version, out[j].Version) > 0 })
	if len(out) > 8 {
		out = out[:8]
	}
	return out, nil
}
