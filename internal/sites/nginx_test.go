package sites

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

func TestNginxConf(t *testing.T) {
	d := store.Data{
		Settings: model.Settings{ProxyHTTPPort: 8080, ProxyHTTPSPort: 8443, SiteServer: ServerNginx},
		Services: []model.Service{{ID: "svc_1", Port: 3000}, {ID: "svc_2", Port: 0}},
		Sites: []model.Site{
			{ID: "s1", Domain: "shop.localhost", Type: "proxy", ServiceID: "svc_1", Enabled: true},
			{ID: "s2", Domain: "docs.localhost", Type: "static", Root: "/srv/my docs", HTTPS: true, Enabled: true},
			{ID: "s3", Domain: "off.localhost", Type: "proxy", UpstreamPort: 9000, Enabled: false},
			{ID: "s5", Domain: "broken.localhost", Type: "proxy", ServiceID: "svc_2", Enabled: true},
		},
	}
	got := NginxConf(d, "/data")
	for _, want := range []string{
		"daemon off;\n",
		"pid logs/nginx.pid;\n",
		"listen 127.0.0.1:8080 default_server;\n\t\tserver_name _;\n\t\treturn 404;",
		"listen 127.0.0.1:8443 ssl default_server;",
		"\tserver {\n\t\tlisten 127.0.0.1:8080;\n\t\tserver_name shop.localhost;\n\t\tlocation / {\n\t\t\tproxy_pass http://127.0.0.1:3000;\n",
		"\tserver {\n\t\tlisten 127.0.0.1:8443 ssl;\n\t\tserver_name docs.localhost;\n\t\tssl_certificate \"/data/nginx/certs/sites.crt\";\n\t\tssl_certificate_key \"/data/nginx/certs/sites.key\";\n\t\troot \"/srv/my docs\";\n",
		"proxy_set_header Connection $connection_upgrade;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, bad := range []string{"off.localhost", "broken.localhost"} {
		if strings.Contains(got, bad) {
			t.Errorf("%s must be left out:\n%s", bad, got)
		}
	}
	if strings.Count(got, "{") != strings.Count(got, "}") {
		t.Fatalf("unbalanced braces:\n%s", got)
	}
	if strings.Index(got, "docs.localhost") > strings.Index(got, "shop.localhost") {
		t.Fatal("sites must be sorted by domain")
	}
	// No HTTPS site: no ssl server at all (and no certificate needed).
	plain := NginxConf(store.Data{Settings: model.Settings{ProxyHTTPPort: 80, ProxyHTTPSPort: 443}}, "/data")
	if strings.Contains(plain, "ssl") || !strings.Contains(plain, "listen 127.0.0.1:80 default_server;") {
		t.Fatalf("bad empty config:\n%s", plain)
	}
}

func TestNginxCerts(t *testing.T) {
	home := t.TempDir()
	if err := ensureNginxCerts(home, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "nginx", "certs")); !os.IsNotExist(err) {
		t.Fatal("no https sites: nothing must be created")
	}
	read := func(name string) *x509.Certificate {
		b, err := os.ReadFile(filepath.Join(home, "nginx", "certs", name))
		if err != nil {
			t.Fatal(err)
		}
		p, _ := pem.Decode(b)
		c, err := x509.ParseCertificate(p.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if err := ensureNginxCerts(home, []string{"a.localhost", "b.test"}); err != nil {
		t.Fatal(err)
	}
	ca, leaf := read("ca.crt"), read("sites.crt")
	if !ca.IsCA || leaf.CheckSignatureFrom(ca) != nil || strings.Join(leaf.DNSNames, ",") != "a.localhost,b.test" {
		t.Fatalf("bad certs: ca=%v names=%v", ca.IsCA, leaf.DNSNames)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: "b.test", Roots: pool}); err != nil {
		t.Fatalf("leaf must verify against the CA: %v", err)
	}

	// Same names: nothing is reissued.
	if err := ensureNginxCerts(home, []string{"a.localhost", "b.test"}); err != nil {
		t.Fatal(err)
	}
	if read("sites.crt").SerialNumber.Cmp(leaf.SerialNumber) != 0 {
		t.Fatal("unchanged names must keep the certificate")
	}
	// New name: reissued, CA kept (a trusted CA stays trusted).
	if err := ensureNginxCerts(home, []string{"a.localhost", "b.test", "c.localhost"}); err != nil {
		t.Fatal(err)
	}
	if l2 := read("sites.crt"); l2.SerialNumber.Cmp(leaf.SerialNumber) == 0 || len(l2.DNSNames) != 3 {
		t.Fatal("changed names must reissue")
	}
	if read("ca.crt").SerialNumber.Cmp(ca.SerialNumber) != 0 {
		t.Fatal("the CA must be kept")
	}
	// Close to expiry: reissued.
	defer func() { now = time.Now }()
	now = func() time.Time { return time.Now().Add(380 * 24 * time.Hour) }
	before := read("sites.crt").SerialNumber
	if err := ensureNginxCerts(home, []string{"a.localhost", "b.test", "c.localhost"}); err != nil {
		t.Fatal(err)
	}
	if read("sites.crt").SerialNumber.Cmp(before) == 0 {
		t.Fatal("expiring certificate must be reissued")
	}
}

