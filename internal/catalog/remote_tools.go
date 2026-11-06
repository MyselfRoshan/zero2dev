package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"zero2dev/internal/model"
)

// Sources for the 0.0.3 kinds: databases, code editors and database tools.
// MySQL has no fetcher: its download pages are not meant for programs, so its
// entries are pinned in the built-in manifest (checked with the MD5 MySQL
// publishes) and newer versions can be imported with a pasted MD5.

const (
	DefaultMariaDBURL   = "https://downloads.mariadb.org/rest-api/mariadb/"
	DefaultPostgresPage = "https://www.enterprisedb.com/download-postgresql-binaries"
	DefaultVSCodeURL    = "https://update.code.visualstudio.com/api/update/win32-x64-archive/stable/latest"
	DefaultGitHubAPI    = "https://api.github.com/repos/"
)

// ---- generic GitHub releases (asset digest = SHA-256) ------------------------------------------

// ghSpec describes one GitHub-hosted Windows ZIP.
type ghSpec struct {
	kind, label, repo string
	asset             *regexp.Regexp // matches the asset name; group 1 is the version
	bin               string
	strip, keep       int
}

var ghSpecs = []ghSpec{
	{kind: "redis", label: "Redis", repo: "redis-windows/redis-windows",
		asset: regexp.MustCompile(`^Redis-(\d+\.\d+\.\d+)-Windows-x64-msys2\.zip$`), bin: "redis-server.exe", strip: 1, keep: 3},
	{kind: "vscodium", label: "VSCodium", repo: "VSCodium/vscodium",
		asset: regexp.MustCompile(`^VSCodium-win32-x64-(\d+\.\d+\.\d+)\.zip$`), bin: "VSCodium.exe", strip: 0, keep: 2},
	{kind: "notepadpp", label: "Notepad++", repo: "notepad-plus-plus/notepad-plus-plus",
		asset: regexp.MustCompile(`^npp\.(\d+(?:\.\d+){1,3})\.portable\.x64\.zip$`), bin: "notepad++.exe", strip: 0, keep: 2},
	{kind: "heidisql", label: "HeidiSQL", repo: "HeidiSQL/HeidiSQL",
		asset: regexp.MustCompile(`^HeidiSQL_(\d+\.\d+(?:\.\d+)?)_64_Portable\.zip$`), bin: "heidisql.exe", strip: 0, keep: 2},
	{kind: "dbeaver", label: "DBeaver", repo: "dbeaver/dbeaver",
		asset: regexp.MustCompile(`^dbeaver-ce-(\d+\.\d+\.\d+)-windows-x86_64\.zip$`), bin: "dbeaver.exe", strip: 1, keep: 2},
}

func (r *Remote) ghURL(s ghSpec) string {
	return or(r.GitHubAPI, DefaultGitHubAPI) + s.repo + "/releases?per_page=10"
}

func (r *Remote) fetchGitHub(s ghSpec) func(context.Context) ([]model.CatalogEntry, error) {
	return func(ctx context.Context) ([]model.CatalogEntry, error) {
		b, err := r.get(ctx, r.ghURL(s))
		if err != nil {
			return nil, err
		}
		return parseGitHub(b, s)
	}
}

// parseGitHub keeps the newest s.keep stable releases that carry the Windows
// ZIP. GitHub publishes a SHA-256 digest for every asset; an asset without one
// becomes an unverified entry.
func parseGitHub(b []byte, s ghSpec) ([]model.CatalogEntry, error) {
	var rels []ghRelease
	if err := json.Unmarshal(b, &rels); err != nil {
		return nil, fmt.Errorf("unexpected GitHub answer: %v", err)
	}
	var out []model.CatalogEntry
	for _, rel := range rels {
		if rel.Draft || rel.Prerelease {
			continue
		}
		for _, a := range rel.Assets {
			m := s.asset.FindStringSubmatch(a.Name)
			if m == nil {
				continue
			}
			if !hostOK(a.URL, "github.com") {
				return nil, fmt.Errorf("a %s download link points to an unexpected host", s.label)
			}
			ver := m[1]
			e := model.CatalogEntry{
				ID: s.kind + "-" + ver + "-windows-amd64", Kind: s.kind, Version: ver, Name: s.label + " " + ver,
				URL: a.URL, BinaryRelPath: s.bin, StripComponents: s.strip, OS: "windows", Arch: "amd64",
				Homepage: "https://github.com/" + s.repo + "/releases/tag/" + rel.Tag, Source: "remote",
			}
			if d, ok := strings.CutPrefix(a.Digest, "sha256:"); ok && hex64.MatchString(d) {
				e.Checksum = model.Checksum{Type: "sha256", Value: strings.ToLower(d)}
			} else {
				e.Checksum, e.Unverified = model.Checksum{Type: "none"}, true
			}
			out = append(out, e)
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return CompareVersions(out[i].Version, out[j].Version) > 0 })
	if len(out) > s.keep {
		out = out[:s.keep]
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Windows %s download found in the GitHub releases", s.label)
	}
	return out, nil
}

