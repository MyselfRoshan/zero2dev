package catalog

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"zero2dev/internal/model"
)

// Fixtures trimmed from real answers (2026-11): only the fields the parsers read.

const mariaMajorsJSON = `{"major_releases":[
{"release_id":"13.2","release_status":"Preview","release_support_type":null},
{"release_id":"13.1","release_status":"RC","release_support_type":"Rolling"},
{"release_id":"13.0","release_status":"Stable","release_support_type":"Rolling"},
{"release_id":"12.3","release_status":"Stable","release_support_type":"Long Term Support"},
{"release_id":"11.8","release_status":"Stable","release_support_type":"Long Term Support"},
{"release_id":"11.4","release_status":"Stable","release_support_type":"Long Term Support"},
{"release_id":"10.11","release_status":"Stable","release_support_type":"Long Term Support"}]}`

func mariaLatestJSON(line, ver string) string {
	return fmt.Sprintf(`{"releases":{%[2]q:{"release_id":%[2]q,"files":[
{"file_name":"mariadb-%[2]s-winx64-debugsymbols.zip","package_type":"ZIP file","os":"Windows","cpu":"x86_64","file_download_url":"http://downloads.mariadb.org/rest-api/mariadb/%[2]s/mariadb-%[2]s-winx64-debugsymbols.zip","checksum":{"sha256sum":"%[3]s"}},
{"file_name":"mariadb-%[2]s-winx64.msi","package_type":"MSI Package","os":"Windows","cpu":"x86_64","file_download_url":"http://downloads.mariadb.org/rest-api/mariadb/%[2]s/mariadb-%[2]s-winx64.msi","checksum":{"sha256sum":"%[3]s"}},
{"file_name":"mariadb-%[2]s-winx64.zip","package_type":"ZIP file","os":"Windows","cpu":"x86_64","file_download_url":"http://downloads.mariadb.org/rest-api/mariadb/%[2]s/mariadb-%[2]s-winx64.zip","checksum":{"sha256sum":"d62986d433eeebfde218560b276103831604a61e929e87f1a17f5aebd80257e2"}},
{"file_name":"mariadb-%[2]s-linux-systemd-x86_64.tar.gz","package_type":"gzipped tar file","os":"Linux","cpu":"x86_64","file_download_url":"http://downloads.mariadb.org/x.tar.gz","checksum":{"sha256sum":"%[3]s"}}]}}}`,
		line, ver, strings.Repeat("a", 64))
}

const vscodeJSON = `{"url":"https://vscode.download.prss.microsoft.com/dbazure/download/stable/a717861c07586a53d83b80d742cd3f59b20a6273/VSCode-win32-x64-1.145.0.zip","name":"1.145.0","productVersion":"1.145.0","sha256hash":"79cf4cc5b17357cc64abdd22a2d7d4884d05f07b3f0defbf5fc0d67fe03d9dc4","hash":"359e091af0b497719c5286ed7a350040d5262735"}`

// postgresHTML mimics the EnterpriseDB page: one block per version, the Windows
// link is not always the first one in its block.
func postgresHTML(base string) string {
	block := func(ver, win, mac string, macFirst bool) string {
		w := `<div class="m-5"><a href="` + base + `/getfile.jsp?fileid=` + win + `"><img alt="Windows x86-64" src="w.gif"/></a></div>`
		m := `<div class="m-5"><a href="` + base + `/getfile.jsp?fileid=` + mac + `"><img alt="Mac OS X" src="m.gif"/></a></div>`
		if macFirst {
			w, m = m, w
		}
		return `<div class="italic">Binaries from installer<span class="font-semibold pl-1">Version <!-- -->` + ver + `</span><div>` + w + m + `</div></div>`
	}
	return "<html>" + block("18.6", "1260609", "1260613", false) + block("17.11", "1260616", "1260620", false) +
		block("16.15", "1260623", "1260627", true) + "</html>"
}

