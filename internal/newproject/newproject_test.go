package newproject

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"zero2dev/internal/envfile"
	"zero2dev/internal/frameworks"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
	"zero2dev/internal/tasks"
)

func hasProblem(p Plan, sub string) bool {
	return slices.ContainsFunc(p.Problems, func(s string) bool { return strings.Contains(s, sub) })
}

func TestPlanProblems(t *testing.T) {
	e := newEnv(t, phpApp(), nodeApp(), htmlApp())
	e.install("php", "8.4.2", true)
	e.install("composer", "2.8.0", true)
	e.install("node", "22.1.0", true)
	// a non-empty folder, a taken domain and a taken port
	os.MkdirAll(filepath.Join(e.parent, "taken"), 0o755)
	os.WriteFile(filepath.Join(e.parent, "taken", "file.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(e.parent, "empty"), 0o755)
	e.st.Update(func(d *store.Data) error {
		d.Sites = append(d.Sites, model.Site{ID: "site_1", Name: "Other", Domain: "busy.localhost", Type: "proxy", UpstreamPort: 9999})
		d.Services = append(d.Services, model.Service{ID: "svc_1", Name: "Old", Kind: "node", Port: 8000, Settings: map[string]string{}})
		return nil
	})

	cases := []struct {
		name    string
		mutate  func(r *Request)
		problem string
	}{
		{"no name", func(r *Request) { r.Name = "  " }, "Give the project a name"},
		{"bad characters", func(r *Request) { r.Name = `a"b` }, "letters, digits"},
		{"no slug", func(r *Request) { r.Name = "日本語" }, ""}, // letters, but nothing ASCII
		{"reserved", func(r *Request) { r.Name = "nul" }, "reserved name"},
		{"no parent", func(r *Request) { r.Parent = "" }, "Choose the folder"},
		{"relative parent", func(r *Request) { r.Parent = "www" }, "not an absolute path"},
		{"missing parent", func(r *Request) { r.Parent = filepath.Join(e.parent, "nope") }, "does not exist"},
		{"folder not empty", func(r *Request) { r.Name = "taken" }, "already exists and is not empty"},
		{"domain taken", func(r *Request) { r.Domain = "busy.localhost" }, "already used by site"},
		{"bad domain", func(r *Request) { r.Domain = "no_good.localhost" }, "not a valid hostname"},
		{"foreign suffix", func(r *Request) { r.Domain = "shop.example.com" }, "must end with"},
		{"port used by a service", func(r *Request) { r.Port = 8000 }, "already used by the service Old"},
		{"port used by a site", func(r *Request) { r.Port = 9999 }, "already used by the site busy.localhost"},
		{"bad port", func(r *Request) { r.Port = 70000 }, "between 1 and 65535"},
		{"database not offered", func(r *Request) { r.DatabaseKind = "postgres" }, "not offered"},
		{"unknown id has no database", func(r *Request) { r.Framework = "nodeapp"; r.DatabaseKind = "mariadb" }, "does not use a database"},
		{"database name of the server", func(r *Request) { r.Name = "mysql"; r.DatabaseKind = "mariadb" }, "used by the database server itself"},
	}
	for _, c := range cases {
		req := e.req("app")
		c.mutate(&req)
		p, err := e.m.Plan(req)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(p.Problems) == 0 || !hasProblem(p, c.problem) {
			t.Errorf("%s: problems %q, want one with %q", c.name, p.Problems, c.problem)
		}
		// a plan with a problem cannot start
		if _, err := e.m.Start(req); err == nil {
			t.Errorf("%s: Start must refuse", c.name)
		}
	}
	// an empty folder is fine (with a warning); everything else good
	req := e.req("app")
	req.Name = "empty"
	if p, _ := e.m.Plan(req); len(p.Problems) != 0 || !slices.ContainsFunc(p.Warnings, func(s string) bool { return strings.Contains(s, "is empty") }) {
		t.Errorf("empty folder: %+v", p)
	}
	// unknown framework is an error, not a plan
	if _, err := e.m.Plan(Request{Framework: "nope"}); err == nil {
		t.Error("unknown framework")
	}
	// a framework with a required database
	req = e.req("app")
	fw := phpApp()
	fw.ID, fw.Database.Optional = "needdb", false
	writeFrameworks(t, e.home, fw)
	req.Framework = "needdb"
	if p, _ := e.m.Plan(req); !hasProblem(p, "needs a database") {
		t.Errorf("required database: %+v", p.Problems)
	}
	// the picked port skips used ones: 8000 is taken by svc_1
	writeFrameworks(t, e.home, phpApp())
	if p, _ := e.m.Plan(e.req("app")); p.Port != 8001 || p.Slug != "my-shop" || p.Domain != "my-shop.localhost" || p.Dir != filepath.Join(e.parent, "my-shop") {
		t.Errorf("defaults: %+v", p)
	}
}

func TestPlanInstallsAndSteps(t *testing.T) {
	e := newEnv(t, phpApp())
	req := e.req("app")
	req.DatabaseKind = "mariadb"
	req.OpenEditor = true
	p, err := e.m.Plan(req)
	if err != nil || len(p.Problems) != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	// PHP: the newest version inside min 8.2 (not 8.1.9, not "9.0.0"? 9.0.0 fits "8.2 or newer" and is newest)
	if len(p.Installs) != 3 || p.Installs[0].CatalogID != "php-9.0.0-other" || p.Installs[1].CatalogID != "composer-2.8.0" || p.Installs[2].CatalogID != "mariadb-11.4.0" {
		t.Fatalf("installs %+v", p.Installs)
	}
	if p.Installs[0].Reason != "App needs PHP 8.2 or newer" {
		t.Errorf("reason %q", p.Installs[0].Reason)
	}
	if p.Database == nil || p.Database.Name != "my_shop" || p.Database.User != "my_shop" || p.Database.ServiceID != "" {
		t.Errorf("database %+v", p.Database)
	}
	want := []string{"Installing PHP 9 elsewhere", "Installing Composer 2.8.0", "Installing MariaDB 11.4.0",
		"Starting MariaDB and creating the database my_shop", "Creating the app", "Writing index.php", "Writing the .env file", "Creating the project"}
	if !slices.Equal(p.Steps, want) {
		t.Errorf("steps\n%q\nwant\n%q", p.Steps, want)
	}
	if !slices.Contains(p.Creates, "Service My Shop (PHP, port 8000)") || !slices.Contains(p.Creates, "Service MariaDB (MariaDB, port 3306)") ||
		!slices.Contains(p.Creates, "Site https://my-shop.localhost") || !slices.Contains(p.Creates, "Project My Shop") {
		t.Errorf("creates %q", p.Creates)
	}
	for _, s := range p.Steps {
		if strings.Contains(s, "password") {
			t.Errorf("the plan shows no password: %q", s)
		}
	}
	// no editor installed: a warning, no step; no site server: a warning
	if !slices.ContainsFunc(p.Warnings, func(s string) bool { return strings.Contains(s, "No code editor") }) ||
		!slices.ContainsFunc(p.Warnings, func(s string) bool { return strings.Contains(s, "No site server") }) {
		t.Errorf("warnings %q", p.Warnings)
	}

	// An installed PHP that fits is preferred over a newer download; the default of the kind wins.
	e.install("php", "8.3.1", false)
	e.install("php", "8.4.2", true)
	e.install("composer", "2.8.0", true)
	e.install("mariadb", "11.4.0", true)
	e.install("vscode", "1.9.0", false)
	req.OpenEditor = true
	p, _ = e.m.Plan(req)
	if len(p.Installs) != 0 {
		t.Errorf("everything is installed: %+v", p.Installs)
	}
	if p.Steps[len(p.Steps)-1] != "Opening the folder in vscode 1.9.0" || p.Steps[0] != "Starting MariaDB and creating the database my_shop" {
		t.Errorf("steps %q", p.Steps)
	}
	// Only an old PHP is installed: the plan installs a fitting one and keeps the old one.
	e2 := newEnv(t, phpApp())
	e2.install("php", "8.1.9", true)
	e2.install("composer", "2.8.0", true)
	if p, _ := e2.m.Plan(e2.req("app")); len(p.Installs) != 1 || p.Installs[0].Kind != "php" {
		t.Errorf("old php: %+v", p.Installs)
	}
	// An upper bound: max 8.3 takes 8.3.1 and not 8.4.2.
	fw := phpApp()
	fw.Runtime.Max = "8.3"
	writeFrameworks(t, e2.home, fw)
	if p, _ := e2.m.Plan(e2.req("app")); len(p.Installs) != 1 || p.Installs[0].CatalogID != "php-8.3.1" {
		t.Errorf("max 8.3: %+v", p.Installs)
	}
	// Nothing fits: a problem, not an install.
	fw.Runtime.Min, fw.Runtime.Max = "10", ""
	writeFrameworks(t, e2.home, fw)
	if p, _ := e2.m.Plan(e2.req("app")); !hasProblem(p, "No PHP version 10 or newer is available") {
		t.Errorf("none fits: %+v", p.Problems)
	}
}

func TestPlanReusesDatabaseService(t *testing.T) {
	e := newEnv(t, phpApp())
	e.install("php", "8.4.2", true)
	e.install("composer", "2.8.0", true)
	e.install("mariadb", "11.4.0", true)
	e.addService(model.Service{ID: "svc_db1", Name: "Stopped DB", Kind: "mariadb", Port: 3306})
	e.addService(model.Service{ID: "svc_db2", Name: "Running DB", Kind: "mariadb", Port: 3307})
	e.sup.running["svc_db2"] = true
	req := e.req("app")
	req.DatabaseKind = "mariadb"
	p, _ := e.m.Plan(req)
	if p.Database == nil || p.Database.ServiceID != "svc_db2" {
		t.Fatalf("a running server is preferred: %+v", p.Database)
	}
	delete(e.sup.running, "svc_db2")
	if p, _ = e.m.Plan(req); p.Database.ServiceID != "svc_db1" {
		t.Fatalf("a stopped one is reused: %+v", p.Database)
	}
	for _, c := range p.Creates {
		if strings.HasPrefix(c, "Service MariaDB") {
			t.Errorf("no new server: %q", p.Creates)
		}
	}
}

func TestStartBuildsEverything(t *testing.T) {
	e := newEnv(t, phpApp())
	e.run.hook = func(c tasks.Cmd) error { // composer create-project makes the folder and its own .env
		dir := filepath.Join(c.Dir, "my-shop")
		os.MkdirAll(filepath.Join(dir, "public"), 0o755)
		return os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=base64:generated\nAPP_URL=http://localhost\n# DB_PORT=old\nOTHER=1\n"), 0o644)
	}
	e.run.lines["php-bin"] = "Creating a project"
	req := e.req("app")
	req.DatabaseKind = "mariadb"
	req.OpenEditor = true
	e.install("vscode", "1.9.0", true)
	jb, err := e.m.Start(req)
	if err != nil {
		t.Fatal(err)
	}
	if jb.Type != model.JobNewProject || jb.State != "running" || jb.Step != "Checking the plan" {
		t.Fatalf("job %+v", jb)
	}
	done := e.finish(jb)
	dir := filepath.Join(e.parent, "my-shop")
	if done.State != "done" || done.Path != dir || done.ProjectID == "" {
		t.Fatalf("done %+v", done)
	}

	// installs in the plan's order: PHP, Composer, MariaDB
	if !slices.Equal(e.inst.started, []string{"php-9.0.0-other", "composer-2.8.0", "mariadb-11.4.0"}) {
		t.Errorf("installs %q", e.inst.started)
	}
	// the generator ran with composer's php, in the parent, without a shell
	if len(e.run.cmds) != 1 || e.run.cmds[0].Bin != "php-bin" || e.run.cmds[0].Dir != e.parent ||
		!slices.Equal(e.run.cmds[0].Args, []string{"composer.phar", "create-project", "x/app", "my-shop"}) {
		t.Fatalf("cmds %+v", e.run.cmds)
	}
	// database: the server service was created, started, the database and user made
	d := e.st.Snapshot()
	var dbSvc, app model.Service
	for _, s := range d.Services {
		switch s.Kind {
		case "mariadb":
			dbSvc = s
		case "php":
			app = s
		}
	}
	if dbSvc.ID == "" || dbSvc.Port != 3306 || !e.sup.running[dbSvc.ID] {
		t.Fatalf("database service %+v", dbSvc)
	}
	calls := e.calls()
	if len(calls) != 2 || calls[0] != "createdb "+dbSvc.ID+" my_shop" || !strings.HasPrefix(calls[1], "createuser "+dbSvc.ID+" my_shop my_shop pw=") {
		t.Fatalf("db calls %q", calls)
	}
	pw := strings.TrimPrefix(calls[1], "createuser "+dbSvc.ID+" my_shop my_shop pw=")
	if len(pw) != 24 {
		t.Errorf("password %q", pw)
	}
	// the app service uses the database
	if app.Name != "My Shop" || app.Port != 8000 || app.DatabaseID != dbSvc.ID || !slices.Contains(app.DependsOn, dbSvc.ID) ||
		app.Cwd != dir || app.EnvFile != ".env" || app.Settings["docroot"] != "public" || app.RuntimeID != "" {
		t.Errorf("service %+v", app)
	}
	if len(d.Sites) != 1 || d.Sites[0].Domain != "my-shop.localhost" || d.Sites[0].Type != "php" || d.Sites[0].ServiceID != app.ID || !d.Sites[0].HTTPS {
		t.Errorf("sites %+v", d.Sites)
	}
	if len(d.Projects) != 1 || d.Projects[0].ID != done.ProjectID || d.Projects[0].Name != "My Shop" ||
		!slices.Equal(d.Projects[0].ServiceIDs, []string{app.ID}) || !slices.Equal(d.Projects[0].SiteIDs, []string{d.Sites[0].ID}) {
		t.Errorf("project %+v", d.Projects)
	}
	// .env: the generator's lines are kept, ifMissing never overwrites, the rest is set
	envPath := filepath.Join(dir, ".env")
	b, _ := os.ReadFile(envPath)
	for _, want := range []string{"APP_KEY=base64:generated", "APP_URL=https://my-shop.localhost", "OTHER=1", "DB_DATABASE=my_shop", "DB_USERNAME=my_shop",
		"DB_PASSWORD=" + pw, "DB_PORT=3306", "# DB_PORT=old"} {
		if !strings.Contains(string(b), want+"\n") {
			t.Errorf(".env lacks %q:\n%s", want, b)
		}
	}
	if v, _ := envfile.Get(envPath, "APP_KEY"); v != "base64:generated" {
		t.Errorf("APP_KEY %q", v)
	}
	// the starter files were written with the placeholders replaced
	if b, _ := os.ReadFile(filepath.Join(dir, "public", "index.php")); string(b) != "<?php echo 'My Shop';\n" {
		t.Errorf("index.php %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.txt")); string(b) != "my-shop on my-shop.localhost\n" {
		t.Errorf("README %q", b)
	}
	// the generator's output is in the new service's log
	logs := e.sup.logs[app.ID]
	if len(logs) < 2 || logs[0] != "task: $ composer create-project x/app my-shop" || !slices.Contains(logs, "task: Creating a project") {
		t.Errorf("log %q", logs)
	}
	for _, l := range logs {
		if strings.Contains(l, pw) {
			t.Errorf("the password is in the log: %q", l)
		}
	}
	// the editor, the site server, the events
	if len(e.launched) != 1 || e.launched[0][1] != dir || e.launched[0][len(e.launched[0])-1] != dir {
		t.Errorf("launched %q", e.launched)
	}
	if e.sites.ensured != 1 || e.sites.regenerated < 1 {
		t.Errorf("sites %+v", e.sites)
	}
	for _, ev := range []string{"services.changed", "sites.changed", "projects.changed"} {
		if !slices.Contains(e.emitted, ev) {
			t.Errorf("event %s was not sent: %q", ev, e.emitted)
		}
	}
	// the same folder cannot be made twice
	if _, err := e.m.Start(req); err == nil {
		t.Error("the folder now exists and is not empty")
	}
}

func TestStartReusesWhatIsThere(t *testing.T) {
	e := newEnv(t, phpApp())
	e.install("php", "8.4.2", true)
	e.install("composer", "2.8.0", true)
	e.install("mariadb", "11.4.0", true)
	e.addService(model.Service{ID: "svc_db", Name: "DB", Kind: "mariadb", Port: 3306})
	e.run.hook = func(c tasks.Cmd) error { return os.MkdirAll(filepath.Join(c.Dir, "my-shop"), 0o755) }
	req := e.req("app")
	req.DatabaseKind = "mariadb"
	if done := e.start(req); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	if len(e.inst.started) != 0 {
		t.Errorf("nothing to install: %q", e.inst.started)
	}
	if !e.sup.running["svc_db"] {
		t.Error("the stopped server must be started")
	}
	n := 0
	for _, s := range e.st.Snapshot().Services {
		if s.Kind == "mariadb" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d database services", n)
	}
	// a project without a database skips the .env lines that need one
	req2 := e.req("app")
	req2.Name = "No Db"
	e.run.hook = func(c tasks.Cmd) error { return os.MkdirAll(filepath.Join(c.Dir, "no-db"), 0o755) }
	if done := e.start(req2); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	b, _ := os.ReadFile(filepath.Join(e.parent, "no-db", ".env"))
	if strings.Contains(string(b), "DB_") || !strings.Contains(string(b), "APP_URL=https://no-db.localhost") {
		t.Errorf(".env without a database:\n%s", b)
	}
	if got := e.calls(); len(got) != 2 {
		t.Errorf("db calls %q", got)
	}
}

func TestFailureCleansUpAtEveryStep(t *testing.T) {
	type tc struct {
		name   string
		inject func(e *tenv)
		// what the error says, what the database cleanup did
		errHas   string
		dbCalls  []string // after the two create calls that happened
		folder   bool     // the generated folder is kept
		svcCount int      // services left in the store (the pre-existing one)
	}
	cases := []tc{
		{name: "installing PHP", errHas: "Installing PHP 9 elsewhere failed: no space left", svcCount: 0,
			inject: func(e *tenv) { e.inst.fail["php-9.0.0-other"] = "no space left" }},
		{name: "installing the database", errHas: "Installing MariaDB 11.4.0 failed", svcCount: 0,
			inject: func(e *tenv) { e.inst.fail["mariadb-11.4.0"] = "download failed" }},
		{name: "starting the database", errHas: "Starting MariaDB failed: port busy", svcCount: 0,
			inject: func(e *tenv) { e.sup.startErr = errBoomMsg("port busy") }},
		{name: "creating the database", errHas: "Creating the database my_shop failed: nope", svcCount: 0,
			inject: func(e *tenv) { e.db.createDBErr = errBoomMsg("nope") }},
		{name: "creating the user", errHas: "Creating the database user my_shop failed: nope", svcCount: 0,
			inject: func(e *tenv) { e.db.userEr = errBoomMsg("nope") }, dbCalls: []string{"dropdb"}},
		{name: "the generator", errHas: "Creating the app failed: composer exited with code 2: it broke", svcCount: 0, folder: true,
			dbCalls: []string{"dropdb", "dropuser"},
			inject: func(e *tenv) {
				e.run.lines["php-bin"] = "it broke"
				e.run.hook = func(c tasks.Cmd) error {
					os.MkdirAll(filepath.Join(c.Dir, "my-shop"), 0o755)
					os.WriteFile(filepath.Join(c.Dir, "my-shop", "keep.txt"), []byte("mine"), 0o644)
					return &tasks.ExitError{Code: 2}
				}
			}},
		{name: "writing the .env", errHas: "Writing .env failed", svcCount: 0, folder: true, dbCalls: []string{"dropdb", "dropuser"},
			inject: func(e *tenv) {
				e.run.hook = func(c tasks.Cmd) error { return os.MkdirAll(filepath.Join(c.Dir, "my-shop", ".env"), 0o755) } // a folder where the file goes
			}},
		{name: "creating the project", errHas: "Creating the site failed: domain my-shop.localhost is already used", svcCount: 0, folder: true,
			dbCalls: []string{"dropdb", "dropuser"},
			inject: func(e *tenv) {
				e.run.hook = func(c tasks.Cmd) error {
					os.MkdirAll(filepath.Join(c.Dir, "my-shop"), 0o755)
					// somebody else takes the domain while the generator runs
					return e.st.Update(func(d *store.Data) error {
						d.Sites = append(d.Sites, model.Site{ID: "site_x", Name: "Thief", Domain: "my-shop.localhost", Type: "proxy", UpstreamPort: 1234})
						return nil
					})
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, phpApp())
			e.install("composer", "2.8.0", true)
			c.inject(e)
			req := e.req("app")
			req.DatabaseKind = "mariadb"
			done := e.start(req)
			if done.State != "failed" || !strings.Contains(done.Error, c.errHas) {
				t.Fatalf("job %+v, want an error with %q", done, c.errHas)
			}
			d := e.st.Snapshot()
			if len(d.Services) != c.svcCount || len(d.Projects) != 0 {
				t.Errorf("left behind: %d services, %d projects", len(d.Services), len(d.Projects))
			}
			for _, s := range d.Sites {
				if s.ID != "site_x" {
					t.Errorf("a site was left: %+v", s)
				}
			}
			var drops []string
			for _, call := range e.calls() {
				if strings.HasPrefix(call, "drop") {
					drops = append(drops, strings.Fields(call)[0])
				}
			}
			if !slices.Equal(drops, c.dbCalls) {
				t.Errorf("drops %q want %q (all calls %q)", drops, c.dbCalls, e.calls())
			}
			dir := filepath.Join(e.parent, "my-shop")
			kept := strings.Contains(done.Error, "The folder "+dir+" was kept.")
			if kept != c.folder {
				t.Errorf("folder kept message %v, want %v: %s", kept, c.folder, done.Error)
			}
			if _, err := os.Stat(dir); (err == nil) != c.folder {
				t.Errorf("folder exists %v, want %v", err == nil, c.folder)
			}
			if c.name == "the generator" {
				if b, _ := os.ReadFile(filepath.Join(dir, "keep.txt")); string(b) != "mine" {
					t.Error("a file of the generator was deleted")
				}
				if !strings.Contains(done.Error, "newproject-my-shop.log") {
					t.Errorf("the output file is named: %s", done.Error)
				}
			}
			// the job's database service made by the job is gone with the failure
			if c.name == "starting the database" && len(e.sup.removed) != 1 {
				t.Errorf("removed %q", e.sup.removed)
			}
		})
	}
}

type msgErr string

func (m msgErr) Error() string  { return string(m) }
func errBoomMsg(s string) error { return msgErr(s) }

func TestFilesNeverOverwrite(t *testing.T) {
	e := newEnv(t, htmlApp())
	dir := filepath.Join(e.parent, "my-shop")
	os.MkdirAll(dir, 0o755) // an empty folder is allowed
	if done := e.start(e.req("html")); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "index.html")); string(b) != "<h1>My Shop</h1>\n" {
		t.Fatalf("%q", b)
	}
	// run the files step again on a folder that has the file: it is kept
	j := &job{m: e.m, p: &planned{fw: htmlApp()}, vals: Values{Name: "Other", Slug: "my-shop"}}
	j.p.Dir = dir
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("mine"), 0o644)
	if err := j.filesStep(htmlApp().Steps[0]); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "index.html")); string(b) != "mine" {
		t.Errorf("overwritten: %q", b)
	}
	// paths that leave the folder are refused
	for _, bad := range []string{"../x", "/etc/x", "a/../../x", "C:/x", "c:x"} {
		if err := j.filesStep(frameworks.Step{Type: "files", Files: map[string]string{bad: "x"}}); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if _, err := os.Stat(filepath.Join(e.parent, "x")); err == nil {
		t.Error("a file was written outside the project folder")
	}
}

