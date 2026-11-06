package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"zero2dev/internal/model"
)

const sha1hex = "cdbb85b45f38f282f05764ca08648b5f92db99c75b2fb3848eb4a559f6553b48"

var phpJSON = `{
 "7.4": {"version":"7.4.33","nts-vc15-x64":{"zip":{"path":"php-7.4.33-nts-Win32-vc15-x64.zip","sha256":"` + sha1hex + `"}}},
 "8.3": {"version":"8.3.14",
   "ts-vs16-x64":{"zip":{"path":"php-8.3.14-Win32-vs16-x64.zip","sha256":"` + sha1hex + `"}},
   "nts-vs16-x64":{"zip":{"path":"php-8.3.14-nts-Win32-vs16-x64.zip","sha256":"` + sha1hex + `"}},
   "source":{"path":"php-8.3.14-src.zip","sha256":"` + sha1hex + `"}},
 "8.4": {"version":"8.4.1",
   "nts-vs17-x64":{"zip":{"path":"php-8.4.1-nts-Win32-vs17-x64.zip","sha256":"` + strings.ToUpper(sha1hex) + `"}},
   "nts-vs17-x86":{"zip":{"path":"php-8.4.1-nts-Win32-vs17-x86.zip","sha256":"` + sha1hex + `"}}}
}`

func TestParsePHP(t *testing.T) {
	es, err := parsePHP([]byte(phpJSON), "https://windows.php.net/downloads/releases/")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].Version != "8.4.1" || es[1].Version != "8.3.14" {
		t.Fatalf("want 8.4.1 then 8.3.14 (7.4 is skipped): %+v", es)
	}
	e := es[0]
	if e.URL != "https://windows.php.net/downloads/releases/php-8.4.1-nts-Win32-vs17-x64.zip" || e.BinaryRelPath != "php.exe" ||
		e.StripComponents != 0 || e.OS != "windows" || e.Line != "8.4" || e.Checksum.Value != sha1hex || e.Source != "remote" {
		t.Fatalf("bad entry %+v", e)
	}
	if err := validate(&e); err != nil {
		t.Fatalf("remote entry must pass the same validation as the manifest: %v", err)
	}
	for _, bad := range []string{`not json`, `{"8.4":{"version":"8.4.1"}}`, `{}`} {
		if _, err := parsePHP([]byte(bad), "https://windows.php.net/"); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	// A downloads base on a foreign host is refused.
	if _, err := parsePHP([]byte(phpJSON), "https://evil.example/"); err == nil {
		t.Error("foreign host must be refused")
	}
}

func adoptJSON(osName, semver, link string) string {
	return fmt.Sprintf(`[{"binary":{"os":%q,"architecture":"x64","image_type":"jdk","package":{"link":%q,"checksum":%q,"name":"x"}},"version":{"major":21,"semver":%q}}]`,
		osName, link, sha1hex, semver)
}

func TestParseAdoptium(t *testing.T) {
	link := "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.5%2B11/OpenJDK21U-jdk_x64_windows_hotspot_21.0.5_11.zip"
	e, err := parseAdoptium([]byte(adoptJSON("windows", "21.0.5+11.0.LTS", link)), 21, "windows", true)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "java-21.0.5-b11-windows-amd64" || e.Version != "21.0.5+11" || e.BinaryRelPath != "bin/java.exe" || e.StripComponents != 1 ||
		e.Line != "21" || e.Channel != "lts" || e.Checksum.Type != "sha256" {
		t.Fatalf("bad entry %+v", e)
	}
	if err := validate(&e); err != nil {
		t.Fatal(err)
	}
	l, err := parseAdoptium([]byte(adoptJSON("linux", "25.0.1+8", strings.Replace(link, ".zip", ".tar.gz", 1))), 25, "linux", false)
	if err != nil || l.BinaryRelPath != "bin/java" || l.Channel != "" || l.Version != "25.0.1+8" {
		t.Fatalf("%+v %v", l, err)
	}
	if _, err := parseAdoptium([]byte(adoptJSON("windows", "21.0.5+11", "https://evil.example/x.zip")), 21, "windows", true); err == nil {
		t.Error("foreign host must be refused")
	}
	if _, err := parseAdoptium([]byte(`{"oops":1}`), 21, "windows", true); err == nil {
		t.Error("garbage must fail")
	}
}