func TestEnsureNginxAndSwitch(t *testing.T) {
	m, st, sup, home := newMgr(t)
	st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_n", Kind: "nginx", Binary: "/x/nginx"})
		return nil
	})
	if c, _ := m.EnsureNginx(); c {
		t.Fatal("caddy serves the sites: no nginx service")
	}
	st.Update(func(d *store.Data) error { d.Settings.SiteServer = ServerNginx; return nil })
	if c, err := m.EnsureServers(); !c || err != nil {
		t.Fatalf("should create: %v %v", c, err)
	}
	if c, _ := m.EnsureNginx(); c {
		t.Fatal("must not create twice")
	}
	svcs := st.Snapshot().Services
	if len(svcs) != 1 || svcs[0].Name != "Nginx (sites)" || !IsSitesNginx(svcs[0]) || svcs[0].Cwd != home {
		t.Fatalf("%+v", svcs)
	}
	if IsSitesNginx(model.Service{Kind: "nginx"}) {
		t.Fatal("a user's own nginx service is not the sites server")
	}
	for _, p := range []string{"config/nginx.conf", "nginx/logs", "nginx/temp"} {
		if _, err := os.Stat(filepath.Join(home, p)); err != nil {
			t.Fatalf("%s must exist", p)
		}
	}

	// An https site with nginx active issues the certificate and restarts
	// the running nginx service.
	sup.setState("running")
	if _, err := m.Create(model.SiteInput{Name: "x", Domain: "x.localhost", Type: "proxy", UpstreamPort: 3000, HTTPS: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "nginx", "certs", "sites.crt")); err != nil {
		t.Fatal("certificate must be issued")
	}
	b, _ := os.ReadFile(m.NginxConfPath())
	if !strings.Contains(string(b), "server_name x.localhost;") {
		t.Fatalf("nginx.conf not written:\n%s", b)
	}
	for i := 0; i < 200 && sup.count() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if sup.count() == 0 {
		t.Fatal("running nginx sites service was not restarted")
	}
}

func TestRootRejectsNginxSpecials(t *testing.T) {
	m, _, _, _ := newMgr(t)
	dir := filepath.Join(t.TempDir(), "a$b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(model.SiteInput{Name: "x", Domain: "x.localhost", Type: "static", Root: dir}); err == nil {
		t.Fatal("a $ in the root must be rejected")
	}
}

func TestSetEnabled(t *testing.T) {
	m, st, sup, home := newMgr(t)
	st.Update(func(d *store.Data) error {
		d.Services = append(d.Services, model.Service{ID: "svc_c", Name: "Caddy", Kind: "caddy"})
		return nil
	})
	v, err := m.Create(model.SiteInput{Name: "x", Domain: "x.localhost", Type: "proxy", UpstreamPort: 3000})
	if err != nil {
		t.Fatal(err)
	}
	read := func() string {
		c, _ := os.ReadFile(filepath.Join(home, "config", "Caddyfile"))
		n, _ := os.ReadFile(m.NginxConfPath())
		return string(c) + string(n)
	}
	sup.setState("running")
	before := sup.count()

	off, err := m.SetEnabled(v.ID, false)
	if err != nil || off.Enabled {
		t.Fatalf("%+v %v", off, err)
	}
	if strings.Contains(read(), "x.localhost") {
		t.Fatal("a disabled site must leave both configs")
	}
	if st.Snapshot().Sites[0].Enabled {
		t.Fatal("not persisted")
	}
	on, err := m.SetEnabled(v.ID, true)
	if err != nil || !on.Enabled || on.URL != "http://x.localhost:8080" {
		t.Fatalf("%+v %v", on, err)
	}
	if strings.Count(read(), "x.localhost") != 2 {
		t.Fatalf("an enabled site must be in both configs:\n%s", read())
	}
	for i := 0; i < 200 && sup.count() < before+2; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if sup.count() < before+2 {
		t.Fatal("the running site server must restart on each switch")
	}
	if _, err := m.SetEnabled("site_none", true); err == nil {
		t.Fatal("unknown id")
	}

	// A static site whose folder vanished can still be switched off (no revalidation).
	root := filepath.Join(t.TempDir(), "gone")
	os.MkdirAll(root, 0o755)
	s, err := m.Create(model.SiteInput{Name: "s", Domain: "s.localhost", Type: "static", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(root)
	if _, err := m.SetEnabled(s.ID, false); err != nil {
		t.Fatalf("switching off a broken site: %v", err)
	}
}
