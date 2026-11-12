package kinds

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zero2dev/internal/model"
)

func TestEveryServiceKindHasAProfile(t *testing.T) {
	for _, k := range model.ServiceKinds {
		if _, ok := Lookup(k); !ok {
			t.Errorf("no profile for service kind %s", k)
		}
	}
	for _, p := range Profiles() {
		if !model.In(p.Kind, model.ServiceKinds) {
			t.Errorf("profile %s is not a service kind", p.Kind)
		}
	}
}

func TestProfilesAreConsistent(t *testing.T) {
	types := []string{"text", "number", "bool", "select", "multi", "size", "lines"}
	for _, p := range Profiles() {
		if p.Label == "" || p.Folder.Use == "" || p.Port.Use == "" {
			t.Errorf("%s: label, folder.use and port.use are required", p.Kind)
		}
		if !slices.Contains([]string{"required", "hidden"}, p.Folder.Use) || !slices.Contains([]string{"required", "optional", "hidden"}, p.Port.Use) {
			t.Errorf("%s: bad folder/port use", p.Kind)
		}
		if !slices.Contains([]string{"service", "shared", "none"}, p.DataDir) {
			t.Errorf("%s: bad dataDir %q", p.Kind, p.DataDir)
		}
		if p.DataDir != "none" && model.DataDirName(p.Kind) == "" {
			t.Errorf("%s: dataDir %s but no model.DataDirName", p.Kind, p.DataDir)
		}
		for _, f := range p.Fixed {
			if f.Help == "" || len(f.Args) == 0 {
				t.Errorf("%s: fixed args need args and help", p.Kind)
			}
		}
		seen := map[string]bool{}
		for i := range p.Options {
			o := &p.Options[i]
			if seen[o.Key] {
				t.Errorf("%s: duplicate key %s", p.Kind, o.Key)
			}
			seen[o.Key] = true
			if !slices.Contains(types, o.Type) {
				t.Errorf("%s.%s: bad type %q", p.Kind, o.Key, o.Type)
			}
			if o.Label == "" || o.Help == "" || o.Group == "" {
				t.Errorf("%s.%s: label, help and group are required", p.Kind, o.Key)
			}
			if err := o.check(o.Default); err != nil && !o.Required {
				t.Errorf("%s.%s: default %q is invalid: %v", p.Kind, o.Key, o.Default, err)
			}
			for k, v := range o.Show {
				dep := p.option(k)
				if dep == nil {
					t.Errorf("%s.%s: show refers to unknown %s", p.Kind, o.Key, k)
				} else if v != "*" && dep.Type != "multi" && dep.check(v) != nil {
					t.Errorf("%s.%s: show %s=%q is not a value of %s", p.Kind, o.Key, k, v, k)
				}
			}
			if o.MinVersion != "" && len(nums(o.MinVersion)) == 0 {
				t.Errorf("%s.%s: bad minVersion", p.Kind, o.Key)
			}
		}
		if err := Validate(p.Kind, nil); err != nil && !hasRequired(&p) {
			t.Errorf("%s: defaults do not validate: %v", p.Kind, err)
		}
	}
}

func hasRequired(p *Profile) bool {
	for _, o := range p.Options {
		if o.Required && o.Default == "" && p.Visible(&o, Defaults(p.Kind)) {
			return true
		}
	}
	return false
}