const nginxHTML = `<h4>Mainline version</h4><a href="/download/nginx-1.27.3.tar.gz">nginx-1.27.3</a> <a href="/download/nginx-1.27.3.zip">nginx/Windows-1.27.3</a>
<h4>Stable version</h4><a href="/download/nginx-1.26.2.zip">nginx/Windows-1.26.2</a> <a href="https://nginx.org/download/nginx-1.26.2.zip">again</a>
<h4>Legacy</h4><a href="/download/nginx-1.24.0.zip">nginx/Windows-1.24.0</a>`

func TestParseNginx(t *testing.T) {
	es, err := parseNginx(nginxHTML, "https://nginx.org/download/")
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 3 || es[0].Version != "1.27.3" || es[0].Channel != "mainline" || es[1].Channel != "stable" || es[2].Version != "1.24.0" {
		t.Fatalf("%+v", es)
	}
	e := es[1]
	if !e.Unverified || e.Checksum.Type != "none" || e.URL != "https://nginx.org/download/nginx-1.26.2.zip" || e.BinaryRelPath != "nginx.exe" {
		t.Fatalf("%+v", e)
	}
	if err := validate(&e); err != nil {
		t.Fatal(err)
	}
	if _, err := parseNginx("<html>nothing</html>", "https://nginx.org/download/"); err == nil {
		t.Error("empty page must fail")
	}
	// "none" is refused for anything that is not flagged unverified.
	e.Unverified = false
	if validate(&e) == nil {
		t.Error("checksum none without unverified must be invalid")
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{{"1.26.10", "1.26.9", 1}, {"21.0.5+11", "21.0.5+9", 1}, {"8.3.14", "8.3.14", 0}, {"8.3", "8.3.0", 0}, {"7.4.33", "8.0.1", -1}, {"21.0.5+11", "21.0.4+7", 1}} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// publishers serves fake PHP, Adoptium and nginx endpoints on one test server.
func publishers(t *testing.T, failJava *bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/php/releases.json", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, phpJSON) })
	mux.HandleFunc("/nginx/download.html", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, nginxHTML) })
	mux.HandleFunc("/v3/info/available_releases", func(w http.ResponseWriter, r *http.Request) {
		if *failJava {
			http.Error(w, "boom", 503)
			return
		}
		fmt.Fprint(w, `{"available_lts_releases":[17,21],"most_recent_feature_release":24}`)
	})
	mux.HandleFunc("/v3/assets/latest/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/") // "", v3, assets, latest, <feat>, hotspot
		feat, osName := parts[4], r.URL.Query().Get("os")
		ext := ".zip"
		if osName == "linux" {
			ext = ".tar.gz"
		}
		link := "http://127.0.0.1/jdk-" + feat + ext // loopback http is allowed for tests
		fmt.Fprintf(w, `[{"binary":{"os":%q,"architecture":"x64","image_type":"jdk","package":{"link":%q,"checksum":%q}},"version":{"major":%s,"semver":"%s.0.1+9.0.LTS"}}]`,
			osName, link, sha1hex, feat, feat)
	})
	mux.HandleFunc("/node/index.json", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, nodeJSON) })
	mux.HandleFunc("/go/dl.json", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, goJSON) })
	mux.HandleFunc("/caddy/releases.json", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, caddyJSON) })
	mux.HandleFunc("/php/archive/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "sha256sum.txt") {
			fmt.Fprintf(w, "%s *php-7.4.33-nts-Win32-vc15-x64.zip\n", sha1hex)
			return
		}
		fmt.Fprint(w, phpArchiveHTML)
	})
	var s *httptest.Server
	toolPublishers(mux, func() string { return s.URL })
	s = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func newRemote(home string, s *httptest.Server) *Remote {
	return &Remote{Home: home, Version: "test",
		PHPURL: s.URL + "/php/releases.json", AdoptiumURL: s.URL, NginxPage: s.URL + "/nginx/download.html", NginxBase: s.URL + "/nginx/dl/",
		NodeURL: s.URL + "/node/index.json", NodeBase: s.URL + "/node/dist/", GoURL: s.URL + "/go/dl.json", GoBase: s.URL + "/go/files/",
		CaddyURL: s.URL + "/caddy/releases.json", PHPArchiveURL: s.URL + "/php/archive/",
		MariaDBURL: s.URL + "/mariadb/", PostgresPage: s.URL + "/pg/page.html", VSCodeURL: s.URL + "/vscode/latest",
		GitHubAPI: s.URL + "/gh/"}
}

