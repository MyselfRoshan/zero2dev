package api

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/catalog"
	"zero2dev/internal/model"
)

func tinyZip(t *testing.T, name, body string) ([]byte, string) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	w.Write([]byte(body))
	zw.Close()
	h := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(h[:])
}

func waitJob(t *testing.T, e *tenv, id string) model.Job {
	t.Helper()
	for i := 0; i < 300; i++ {
		var j model.Job
		// there is no GET /api/jobs here; poll the runtimes list instead via the installer
		if cur, ok := e.srv.Installer.Get(id); ok && cur.State != "running" {
			return cur
		}
		_ = j
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return model.Job{}
}

func TestStatusRefreshAndMergedCatalogAPI(t *testing.T) {
	e := newEnv(t)
	var st catalog.Status
	e.do("GET", "/api/catalog/status", nil, &st)
	if st.CheckedAt != nil || st.Sources == nil || len(st.Sources) != 0 {
		t.Fatalf("fresh status: %+v", st)
	}
	// No remote configured: refresh is refused cleanly.
	e.errCode("POST", "/api/catalog/refresh", nil, 409)

	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/php.json":
			fmt.Fprintf(w, `{"8.4":{"version":"8.4.1","nts-vs17-x64":{"zip":{"path":"php-8.4.1-nts-Win32-vs17-x64.zip","sha256":%q}}}}`, strings.Repeat("a", 64))
		case r.URL.Path == "/ng.html":
			fmt.Fprint(w, `<a href="/download/nginx-1.27.3.zip">x</a><a href="/download/nginx-1.28.0.zip">y</a>`)
		default:
			http.Error(w, "down", 500) // java unreachable
		}
	}))
	defer pub.Close()
	e.srv.Remote = &catalog.Remote{Home: e.home, PHPURL: pub.URL + "/php.json", AdoptiumURL: pub.URL, NginxPage: pub.URL + "/ng.html", NginxBase: pub.URL + "/dl/",
		NodeURL: pub.URL + "/node.json", NodeBase: pub.URL + "/node/", GoURL: pub.URL + "/go.json", GoBase: pub.URL + "/go/",
		CaddyURL: pub.URL + "/caddy.json", PHPArchiveURL: pub.URL + "/archive/"}

	var out struct {
		catalog.Status
		Added []string `json:"added"`
	}
	if resp := e.do("POST", "/api/catalog/refresh", nil, &out); resp.StatusCode != 200 {
		t.Fatalf("refresh status %d", resp.StatusCode)
	}
	if out.CheckedAt == nil || len(out.Sources) != 6 || strings.Join(out.Added, ",") != "nginx-1.28.0-windows-amd64,php-8.4.1-windows-amd64" {
		t.Fatalf("%+v", out)
	}
	bySrc := map[string]catalog.SourceStatus{}
	for _, s := range out.Sources {
		bySrc[s.Kind] = s
	}
	if !bySrc["php"].OK || !bySrc["nginx"].OK || bySrc["java"].OK || bySrc["java"].Error == "" || bySrc["node"].OK {
		t.Fatalf("per-source results: %+v", bySrc)
	}

	var again catalog.Status
	e.do("GET", "/api/catalog/status", nil, &again)
	if again.CheckedAt == nil || len(again.Sources) != 6 {
		t.Fatalf("status must persist: %+v", again)
	}
	var es []model.CatalogEntry
	e.do("GET", "/api/catalog", nil, &es)
	var php, ngx *model.CatalogEntry
	for i := range es {
		switch es[i].ID {
		case "php-8.4.1-windows-amd64":
			php = &es[i]
		case "nginx-1.28.0-windows-amd64":
			ngx = &es[i]
		}
	}
	if php == nil || ngx == nil {
		t.Fatalf("remote entries must be in the catalog: %d entries", len(es))
	}
	if php.Source != "remote" || !php.Latest || php.Line != "8.4" || php.Supported == (runtime.GOOS != "windows") {
		t.Fatalf("php flags %+v", php)
	}
	if !ngx.Unverified || ngx.Channel != "stable" || !ngx.Latest || ngx.Checksum.Type != "none" {
		t.Fatalf("nginx flags %+v", ngx)
	}
	// Newest first inside a kind.
	var nv []string
	for _, x := range es {
		if x.Kind == "nginx" {
			nv = append(nv, x.Version)
		}
	}
	if strings.Join(nv, " ") != "1.28.0 1.27.3 1.26.2" {
		t.Fatalf("nginx order %v", nv)
	}
}