var pgRedirects = map[string]string{
	"1260609": "https://get.enterprisedb.com/postgresql/postgresql-18.6-1-windows-x64-binaries.zip",
	"1260616": "https://get.enterprisedb.com/postgresql/postgresql-17.11-5-windows-x64-binaries.zip",
	"1260623": "https://get.enterprisedb.com/postgresql/postgresql-16.15-2-windows-x64-binaries.zip",
}

func ghJSON(assets ...string) string {
	return "[" + strings.Join(assets, ",") + "]"
}

func ghRel(tag string, pre bool, name, digest string) string {
	d := ""
	if digest != "" {
		d = `,"digest":"sha256:` + digest + `"`
	}
	return fmt.Sprintf(`{"tag_name":%q,"draft":false,"prerelease":%t,"assets":[{"name":"notes.txt","browser_download_url":"https://github.com/x/notes.txt"},{"name":%q,"browser_download_url":"https://github.com/o/r/releases/download/%s/%s"%s}]}`,
		tag, pre, name, tag, name, d)
}

var ghFixtures = map[string]string{
	"redis-windows/redis-windows": ghJSON(
		ghRel("8.10.3", false, "Redis-8.10.3-Windows-x64-msys2.zip", strings.Repeat("1", 64)),
		ghRel("8.10.2", false, "Redis-8.10.2-Windows-x64-msys2.zip", strings.Repeat("2", 64)),
		ghRel("8.8.3", false, "Redis-8.8.3-Windows-x64-msys2.zip", strings.Repeat("3", 64)),
		ghRel("8.6.0", false, "Redis-8.6.0-Windows-x64-msys2.zip", strings.Repeat("4", 64))),
	"VSCodium/vscodium": ghJSON(
		ghRel("1.144.00001", true, "VSCodium-win32-x64-1.144.00001.zip", strings.Repeat("5", 64)), // insider: skipped
		ghRel("1.143.17232", false, "VSCodium-win32-x64-1.143.17232.zip", strings.Repeat("6", 64))),
	"notepad-plus-plus/notepad-plus-plus": ghJSON(
		ghRel("v8.9.8.1", false, "npp.8.9.8.1.portable.x64.zip", strings.Repeat("7", 64))),
	"HeidiSQL/HeidiSQL": ghJSON(
		ghRel("v12.22", false, "HeidiSQL_12.22_64_Portable.zip", "7b36821fa3e16cc37b511d41747efa4c8b980d1e3fa937e52c5931bcc4ee79b4")),
	"dbeaver/dbeaver": ghJSON(
		ghRel("26.2.4", false, "dbeaver-ce-26.2.4-windows-x86_64.zip", "e667f516d64f29b87e3571203fc3eb02f0a419b9ff66865043bf9954ebc230c6"),
		ghRel("26.2.3", false, "dbeaver-ce-26.2.3-windows-x86_64.zip", "")), // no digest: unverified
}

// toolPublishers adds the 0.0.3 sources to the fake publisher server.
func toolPublishers(mux *http.ServeMux, base func() string) {
	mux.HandleFunc("/mariadb/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/mariadb/")
		if rest == "" {
			fmt.Fprint(w, mariaMajorsJSON)
			return
		}
		line := strings.TrimSuffix(strings.TrimSuffix(rest, "/"), "/latest")
		fmt.Fprint(w, mariaLatestJSON(line, line+".9"))
	})
	mux.HandleFunc("/vscode/latest", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, vscodeJSON) })
	mux.HandleFunc("/pg/page.html", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, postgresHTML(base())) })
	mux.HandleFunc("/getfile.jsp", func(w http.ResponseWriter, r *http.Request) {
		to, ok := pgRedirects[r.URL.Query().Get("fileid")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, to, http.StatusFound)
	})
	mux.HandleFunc("/gh/", func(w http.ResponseWriter, r *http.Request) {
		repo := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/gh/"), "/releases")
		body, ok := ghFixtures[repo]
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	})
}