func TestRefreshCachesAndMerges(t *testing.T) {
	fail := false
	s := publishers(t, &fail)
	home := t.TempDir()
	r := newRemote(home, s)

	if st := r.Status(); st.CheckedAt != nil || len(st.Sources) != 0 {
		t.Fatalf("fresh status must be empty: %+v", st)
	}
	st, added, err := r.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.CheckedAt == nil || len(st.Sources) != 6+3+len(ghSpecs) {
		t.Fatalf("status %+v", st)
	}
	for _, src := range st.Sources {
		if !src.OK || src.Count == 0 || src.Error != "" {
			t.Errorf("source %+v", src)
		}
	}
	// nginx 1.26.2 and 1.27.3 are in the embedded manifest, so only 1.24.0 is new for nginx.
	has := func(id string) bool {
		for _, a := range added {
			if a == id {
				return true
			}
		}
		return false
	}
	if has("nginx-1.26.2-windows-amd64") || !has("nginx-1.24.0-windows-amd64") || !has("php-8.4.1-windows-amd64") || !has("java-21.0.1-b9-linux-amd64") ||
		!has("php-7.1.33-windows-amd64") || !has("node-20.18.0-windows-amd64") || !has("go-1.22.9-linux-amd64") || !has("caddy-2.7.6-windows-amd64") ||
		has("node-22.11.0-windows-amd64") || has("go-1.23.3-windows-amd64") || has("caddy-2.8.4-windows-amd64") {
		t.Fatalf("added = %v", added)
	}
	es, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, e := range es {
		counts[e.Kind]++
		if seen[e.ID] {
			t.Errorf("duplicate id %s after merge", e.ID)
		}
		seen[e.ID] = true
	}
	// php: 2 current + 2 archived lines; java: 3 versions x 2 OS; node/go/caddy: the 2 built-in entries plus 2 new lines x 2 OS.
	if counts["php"] != 4 || counts["java"] != 6 || counts["nginx"] != 3 || counts["node"] != 6 || counts["go"] != 4 || counts["caddy"] != 4 {
		t.Fatalf("counts %v", counts)
	}
	// A second refresh adds nothing new.
	if _, added2, _ := r.Refresh(context.Background()); len(added2) != 0 {
		t.Fatalf("second refresh added %v", added2)
	}
}

func TestRefreshKeepsOldEntriesWhenASourceFails(t *testing.T) {
	fail := false
	s := publishers(t, &fail)
	home := t.TempDir()
	r := newRemote(home, s)
	if _, _, err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail = true
	st, _, err := r.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var java SourceStatus
	for _, x := range st.Sources {
		if x.Kind == "java" {
			java = x
		}
	}
	if java.OK || java.Error == "" || !strings.Contains(java.Error, "503") {
		t.Fatalf("java must report the failure: %+v", java)
	}
	es, _ := Load(home)
	n := 0
	for _, e := range es {
		if e.Kind == "java" {
			n++
		}
	}
	if n != 6 {
		t.Fatalf("old java entries must be kept, have %d", n)
	}
	// Everything unreachable: still no error, checkedAt unchanged.
	s.Close()
	st2, _, err := r.Refresh(context.Background())
	if err != nil || st2.CheckedAt == nil || !st2.CheckedAt.Equal(*st.CheckedAt) {
		t.Fatalf("%+v %v", st2, err)
	}
	for _, x := range st2.Sources {
		if x.OK || x.Error == "" || strings.Contains(x.Error, "http://") {
			t.Errorf("unreachable source must show a clean error: %+v", x)
		}
	}
}