func TestManyVersionsAndDefaultAPIFlow(t *testing.T) {
	e := newEnv(t)
	z1, s1 := tinyZip(t, "php", "v1")
	z2, s2 := tinyZip(t, "php", "v2")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p1.zip":
			w.Write(z1)
		case "/p2.zip":
			w.Write(z2)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	entry := func(id, ver, file, sha string) map[string]any {
		return map[string]any{"id": id, "kind": "php", "version": ver, "name": "PHP " + ver, "url": srv.URL + file, "line": "8", "checksum": map[string]any{"type": "sha256", "value": sha},
			"binaryRelPath": "php", "stripComponents": 0, "os": runtime.GOOS, "arch": runtime.GOARCH, "homepage": "https://example.invalid/"}
	}
	b, _ := json.Marshal([]any{entry("php-7.1.33", "7.1.33", "/p1.zip", s1), entry("php-8.4.0", "8.4.0", "/p2.zip", s2)})
	os.WriteFile(filepath.Join(e.home, "catalog.json"), b, 0o644)

	install := func(id string) model.Runtime {
		var j model.Job
		if r := e.do("POST", "/api/runtimes/install", map[string]any{"catalogId": id}, &j); r.StatusCode != 202 {
			t.Fatalf("install %s: status %d", id, r.StatusCode)
		}
		if done := waitJob(t, e, j.ID); done.State != "done" {
			t.Fatalf("%+v", done)
		}
		var rs []model.Runtime
		e.do("GET", "/api/runtimes", nil, &rs)
		for _, r := range rs {
			if r.CatalogID == id {
				return r
			}
		}
		t.Fatalf("%s not registered", id)
		return model.Runtime{}
	}
	old := install("php-7.1.33")
	if !old.Default {
		t.Fatal("the first installed runtime of a kind becomes the default")
	}
	cur := install("php-8.4.0")
	if cur.Default {
		t.Fatal("a second version must not steal the default")
	}
	// Same version twice: conflict. Unknown fields are still rejected.
	e.errCode("POST", "/api/runtimes/install", map[string]any{"catalogId": "php-8.4.0"}, 409)
	e.errCode("POST", "/api/runtimes/install", map[string]any{"catalogId": "php-8.4.0", "replace": true}, 400)

	var def model.Runtime
	if r := e.do("POST", "/api/runtimes/"+cur.ID+"/default", nil, &def); r.StatusCode != 200 || !def.Default || def.ID != cur.ID {
		t.Fatalf("make default: %d %+v", r.StatusCode, def)
	}
	var rs []model.Runtime
	e.do("GET", "/api/runtimes", nil, &rs)
	defaults := 0
	for _, r := range rs {
		if r.Default {
			defaults++
			if r.ID != cur.ID {
				t.Fatalf("wrong default %+v", r)
			}
		}
	}
	if len(rs) != 2 || defaults != 1 {
		t.Fatalf("two versions, one default: %+v", rs)
	}
	// Removing the default promotes the remaining version.
	if r := e.do("DELETE", "/api/runtimes/"+cur.ID, nil, nil); r.StatusCode != 204 && r.StatusCode != 200 {
		t.Fatalf("delete status %d", r.StatusCode)
	}
	e.do("GET", "/api/runtimes", nil, &rs)
	if len(rs) != 1 || rs[0].ID != old.ID || !rs[0].Default {
		t.Fatalf("promotion: %+v", rs)
	}
	e.errCode("POST", "/api/runtimes/rt_nope/default", nil, 404)
}

func TestUnverifiedInstallAPI(t *testing.T) {
	e := newEnv(t)
	z, _ := tinyZip(t, "top/nginx", "ng")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(z) }))
	defer srv.Close()
	b, _ := json.Marshal([]any{map[string]any{"id": "nginx-1.0.0", "kind": "nginx", "version": "1.0.0", "name": "nginx 1.0.0", "url": srv.URL + "/nginx-1.0.0.zip",
		"checksum": map[string]any{"type": "none"}, "unverified": true, "binaryRelPath": "nginx", "stripComponents": 1, "os": runtime.GOOS, "arch": runtime.GOARCH, "homepage": "https://example.invalid/"}})
	os.WriteFile(filepath.Join(e.home, "catalog.json"), b, 0o644)
	if c := e.errCode("POST", "/api/runtimes/install", map[string]any{"catalogId": "nginx-1.0.0"}, 400); c != "unverified" {
		t.Fatalf("code %s", c)
	}
	var j model.Job
	e.do("POST", "/api/runtimes/install", map[string]any{"catalogId": "nginx-1.0.0", "acceptUnverified": true}, &j)
	if done := waitJob(t, e, j.ID); done.State != "done" {
		t.Fatalf("%+v", done)
	}
}