func specFor(kind string) ghSpec {
	for _, s := range ghSpecs {
		if s.kind == kind {
			return s
		}
	}
	panic(kind)
}

func TestParseGitHubTools(t *testing.T) {
	for _, c := range []struct {
		kind, repo string
		ids        []string
		bin        string
		strip      int
	}{
		{"redis", "redis-windows/redis-windows", []string{"redis-8.10.3-windows-amd64", "redis-8.10.2-windows-amd64", "redis-8.8.3-windows-amd64"}, "redis-server.exe", 1},
		{"vscodium", "VSCodium/vscodium", []string{"vscodium-1.143.17232-windows-amd64"}, "VSCodium.exe", 0},
		{"notepadpp", "notepad-plus-plus/notepad-plus-plus", []string{"notepadpp-8.9.8.1-windows-amd64"}, "notepad++.exe", 0},
		{"heidisql", "HeidiSQL/HeidiSQL", []string{"heidisql-12.22-windows-amd64"}, "heidisql.exe", 0},
		{"dbeaver", "dbeaver/dbeaver", []string{"dbeaver-26.2.4-windows-amd64", "dbeaver-26.2.3-windows-amd64"}, "dbeaver.exe", 1},
	} {
		es, err := parseGitHub([]byte(ghFixtures[c.repo]), specFor(c.kind))
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		ids := []string{}
		for _, e := range es {
			ids = append(ids, e.ID)
			if err := validate(&e); err != nil {
				t.Fatalf("%s: %v", e.ID, err)
			}
			if e.BinaryRelPath != c.bin || e.StripComponents != c.strip || e.OS != "windows" {
				t.Fatalf("%s: %+v", e.ID, e)
			}
		}
		if strings.Join(ids, " ") != strings.Join(c.ids, " ") {
			t.Fatalf("%s: ids %v, want %v", c.kind, ids, c.ids)
		}
	}
	db, _ := parseGitHub([]byte(ghFixtures["dbeaver/dbeaver"]), specFor("dbeaver"))
	if db[0].Checksum.Type != "sha256" || db[0].Checksum.Value != "e667f516d64f29b87e3571203fc3eb02f0a419b9ff66865043bf9954ebc230c6" || db[0].Unverified {
		t.Fatalf("digest must become the sha256: %+v", db[0])
	}
	if db[1].Checksum.Type != "none" || !db[1].Unverified {
		t.Fatalf("no digest: unverified: %+v", db[1])
	}
	bad := strings.ReplaceAll(ghFixtures["HeidiSQL/HeidiSQL"], "https://github.com/o/", "https://evil.example/o/")
	if _, err := parseGitHub([]byte(bad), specFor("heidisql")); err == nil {
		t.Fatal("foreign host must be refused")
	}
	if _, err := parseGitHub([]byte(`[]`), specFor("heidisql")); err == nil {
		t.Fatal("no asset: error")
	}
}

func TestParseVSCode(t *testing.T) {
	es, err := parseVSCode([]byte(vscodeJSON))
	if err != nil {
		t.Fatal(err)
	}
	e := es[0]
	if e.ID != "vscode-1.145.0-windows-amd64" || e.BinaryRelPath != "Code.exe" || e.Checksum.Value != "79cf4cc5b17357cc64abdd22a2d7d4884d05f07b3f0defbf5fc0d67fe03d9dc4" {
		t.Fatalf("%+v", e)
	}
	if err := validate(&e); err != nil {
		t.Fatal(err)
	}
	if _, err := parseVSCode([]byte(strings.Replace(vscodeJSON, "vscode.download.prss.microsoft.com", "evil.example", 1))); err == nil {
		t.Fatal("foreign host must be refused")
	}
	if _, err := parseVSCode([]byte(`{"url":"https://x.visualstudio.com/a.zip","productVersion":"1.0.0"}`)); err == nil {
		t.Fatal("missing checksum must be refused")
	}
}