func TestDamagedOrHostileCacheIsIgnored(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(home+"/cache", 0o755)
	os.WriteFile(home+"/cache/remote-catalog.json", []byte("{not json"), 0o644)
	builtin, _ := Parse(defaultManifest)
	if es, err := Load(home); err != nil || len(es) != len(builtin) {
		t.Fatalf("damaged cache must be ignored: %d %v", len(es), err)
	}
	// An entry that would not pass validation (plain http to a non-loopback host) is dropped.
	bad := `{"status":{"sources":[]},"entries":[{"id":"php-9.9.9-windows-amd64","kind":"php","version":"9.9.9","name":"x","url":"http://evil.example/php.zip","checksum":{"type":"sha256","value":"` + sha1hex + `"},"binaryRelPath":"php.exe","os":"windows","arch":"amd64"}]}`
	os.WriteFile(home+"/cache/remote-catalog.json", []byte(bad), 0o644)
	es, _ := Load(home)
	if len(es) != len(builtin) {
		t.Fatalf("hostile entry must be dropped, have %d", len(es))
	}
}

func TestAnnotateLatestPerLine(t *testing.T) {
	mk := func(id, v, line string) model.CatalogEntry {
		return model.CatalogEntry{ID: id, Kind: "java", Version: v, Line: line, OS: "linux", Arch: "amd64", URL: "https://x/a.zip", BinaryRelPath: "bin/java"}
	}
	out := Annotate([]model.CatalogEntry{mk("a", "21.0.4+7", "21"), mk("b", "21.0.5+11", "21"), mk("c", "17.0.13+11", "17"), mk("d", "25.0.1+8", "25")}, t.TempDir(), nil)
	got := map[string]bool{}
	order := []string{}
	for _, e := range out {
		got[e.ID] = e.Latest
		order = append(order, e.ID)
	}
	if got["a"] || !got["b"] || !got["c"] || !got["d"] {
		t.Fatalf("latest flags %v", got)
	}
	if strings.Join(order, "") != "dbac" {
		t.Fatalf("newest first expected, got %v", order)
	}
}

const nodeJSON = `[
 {"version":"v23.1.0","lts":false,"files":["win-x64-zip","linux-x64"]},
 {"version":"v22.11.0","lts":"Jod","files":["win-x64-zip","linux-x64","osx-x64-tar"]},
 {"version":"v22.10.0","lts":false,"files":["win-x64-zip","linux-x64"]},
 {"version":"v20.18.0","lts":"Iron","files":["win-x64-zip","linux-x64"]},
 {"version":"v12.22.12","lts":"Erbium","files":["win-x64-zip","linux-x64"]},
 {"version":"v0.12.18","lts":false,"files":["win-x64-zip"]}
]`

func goFile(name, os string) string {
	return fmt.Sprintf(`{"filename":%q,"os":%q,"arch":"amd64","sha256":%q,"kind":"archive"}`, name, os, sha1hex)
}

var goJSON = `[
 {"version":"go1.24rc1","stable":false,"files":[` + goFile("go1.24rc1.linux-amd64.tar.gz", "linux") + `]},
 {"version":"go1.23.3","stable":true,"files":[` + goFile("go1.23.3.windows-amd64.zip", "windows") + `,` + goFile("go1.23.3.linux-amd64.tar.gz", "linux") + `,{"filename":"go1.23.3.windows-amd64.msi","os":"windows","arch":"amd64","sha256":"` + sha1hex + `","kind":"installer"}]},
 {"version":"go1.23.2","stable":true,"files":[` + goFile("go1.23.2.windows-amd64.zip", "windows") + `]},
 {"version":"go1.22.9","stable":true,"files":[` + goFile("go1.22.9.windows-amd64.zip", "windows") + `,` + goFile("go1.22.9.linux-amd64.tar.gz", "linux") + `]}
]`

func ghAssets(ver string) string {
	b := "https://github.com/caddyserver/caddy/releases/download/v" + ver + "/"
	return fmt.Sprintf(`[{"name":"caddy_%[1]s_windows_amd64.zip","browser_download_url":"%[2]scaddy_%[1]s_windows_amd64.zip"},
	 {"name":"caddy_%[1]s_linux_amd64.tar.gz","browser_download_url":"%[2]scaddy_%[1]s_linux_amd64.tar.gz"},
	 {"name":"caddy_%[1]s_checksums.txt","browser_download_url":"%[2]scaddy_%[1]s_checksums.txt"}]`, ver, b)
}