// ---- VS Code: the update API --------------------------------------------------------------

func (r *Remote) fetchVSCode(ctx context.Context) ([]model.CatalogEntry, error) {
	b, err := r.get(ctx, or(r.VSCodeURL, DefaultVSCodeURL))
	if err != nil {
		return nil, err
	}
	return parseVSCode(b)
}

// parseVSCode reads the latest stable Windows x64 ZIP and its SHA-256.
func parseVSCode(b []byte) ([]model.CatalogEntry, error) {
	var v struct {
		URL     string `json:"url"`
		Version string `json:"productVersion"`
		SHA256  string `json:"sha256hash"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("unexpected VS Code answer: %v", err)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(v.Version) || !hex64.MatchString(v.SHA256) {
		return nil, errors.New("the VS Code answer has no version or checksum")
	}
	if !hostOK(v.URL, "visualstudio.com", "microsoft.com", "vscode-cdn.net") {
		return nil, errors.New("the VS Code download link points to an unexpected host")
	}
	return []model.CatalogEntry{{
		ID: "vscode-" + v.Version + "-windows-amd64", Kind: "vscode", Version: v.Version, Name: "VS Code " + v.Version,
		URL: v.URL, Checksum: model.Checksum{Type: "sha256", Value: strings.ToLower(v.SHA256)},
		BinaryRelPath: "Code.exe", StripComponents: 0, OS: "windows", Arch: "amd64",
		Homepage: "https://code.visualstudio.com/updates", Source: "remote",
	}}, nil
}

// ---- MariaDB: downloads.mariadb.org REST API ----------------------------------------------

type mariaMajors struct {
	Majors []struct {
		ID      string `json:"release_id"`
		Status  string `json:"release_status"`
		Support string `json:"release_support_type"`
	} `json:"major_releases"`
}

type mariaLatest struct {
	Releases map[string]struct {
		Files []struct {
			Name     string `json:"file_name"`
			Package  string `json:"package_type"`
			OS       string `json:"os"`
			CPU      string `json:"cpu"`
			URL      string `json:"file_download_url"`
			Checksum struct {
				SHA256 string `json:"sha256sum"`
			} `json:"checksum"`
		} `json:"files"`
	} `json:"releases"`
}

func (r *Remote) fetchMariaDB(ctx context.Context) ([]model.CatalogEntry, error) {
	base := or(r.MariaDBURL, DefaultMariaDBURL)
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	b, err := r.get(ctx, base)
	if err != nil {
		return nil, err
	}
	lines, err := parseMariaMajors(b)
	if err != nil {
		return nil, err
	}
	var out []model.CatalogEntry
	var lastErr error
	for _, l := range lines {
		b, err := r.get(ctx, base+l.id+"/latest/")
		if err != nil {
			lastErr = err
			continue
		}
		e, err := parseMariaLatest(b, l.id, l.lts)
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		if lastErr == nil {
			lastErr = errors.New("no MariaDB Windows downloads found")
		}
		return nil, lastErr
	}
	return out, nil
}

type mariaLine struct {
	id  string
	lts bool
}

// parseMariaMajors keeps the stable lines: every long-term-support line plus
// the newest rolling one, newest first, at most four.
func parseMariaMajors(b []byte) ([]mariaLine, error) {
	var m mariaMajors
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("unexpected MariaDB answer: %v", err)
	}
	var out []mariaLine
	rolling := false
	idRE := regexp.MustCompile(`^\d+\.\d+$`)
	for _, r := range m.Majors {
		if r.Status != "Stable" || !idRE.MatchString(r.ID) {
			continue
		}
		lts := strings.Contains(strings.ToLower(r.Support), "long term")
		if !lts {
			if rolling {
				continue
			}
			rolling = true
		}
		out = append(out, mariaLine{id: r.ID, lts: lts})
	}
	sort.SliceStable(out, func(i, j int) bool { return CompareVersions(out[i].id, out[j].id) > 0 })
	if len(out) > 4 {
		out = out[:4]
	}
	if len(out) == 0 {
		return nil, errors.New("no stable MariaDB lines in the answer")
	}
	return out, nil
}

// parseMariaLatest picks the Windows x64 ZIP (not the debug symbols) of a line.
// The API hands out http:// links; the same path is served over https.
func parseMariaLatest(b []byte, line string, lts bool) (model.CatalogEntry, error) {
	var l mariaLatest
	if err := json.Unmarshal(b, &l); err != nil {
		return model.CatalogEntry{}, fmt.Errorf("unexpected MariaDB answer: %v", err)
	}
	for ver, rel := range l.Releases {
		for _, f := range rel.Files {
			if f.OS != "Windows" || f.CPU != "x86_64" || f.Package != "ZIP file" || !strings.HasSuffix(f.Name, "-winx64.zip") {
				continue
			}
			link := f.URL
			if rest, ok := strings.CutPrefix(link, "http://"); ok {
				link = "https://" + rest
			}
			if !hostOK(link, "mariadb.org") {
				return model.CatalogEntry{}, errors.New("a MariaDB download link points to an unexpected host")
			}
			if !hex64.MatchString(f.Checksum.SHA256) {
				return model.CatalogEntry{}, fmt.Errorf("MariaDB %s has no SHA-256", ver)
			}
			ch := "stable"
			if lts {
				ch = "lts"
			}
			return model.CatalogEntry{
				ID: "mariadb-" + ver + "-windows-amd64", Kind: "mariadb", Version: ver, Name: "MariaDB " + ver,
				URL: link, Checksum: model.Checksum{Type: "sha256", Value: strings.ToLower(f.Checksum.SHA256)},
				BinaryRelPath: "bin/mariadbd.exe", StripComponents: 1, OS: "windows", Arch: "amd64",
				Homepage: "https://mariadb.org/download/", Line: line, Channel: ch, Source: "remote",
			}, nil
		}
	}
	return model.CatalogEntry{}, fmt.Errorf("no Windows ZIP for MariaDB %s", line)
}

// ---- PostgreSQL: EnterpriseDB binaries page -----------------------------------------------

var (
	pgBlockRE = regexp.MustCompile(`Version (?:<!-- -->)?(\d+\.\d+)</span>`)
	// Loopback links are accepted for tests, like everywhere else in this package.
	pgWinRE  = regexp.MustCompile(`href="((?:https://sbp\.enterprisedb\.com|http://127\.0\.0\.1:\d+)/getfile\.jsp\?fileid=\d+)"><img alt="Windows x86-64"`)
	pgFileRE = regexp.MustCompile(`^https://get\.enterprisedb\.com/postgresql/(postgresql-(\d+\.\d+)-\d+-windows-x64-binaries\.zip)$`)
)

func (r *Remote) fetchPostgres(ctx context.Context) ([]model.CatalogEntry, error) {
	b, err := r.get(ctx, or(r.PostgresPage, DefaultPostgresPage))
	if err != nil {
		return nil, err
	}
	links := parsePostgresPage(string(b))
	if len(links) == 0 {
		return nil, errors.New("no Windows downloads found on the PostgreSQL binaries page")
	}
	var out []model.CatalogEntry
	var lastErr error
	for _, l := range links {
		final, err := r.redirectTarget(ctx, l.link)
		if err != nil {
			lastErr = err
			continue
		}
		e, err := postgresEntry(final, l.version)
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, lastErr
	}
	return out, nil
}

type pgLink struct{ version, link string }

// parsePostgresPage returns, newest first, the Windows x86-64 link of each of
// the five newest versions. Each version block holds one link per platform.
func parsePostgresPage(html string) []pgLink {
	idx := pgBlockRE.FindAllStringSubmatchIndex(html, -1)
	var out []pgLink
	for i, m := range idx {
		end := len(html)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		if w := pgWinRE.FindStringSubmatch(html[m[1]:end]); w != nil {
			out = append(out, pgLink{version: html[m[2]:m[3]], link: w[1]})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return CompareVersions(out[i].version, out[j].version) > 0 })
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// postgresEntry builds the entry from the direct download link the
// getfile.jsp link redirects to. EnterpriseDB publishes no checksums.
func postgresEntry(final, version string) (model.CatalogEntry, error) {
	m := pgFileRE.FindStringSubmatch(final)
	if m == nil || m[2] != version {
		return model.CatalogEntry{}, fmt.Errorf("unexpected PostgreSQL download link for %s", version)
	}
	major := strings.Split(version, ".")[0]
	return model.CatalogEntry{
		ID: "postgres-" + version + "-windows-amd64", Kind: "postgres", Version: version, Name: "PostgreSQL " + version,
		URL: final, Checksum: model.Checksum{Type: "none"}, Unverified: true,
		BinaryRelPath: "bin/postgres.exe", StripComponents: 1, OS: "windows", Arch: "amd64",
		Homepage: "https://www.enterprisedb.com/download-postgresql-binaries", Line: major, Source: "remote",
	}, nil
}

// redirectTarget asks for rawURL without following the redirect and returns
// the Location it points to.
func (r *Remote) redirectTarget(ctx context.Context, rawURL string) (string, error) {
	if !IsAllowedURL(rawURL) {
		return "", errors.New("url not allowed")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Zero2Dev/"+or(r.Version, "0"))
	c := *http.DefaultClient
	if r.Client != nil {
		c = *r.Client()
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %v", hostLabel(rawURL), errText(err))
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode > 399 || loc == "" {
		return "", fmt.Errorf("%s did not redirect to a download (%s)", hostLabel(rawURL), resp.Status)
	}
	return loc, nil
}