func TestParseMariaDB(t *testing.T) {
	lines, err := parseMariaMajors([]byte(mariaMajorsJSON))
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, l := range lines {
		got = append(got, fmt.Sprintf("%s/%t", l.id, l.lts))
	}
	// preview and RC skipped; newest rolling + LTS lines, at most four
	if strings.Join(got, " ") != "13.0/false 12.3/true 11.8/true 11.4/true" {
		t.Fatalf("lines %v", got)
	}
	e, err := parseMariaLatest([]byte(mariaLatestJSON("11.4", "11.4.13")), "11.4", true)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "mariadb-11.4.13-windows-amd64" || e.URL != "https://downloads.mariadb.org/rest-api/mariadb/11.4.13/mariadb-11.4.13-winx64.zip" ||
		e.Checksum.Value != "d62986d433eeebfde218560b276103831604a61e929e87f1a17f5aebd80257e2" || e.Channel != "lts" || e.Line != "11.4" ||
		e.BinaryRelPath != "bin/mariadbd.exe" || e.StripComponents != 1 {
		t.Fatalf("%+v", e)
	}
	if err := validate(&e); err != nil {
		t.Fatal(err)
	}
}

func TestParsePostgres(t *testing.T) {
	links := parsePostgresPage(postgresHTML("https://sbp.enterprisedb.com"))
	got := []string{}
	for _, l := range links {
		got = append(got, l.version+"="+l.link[strings.LastIndex(l.link, "=")+1:])
	}
	// 16.15 lists the Mac link first: the Windows one is still found
	if strings.Join(got, " ") != "18.6=1260609 17.11=1260616 16.15=1260623" {
		t.Fatalf("links %v", got)
	}
	e, err := postgresEntry(pgRedirects["1260616"], "17.11")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "postgres-17.11-windows-amd64" || !e.Unverified || e.Line != "17" || e.BinaryRelPath != "bin/postgres.exe" {
		t.Fatalf("%+v", e)
	}
	if err := validate(&e); err != nil {
		t.Fatal(err)
	}
	if _, err := postgresEntry("https://evil.example/postgresql-17.11-5-windows-x64-binaries.zip", "17.11"); err == nil {
		t.Fatal("unexpected host must be refused")
	}
	if _, err := postgresEntry(pgRedirects["1260609"], "17.11"); err == nil {
		t.Fatal("version mismatch must be refused")
	}
}

func TestToolSourcesRefreshOffline(t *testing.T) {
	fail := false
	s := publishers(t, &fail)
	home := t.TempDir()
	r := newRemote(home, s)
	st, _, err := r.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range st.Sources {
		if !src.OK || src.Error != "" {
			t.Errorf("source %+v", src)
		}
		if strings.Contains(src.Name, "github.com/") == false && !strings.HasPrefix(src.Name, "127.0.0.1") {
			t.Errorf("source %s must point at the test server", src.Name)
		}
	}
	es, _ := Load(home)
	// After the merge (built-in entries win on equal ids): MariaDB has 4 fetched lines plus the built-in
	// 11.4.13; the built-in PostgreSQL 17.11, Redis 8.10.3, VS Code and the GitHub tools coincide with fetched
	// ones; MySQL comes only from the built-in manifest.
	counts := map[string]int{}
	for _, e := range es {
		counts[e.Kind]++
	}
	want := map[string]int{"mariadb": 5, "mysql": 2, "postgres": 3, "vscode": 1, "redis": 3, "vscodium": 1, "notepadpp": 1, "heidisql": 1, "dbeaver": 2}
	for k, n := range want {
		if counts[k] != n {
			t.Errorf("%s: %d remote entries, want %d (%v)", k, counts[k], n, counts)
		}
	}
	for _, e := range es {
		if model.In(e.Kind, []string{"mariadb", "postgres", "vscode"}) && e.Source == "remote" && strings.Contains(e.URL, "127.0.0.1") {
			t.Errorf("%s: download link must be the publisher's, got %s", e.ID, e.URL)
		}
	}
}