var caddyJSON = `[
 {"tag_name":"v2.9.0-beta.1","prerelease":true,"assets":` + ghAssets("2.9.0-beta.1") + `},
 {"tag_name":"v2.8.4","assets":` + ghAssets("2.8.4") + `},
 {"tag_name":"v2.8.3","assets":` + ghAssets("2.8.3") + `},
 {"tag_name":"v2.7.6","assets":` + ghAssets("2.7.6") + `}
]`

const phpArchiveHTML = `<a href="php-7.4.32-nts-Win32-vc15-x64.zip">a</a> <a href="/downloads/releases/archives/php-7.4.33-nts-Win32-vc15-x64.zip">b</a>
<a href="php-7.1.33-nts-Win32-VC14-x64.zip">c</a> <a href="php-7.1.33-Win32-VC14-x64.zip">ts build, ignored</a> <a href="php-7.1.33-nts-Win32-VC14-x86.zip">x86</a>
<a href="php-8.4.1-nts-Win32-vs17-x64.zip">current line, skipped</a>`

func TestParseNode(t *testing.T) {
	es, err := parseNode([]byte(nodeJSON), "https://nodejs.org/dist/")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]model.CatalogEntry{}
	for _, e := range es {
		got[e.ID] = e
	}
	if len(es) != 6 || got["node-22.11.0-windows-amd64"].Version == "" || got["node-22.10.0-windows-amd64"].ID != "" || got["node-12.22.12-windows-amd64"].ID != "" {
		t.Fatalf("newest patch of each major >= 14 only: %v", len(es))
	}
	w, l := got["node-22.11.0-windows-amd64"], got["node-23.1.0-linux-amd64"]
	if w.URL != "https://nodejs.org/dist/v22.11.0/node-v22.11.0-win-x64.zip" || w.Checksum.Type != "shasums" || w.Checksum.URL != "https://nodejs.org/dist/v22.11.0/SHASUMS256.txt" ||
		w.BinaryRelPath != "node.exe" || w.StripComponents != 1 || w.Line != "22" || w.Channel != "lts" || l.Channel != "" || l.BinaryRelPath != "bin/node" || l.Line != "23" {
		t.Fatalf("%+v / %+v", w, l)
	}
	// Same id and URL as the built-in manifest entry, so the merge dedupes it.
	builtin, _ := Parse(defaultManifest)
	for _, b := range builtin {
		if b.ID == w.ID && (b.URL != w.URL || b.Checksum.URL != w.Checksum.URL) {
			t.Fatalf("remote and built-in node entry differ: %+v vs %+v", b, w)
		}
	}
	for _, e := range es {
		if err := validate(&e); err != nil {
			t.Fatalf("%s: %v", e.ID, err)
		}
	}
	if _, err := parseNode([]byte(`{"x":1}`), "https://nodejs.org/dist/"); err == nil {
		t.Error("garbage must fail")
	}
}

func TestParseGo(t *testing.T) {
	es, err := parseGo([]byte(goJSON), "https://go.dev/dl/")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, e := range es {
		ids = append(ids, e.ID)
		if err := validate(&e); err != nil {
			t.Fatalf("%s: %v", e.ID, err)
		}
	}
	want := "go-1.23.3-windows-amd64 go-1.23.3-linux-amd64 go-1.22.9-windows-amd64 go-1.22.9-linux-amd64"
	if strings.Join(ids, " ") != want { // rc skipped, 1.23.2 superseded, msi skipped
		t.Fatalf("ids %v", ids)
	}
	if e := es[0]; e.URL != "https://go.dev/dl/go1.23.3.windows-amd64.zip" || e.Checksum.Type != "sha256" || e.BinaryRelPath != "bin/go.exe" || e.Line != "1.23" || e.StripComponents != 1 {
		t.Fatalf("%+v", e)
	}
}