func TestValidate(t *testing.T) {
	cases := []struct {
		kind string
		s    map[string]string
		bad  string // "" = valid; else part of the message
	}{
		{"mariadb", map[string]string{"maxConnections": "50"}, ""},
		{"mariadb", map[string]string{"maxConnections": "0"}, "between"},
		{"mariadb", map[string]string{"maxConnections": "lots"}, "whole number"},
		{"mariadb", map[string]string{"nope": "1"}, "unknown setting"},
		{"mariadb", map[string]string{"bufferPool": "1T"}, "not valid"},
		{"mariadb", map[string]string{"charset": "klingon"}, "not one of"},
		{"mariadb", map[string]string{"collation": "x\ny"}, "one line"},
		{"node", map[string]string{}, "Script is required"},
		{"node", map[string]string{"script": "server.js"}, ""},
		{"node", map[string]string{"run": "npm"}, ""}, // npmScript defaults to dev
		{"node", map[string]string{"run": "npm", "npmScript": "a b"}, "not valid"},
		{"php", map[string]string{"extensions": "curl,pdo_mysql"}, ""},
		{"php", map[string]string{"extensions": "curl,../evil"}, "not a valid name"},
		{"php", map[string]string{"iniLines": "short_open_tag = On\n; comment\n"}, ""},
		{"php", map[string]string{"iniLines": "[evil]\nx=1"}, "not a php.ini line"},
		{"php", map[string]string{"timezone": "Asia/Kathmandu"}, ""},
		{"php", map[string]string{"timezone": "UTC\nextension=evil"}, "one line"},
		{"php", map[string]string{"displayErrors": "yes"}, "true or false"},
		{"php", map[string]string{"xdebugMode": "debug,profile"}, ""},
		{"php", map[string]string{"xdebugMode": "debug,party"}, "not one of"},
		{"custom", map[string]string{"x": "1"}, "unknown setting"},
		{"java", map[string]string{"jar": "app.jar", "maxHeap": "2g"}, ""},
		{"java", map[string]string{"run": "class", "mainClass": "com.x.App"}, ""},
		{"java", map[string]string{"run": "class"}, "Main class is required"},
	}
	for _, c := range cases {
		err := Validate(c.kind, c.s)
		switch {
		case c.bad == "" && err != nil:
			t.Errorf("%s %v: unexpected error %v", c.kind, c.s, err)
		case c.bad != "" && (err == nil || !strings.Contains(err.Error(), c.bad)):
			t.Errorf("%s %v: want error containing %q, got %v", c.kind, c.s, c.bad, err)
		}
	}
}

func ctx() Ctx {
	return Ctx{Home: filepath.FromSlash("/z/data"), Data: filepath.FromSlash("/z/data/x/svc_1"), Bin: filepath.FromSlash("/z/bin/rt/prog"),
		Cwd: filepath.FromSlash("/work/app"), Port: "3306", Version: "", Windows: true, ServiceID: "svc_1"}
}

func TestBuildDatabaseDefaultsKeepTheOldArguments(t *testing.T) {
	c := ctx()
	d := c.Data
	// The arguments 0.0.3 used before settings existed come first, unchanged.
	old := map[string][]string{
		"mariadb":  {"--no-defaults", "--datadir=" + d, "--port=3306", "--bind-address=127.0.0.1", "--console"},
		"mysql":    {"--no-defaults", "--datadir=" + d, "--port=3306", "--bind-address=127.0.0.1", "--console", "--mysqlx=OFF"},
		"postgres": {"-D", d, "-p", "3306", "-c", "listen_addresses=127.0.0.1"},
		"redis":    {"--port", "3306", "--bind", "127.0.0.1", "--dir", filepath.ToSlash(d)},
	}
	for kind, want := range old {
		got := Build(kind, nil, nil, c).Args
		if len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
			t.Errorf("%s: got %q, want prefix %q", kind, got, want)
		}
	}
	got := Build("mariadb", nil, nil, c).Args
	for _, a := range []string{"--character-set-server=utf8mb4", "--max-connections=151", "--innodb-buffer-pool-size=128M", "--max-allowed-packet=64M"} {
		if !slices.Contains(got, a) {
			t.Errorf("mariadb defaults: missing %s in %q", a, got)
		}
	}
	if slices.ContainsFunc(got, func(a string) bool { return strings.HasPrefix(a, "--sql-mode") || strings.HasPrefix(a, "--collation") }) {
		t.Errorf("server-default choices must emit nothing: %q", got)
	}
	c.Windows = false
	if got := Build("mariadb", nil, nil, c).Args; slices.Contains(got, "--console") {
		t.Errorf("--console is Windows only: %q", got)
	}
}