func TestNodeStepsAndEnvironment(t *testing.T) {
	e := newEnv(t, nodeApp())
	e.prog("npm")
	e.install("node", "20.5.0", true) // fits min 20
	var seen tasks.Cmd
	e.run.hook = func(c tasks.Cmd) error { seen = c; return nil }
	req := e.req("nodeapp")
	req.Port = 4000
	if done := e.start(req); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	dir := filepath.Join(e.parent, "my-shop")
	if !strings.HasPrefix(filepath.Base(seen.Bin), "npm") || seen.Dir != dir || !slices.Equal(seen.Args, []string{"install"}) {
		t.Errorf("npm ran as %+v", seen)
	}
	if !slices.Contains(seen.Env, "CI=1") || !slices.Contains(seen.Env, "Z2D_KIND=node") {
		t.Errorf("env %q", seen.Env)
	}
	s := e.st.Snapshot().Services[0]
	if s.Port != 4000 || s.RuntimeID != "" || s.Settings["npmScript"] != "dev" {
		t.Errorf("service %+v", s)
	}
	if st := e.st.Snapshot().Sites[0]; st.Type != "proxy" || st.ServiceID != s.ID {
		t.Errorf("site %+v", st)
	}
	// a program that is not there fails the step
	e2 := newEnv(t, nodeApp())
	e2.install("node", "20.5.0", true)
	if done := e2.start(e2.req("nodeapp")); done.State != "failed" || !strings.Contains(done.Error, "Installing the packages failed: npm was not found") {
		t.Errorf("%+v", done)
	}
}