func TestParseCaddy(t *testing.T) {
	es, err := parseCaddy([]byte(caddyJSON))
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, e := range es {
		ids = append(ids, e.ID)
		if err := validate(&e); err != nil {
			t.Fatalf("%s: %v", e.ID, err)
		}
	}
	if strings.Join(ids, " ") != "caddy-2.8.4-windows-amd64 caddy-2.8.4-linux-amd64 caddy-2.7.6-windows-amd64 caddy-2.7.6-linux-amd64" { // beta skipped, 2.8.3 superseded
		t.Fatalf("ids %v", ids)
	}
	e := es[0]
	builtin, _ := Parse(defaultManifest)
	for _, b := range builtin {
		if b.ID == e.ID && (b.URL != e.URL || b.Checksum.URL != e.Checksum.URL || b.BinaryRelPath != e.BinaryRelPath) {
			t.Fatalf("remote and built-in caddy entry differ:\n%+v\n%+v", b, e)
		}
	}
	bad := strings.ReplaceAll(caddyJSON, "https://github.com/", "https://evil.example/")
	if _, err := parseCaddy([]byte(bad)); err == nil {
		t.Error("foreign host must be refused")
	}
}

func TestParsePHPArchive(t *testing.T) {
	sums := parsePHPSums(sha1hex + " *php-7.4.33-nts-Win32-vc15-x64.zip\r\nphp-7.0.1-nts-Win32-vc14-x64.zip  " + strings.ToUpper(sha1hex) + "\n# comment\n")
	if sums["php-7.4.33-nts-Win32-vc15-x64.zip"] != sha1hex || sums["php-7.0.1-nts-Win32-vc14-x64.zip"] != sha1hex {
		t.Fatalf("sums %v", sums)
	}
	es, err := parsePHPArchive(phpArchiveHTML, "https://windows.php.net/downloads/releases/archives/", sums, map[string]bool{"8.4": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].Version != "7.4.33" || es[1].Version != "7.1.33" {
		t.Fatalf("newest patch per old line, TS/x86 and current lines ignored: %+v", es)
	}
	if es[0].Unverified || es[0].Checksum.Type != "sha256" || es[0].Checksum.Value != sha1hex {
		t.Fatalf("7.4 has a published hash: %+v", es[0])
	}
	if !es[1].Unverified || es[1].Checksum.Type != "none" || es[1].Line != "7.1" {
		t.Fatalf("7.1 has none: %+v", es[1])
	}
	for _, e := range es {
		if err := validate(&e); err != nil {
			t.Fatalf("%s: %v", e.ID, err)
		}
	}
	if _, err := parsePHPArchive(phpArchiveHTML, "https://evil.example/", nil, nil); err == nil {
		t.Error("foreign host must be refused")
	}
}

func TestGetRefusesTruncatedDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 3<<20)))
	}))
	defer srv.Close()
	r := &Remote{}
	if _, err := r.getN(context.Background(), srv.URL, 1<<20); err == nil || !strings.Contains(err.Error(), "more than 1 MB") {
		t.Fatalf("an oversized answer must be an error, not a cut-off body: %v", err)
	}
	if b, err := r.getN(context.Background(), srv.URL, 4<<20); err != nil || len(b) != 3<<20 {
		t.Fatalf("%d %v", len(b), err)
	}
}

func TestRefreshDoesNotRecopyFetchedKinds(t *testing.T) {
	fail := false
	s := publishers(t, &fail)
	home := t.TempDir()
	r := newRemote(home, s)
	r.NodeURL = s.URL + "/missing"
	old := cacheFile{Entries: []model.CatalogEntry{
		{ID: "old-node", Kind: "node"},
		{ID: "old-go", Kind: "go"},
		{ID: "old-caddy", Kind: "caddy"},
		{ID: "future", Kind: "future"},
	}}
	if err := saveCache(home, old); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, e := range loadCache(home).Entries {
		counts[e.ID]++
	}
	if counts["old-node"] != 1 || counts["old-go"] != 0 || counts["old-caddy"] != 0 || counts["future"] != 1 {
		t.Fatalf("incorrect fallback entries: %v", counts)
	}
}