func TestBuildSettings(t *testing.T) {
	c := ctx()
	cases := []struct {
		kind  string
		s     map[string]string
		extra []string
		want  []string // must appear in order (not necessarily adjacent)
		not   []string
		prog  string
	}{
		{"mariadb", map[string]string{"maxConnections": "50", "slowLog": "true", "slowSeconds": "5", "sqlMode": "permissive"}, nil,
			[]string{"--sql-mode=NO_ENGINE_SUBSTITUTION", "--max-connections=50", "--slow-query-log=1", "--long-query-time=5"}, nil, ""},
		{"redis", map[string]string{"persistence": "none"}, nil, []string{"--save", "", "--appendonly", "no"}, []string{"--maxmemory-policy"}, ""},
		{"redis", map[string]string{"maxMemory": "256mb", "maxMemoryPolicy": "allkeys-lru"}, nil, []string{"--maxmemory", "256mb", "--maxmemory-policy", "allkeys-lru"}, nil, ""},
		{"node", map[string]string{"script": "server.js", "watch": "true", "inspect": "true"}, []string{"--x={port}"},
			[]string{"--watch", "--inspect=127.0.0.1:9229", "server.js", "--x=3306"}, nil, ""},
		{"node", map[string]string{"run": "npm", "npmScript": "start", "watch": "true"}, []string{"--verbose"},
			[]string{"run", "start", "--", "--verbose"}, []string{"--watch"}, "npm"},
		{"go", nil, []string{"-v"}, []string{"run", ".", "-v"}, nil, ""},
		{"go", map[string]string{"tags": "dev", "package": "./cmd/api"}, nil, []string{"run", "-tags=dev", "./cmd/api"}, nil, ""},
		{"java", map[string]string{"jar": "app.jar", "maxHeap": "512m", "springPort": "true"}, []string{"--debug"},
			[]string{"-Xmx512m", "-Dserver.port=3306", "-jar", "app.jar", "--debug"}, nil, ""},
		{"java", map[string]string{"run": "class", "classpath": "lib\\*", "mainClass": "a.B"}, nil, []string{"-cp", "lib\\*", "a.B"}, []string{"-jar"}, ""},
		{"postgres", map[string]string{"logStatement": "all", "timezone": "UTC"}, nil, []string{"-c", "log_statement=all", "-c", "TimeZone=UTC"}, nil, ""},
		{"caddy", nil, nil, []string{"run", "--config", "/z/data/config/Caddyfile", "--adapter", "caddyfile"}, nil, ""},
		{"nginx", nil, nil, []string{"-p", "/z/bin/rt/"}, nil, ""},
		{"custom", nil, []string{"--data={data}", "{port}"}, []string{"--data=" + c.Data, "3306"}, nil, ""},
	}
	for _, tc := range cases {
		pl := Build(tc.kind, tc.s, tc.extra, c)
		if !inOrder(pl.Args, tc.want) {
			t.Errorf("%s %v: args %q do not contain %q in order", tc.kind, tc.s, pl.Args, tc.want)
		}
		for _, n := range tc.not {
			if slices.Contains(pl.Args, n) {
				t.Errorf("%s %v: args %q must not contain %q", tc.kind, tc.s, pl.Args, n)
			}
		}
		if pl.Program != tc.prog {
			t.Errorf("%s: program %q, want %q", tc.kind, pl.Program, tc.prog)
		}
	}
}

func inOrder(have, want []string) bool {
	i := 0
	for _, h := range have {
		if i < len(want) && h == want[i] {
			i++
		}
	}
	return i == len(want)
}

func TestBuildEnv(t *testing.T) {
	c := ctx()
	pl := Build("go", nil, nil, c)
	for _, e := range []string{"GOPATH=/z/data/go/path", "GOCACHE=/z/data/go/cache", "GOENV=/z/data/go/env"} {
		if !slices.Contains(pl.Env, e) {
			t.Errorf("go env %q missing %s", pl.Env, e)
		}
	}
	if pl := Build("go", map[string]string{"isolate": "false"}, nil, c); len(pl.Env) != 0 {
		t.Errorf("isolate off: env %q", pl.Env)
	}
	pl = Build("node", map[string]string{"script": "a.js"}, nil, c)
	if !slices.Contains(pl.Env, "NODE_ENV=development") || !slices.Contains(pl.Env, "npm_config_cache=/z/data/node/npm-cache") {
		t.Errorf("node env %q", pl.Env)
	}
	if pl := Build("node", map[string]string{"script": "a.js", "nodeEnv": ""}, nil, c); slices.ContainsFunc(pl.Env, func(e string) bool { return strings.HasPrefix(e, "NODE_ENV=") }) {
		t.Errorf("NODE_ENV not set must not be emitted: %q", pl.Env)
	}
}

func TestBuildMinVersion(t *testing.T) {
	c := ctx()
	c.Version = "18.0.0"
	pl := Build("node", map[string]string{"script": "a.js", "watch": "true", "envFile": ".env"}, nil, c)
	if slices.Contains(pl.Args, "--watch") || slices.Contains(pl.Args, "--env-file=.env") {
		t.Errorf("old node must not get --watch/--env-file: %q", pl.Args)
	}
	if len(pl.Warnings) != 2 {
		t.Errorf("warnings %q", pl.Warnings)
	}
	c.Version = "22.11.0"
	if pl := Build("node", map[string]string{"script": "a.js", "watch": "true"}, nil, c); !slices.Contains(pl.Args, "--watch") || len(pl.Warnings) != 0 {
		t.Errorf("new node: %q %q", pl.Args, pl.Warnings)
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		have, want string
		ok         bool
	}{
		{"20.6.0", "20.6", true}, {"20.5.9", "20.6", false}, {"v22.1.0", "18.11", true}, {"18.11", "18.11", true},
		{"18.9.1", "18.11", false}, {"", "20.6", true}, {"unknown", "1", true}, {"8.4.13-nts", "8.4", true},
	}
	for _, c := range cases {
		if got := VersionAtLeast(c.have, c.want); got != c.ok {
			t.Errorf("VersionAtLeast(%q,%q) = %v", c.have, c.want, got)
		}
	}
}