func TestPythonVirtualEnvironment(t *testing.T) {
	e := newEnv(t, pyApp())
	e.prog("pip")
	e.install("python", "3.13.1", true)
	if done := e.start(e.req("pyapp")); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	svcs := e.st.Snapshot().Services
	if len(svcs) != 1 || len(e.sup.prepared) != 1 || e.sup.prepared[0].ID != svcs[0].ID || e.sup.prepared[0].Kind != "python" {
		t.Fatalf("the virtual environment is made for the service that is created: %+v / %+v", e.sup.prepared, svcs)
	}
	venv := filepath.Join(e.home, "python", "venvs", svcs[0].ID)
	if b, _ := os.ReadFile(filepath.Join(e.parent, "my-shop", "requirements.txt")); string(b) != "flask\n# "+venv+"\n" {
		t.Errorf("{venv}: %q", b)
	}
	if len(e.run.cmds) != 1 || !strings.HasPrefix(filepath.Base(e.run.cmds[0].Bin), "pip") {
		t.Errorf("cmds %+v", e.run.cmds)
	}
}

func TestDownloadSteps(t *testing.T) {
	srv, base := zipServer(t, false)
	fw := frameworks.Framework{
		ID: "dl", Label: "DL", Language: "static", Runtime: frameworks.Runtime{},
		Steps: []frameworks.Step{
			{Type: "download", Label: "Downloading", URL: base + "/pkg.zip", Checksum: base + "/pkg.zip.sha1", Strip: 1},
			{Type: "files", Label: "Writing", Files: map[string]string{"extra.txt": "x"}},
		},
		Site: &frameworks.SiteTpl{Type: "static", Root: "."},
	}
	e := newEnv(t, fw)
	e.m.HTTP, e.m.AllowHTTP = srv.Client(), true
	if done := e.start(e.req("dl")); done.State != "done" {
		t.Fatalf("%+v", done)
	}
	dir := filepath.Join(e.parent, "my-shop")
	if b, _ := os.ReadFile(filepath.Join(dir, "wp-content", "readme.txt")); string(b) != "hi\n" {
		t.Errorf("stripped extraction: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "extra.txt")); err != nil {
		t.Error("the step after the download ran")
	}
	if left, _ := filepath.Glob(filepath.Join(e.home, "tmp", "newproject-*")); len(left) != 0 {
		t.Errorf("the download was not removed: %q", left)
	}

	// a wrong checksum fails the job and unpacks nothing
	bad, badBase := zipServer(t, true)
	fw.Steps[0].URL, fw.Steps[0].Checksum = badBase+"/pkg.zip", badBase+"/pkg.zip.sha1"
	e2 := newEnv(t, fw)
	e2.m.HTTP, e2.m.AllowHTTP = bad.Client(), true
	done := e2.start(e2.req("dl"))
	if done.State != "failed" || !strings.Contains(done.Error, "does not match its sha1 checksum") {
		t.Fatalf("%+v", done)
	}
	if _, err := os.Stat(filepath.Join(e2.parent, "my-shop", "index.php")); err == nil {
		t.Error("nothing may be unpacked after a wrong checksum")
	}
	if left, _ := filepath.Glob(filepath.Join(e2.home, "tmp", "newproject-*")); len(left) != 0 {
		t.Errorf("the download was not removed: %q", left)
	}

	// the checksum written in the list: sha1:<hex> works, an unverified download warns in the plan
	var fw3 = fw
	fw3.Steps = append([]frameworks.Step(nil), fw.Steps...)
	fw3.Steps[0] = frameworks.Step{Type: "download", Label: "Downloading", URL: base + "/pkg.zip", Unverified: true, Reason: "nobody publishes a checksum", Strip: 1}
	e3 := newEnv(t, fw3)
	e3.m.HTTP, e3.m.AllowHTTP = srv.Client(), true
	if p, _ := e3.m.Plan(e3.req("dl")); !slices.ContainsFunc(p.Warnings, func(s string) bool { return strings.Contains(s, "nobody publishes a checksum") }) {
		t.Errorf("warnings %q", p.Warnings)
	}
	if done := e3.start(e3.req("dl")); done.State != "done" {
		t.Fatalf("%+v", done)
	}
}

func TestChecksumFileFormats(t *testing.T) {
	for _, tc := range []struct{ spec, body, sum, algo string }{
		{"sha256:" + strings.Repeat("A", 64), "", strings.Repeat("a", 64), "sha256"},
		{"sha1:" + strings.Repeat("b", 40), "", strings.Repeat("b", 40), "sha1"},
	} {
		sum, algo, err := expectedHash(t.Context(), nil, tc.spec)
		if err != nil || sum != tc.sum || algo != tc.algo {
			t.Errorf("%s: %q %q %v", tc.spec, sum, algo, err)
		}
	}
}