func fakePHP(t *testing.T, exts ...string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "php.exe")
	os.WriteFile(bin, nil, 0o755)
	os.MkdirAll(filepath.Join(dir, "ext"), 0o755)
	for _, e := range exts {
		os.WriteFile(filepath.Join(dir, "ext", e), nil, 0o644)
	}
	os.WriteFile(filepath.Join(dir, "php.ini-development"), []byte("; base dev\r\ndisplay_errors = On\r\n"), 0o644)
	return bin
}

func TestListExtensions(t *testing.T) {
	bin := fakePHP(t, "php_curl.dll", "php_xdebug.dll", "php_opcache.dll", "README.txt", "php_pdo_mysql.dll")
	got := ListExtensions(bin)
	names := []string{}
	for _, e := range got {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"curl", "opcache", "pdo_mysql", "xdebug"}) {
		t.Fatalf("names %q", names)
	}
	if !got[1].Zend || !got[3].Zend || got[0].Zend || got[0].File != "php_curl.dll" {
		t.Errorf("zend flags/file: %+v", got)
	}
	if got := ListExtensions(filepath.Join(t.TempDir(), "php")); got == nil || len(got) != 0 {
		t.Errorf("missing ext folder: %v", got)
	}
}

func TestPHPIni(t *testing.T) {
	bin := fakePHP(t, "php_curl.dll", "php_mbstring.dll", "php_xdebug.dll")
	c := ctx()
	c.Bin, c.Data = bin, filepath.Join(t.TempDir(), "php", "svc_1")
	pl := Build("php", map[string]string{
		"extensions": "curl,mbstring,xdebug,intl", "memoryLimit": "512M", "displayErrors": "false",
		"xdebugMode": "debug,develop", "iniLines": "short_open_tag = On", "docroot": "public", "router": "public/index.php",
	}, nil, c)
	if !inOrder(pl.Args, []string{"-c", PHPIniPath(c.Data), "-S", "127.0.0.1:3306", "-t", filepath.Join(c.Cwd, "public"), "public/index.php"}) {
		t.Errorf("args %q", pl.Args)
	}
	if len(pl.Files) != 1 || pl.Files[0].Path != PHPIniPath(c.Data) {
		t.Fatalf("files %+v", pl.Files)
	}
	ini := string(pl.Files[0].Content)
	for _, want := range []string{
		"; base dev", `extension_dir = "` + ExtDir(bin) + `"`, "memory_limit = 512M", "display_errors = Off", "date.timezone = UTC",
		"extension = php_curl.dll", "extension = php_mbstring.dll", "zend_extension = php_xdebug.dll", "xdebug.mode = debug,develop",
		"xdebug.start_with_request = trigger", `xdebug.output_dir = "` + filepath.Join(c.Data, "xdebug") + `"`, "short_open_tag = On",
		`session.save_path = "` + filepath.Join(c.Data, "sessions") + `"`,
	} {
		if !strings.Contains(ini, want) {
			t.Errorf("php.ini lacks %q:\n%s", want, ini)
		}
	}
	if strings.Index(ini, "display_errors = On") > strings.Index(ini, "display_errors = Off") {
		t.Error("the base php.ini must come first so the settings win")
	}
	if len(pl.Warnings) != 1 || !strings.Contains(pl.Warnings[0], "intl") {
		t.Errorf("warnings %q", pl.Warnings)
	}
	if !slices.Contains(pl.Dirs, filepath.Join(c.Data, "xdebug")) || !slices.Contains(pl.Dirs, filepath.Join(c.Data, "tmp")) {
		t.Errorf("dirs %q", pl.Dirs)
	}
	// Without xdebug ticked, no xdebug lines.
	pl = Build("php", map[string]string{"extensions": "curl"}, nil, c)
	if ini := string(pl.Files[0].Content); strings.Contains(ini, "xdebug") {
		t.Errorf("xdebug lines without xdebug:\n%s", ini)
	}
	// iniBase none: no base file.
	pl = Build("php", map[string]string{"iniBase": "none"}, nil, c)
	if strings.Contains(string(pl.Files[0].Content), "base dev") {
		t.Error("iniBase none must not include the base file")
	}
}
