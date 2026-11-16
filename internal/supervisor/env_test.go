package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// envSetup makes a supervisor over a temp home with a fake java runtime
// (<home>/jdk/bin/java) and a Postgres service "db" on port 5544.
func envSetup(t *testing.T) (*Supervisor, string, model.Runtime) {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "jdk", "bin", "java")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	rt := model.Runtime{ID: "rt_java", Kind: "java", Version: "21", Name: "Java 21", Binary: bin, Default: true}
	st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, rt)
		d.Services = append(d.Services, model.Service{ID: "db", Name: "PG", Kind: "postgres", Port: 5544})
		return nil
	})
	return New(home, st, &recorder{}), home, rt
}

// last returns the final value of name in env (what a process sees).
func last(env []string, name string) (string, bool) {
	v, ok := "", false
	for _, kv := range env {
		if k, val, found := strings.Cut(kv, "="); found && k == name {
			v, ok = val, true
		}
	}
	return v, ok
}

func TestEnvironOrderLaterWins(t *testing.T) {
	s, home, rt := envSetup(t)
	cwd := filepath.Join(home, "app")
	os.MkdirAll(cwd, 0o755)
	os.WriteFile(filepath.Join(cwd, ".env"), []byte("DB_HOST=from-env-file\nFROM_FILE=1\nUSER_WINS=file\nPORT=1\nJAVA_HOME=file-java\n"), 0o644)
	svc := model.Service{ID: "app", Name: "App", Kind: "java", Cwd: cwd, DatabaseID: "db", EnvFile: ".env", Port: 8080,
		Env: map[string]string{"USER_WINS": "user", "DB_PORT": "{port}"}}
	env, err := s.Environ(svc)
	if err != nil {
		t.Fatal(err)
	}
	name, wantHome := model.RuntimeHome(rt)
	want := map[string]string{
		"JAVA_HOME":    "file-java",     // runtime home < .env
		"DB_HOST":      "from-env-file", // database < .env
		"DB_PORT":      "8080",          // database < user Env
		"DB_USERNAME":  "postgres",
		"DATABASE_URL": "postgresql://postgres@127.0.0.1:5544/postgres",
		"FROM_FILE":    "1",
		"USER_WINS":    "user", // .env < user Env
		"PORT":         "8080", // everything < PORT
	}
	for k, v := range want {
		if got, ok := last(env, k); !ok || got != v {
			t.Errorf("%s = %q (set %v), want %q", k, got, ok, v)
		}
	}
	if name != "JAVA_HOME" {
		t.Fatal(name)
	}
	// without the .env override the runtime home is set
	svc.EnvFile = ""
	env, _ = s.Environ(svc)
	if got, _ := last(env, "JAVA_HOME"); got != wantHome {
		t.Errorf("JAVA_HOME = %q, want %q", got, wantHome)
	}
	// the runtime is first on PATH
	if p, _ := last(env, "PATH"); !strings.HasPrefix(p, filepath.Dir(rt.Binary)+string(os.PathListSeparator)) {
		if p2, _ := last(env, "Path"); !strings.HasPrefix(p2, filepath.Dir(rt.Binary)+string(os.PathListSeparator)) {
			t.Errorf("runtime bin not first on PATH: %q", p)
		}
	}
}

func TestRuntimeHomeOnlyForOwnRuntime(t *testing.T) {
	s, home, rt := envSetup(t)
	// a custom command does not use the runtime: no JAVA_HOME from Zero2Dev
	t.Setenv("JAVA_HOME", "")
	env, err := s.Environ(model.Service{ID: "c", Kind: "custom", Command: os.Args[0], Cwd: home})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := last(env, "JAVA_HOME"); v != "" {
		t.Errorf("custom service got JAVA_HOME=%q", v)
	}
	// a node service never gets JAVA_HOME / GOROOT
	node := filepath.Join(home, "node", "node")
	os.MkdirAll(filepath.Dir(node), 0o755)
	os.WriteFile(node, []byte("x"), 0o755)
	s.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_node", Kind: "node", Binary: node, Default: true})
		return nil
	})
	t.Setenv("GOROOT", "")
	env, _ = s.Environ(model.Service{ID: "n", Kind: "node", Cwd: home})
	for _, k := range []string{"JAVA_HOME", "GOROOT"} {
		if v, _ := last(env, k); v != "" {
			t.Errorf("node service got %s=%q", k, v)
		}
	}
	// an explicit runtime of the kind is used even when it is not the default
	other := filepath.Join(home, "jdk8", "bin", "java")
	os.MkdirAll(filepath.Dir(other), 0o755)
	os.WriteFile(other, []byte("x"), 0o755)
	s.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_j8", Kind: "java", Binary: other})
		return nil
	})
	env, _ = s.Environ(model.Service{ID: "j", Kind: "java", RuntimeID: "rt_j8", Cwd: home})
	if v, _ := last(env, "JAVA_HOME"); v != filepath.Join(home, "jdk8") {
		t.Errorf("JAVA_HOME = %q", v)
	}
	_ = rt
}

func TestPreviewShowsAddedVariables(t *testing.T) {
	s, home, _ := envSetup(t)
	cwd := filepath.Join(home, "app")
	os.MkdirAll(cwd, 0o755)
	os.WriteFile(filepath.Join(cwd, ".env"), []byte("API_URL=http://x\nAPI_TOKEN=abc123\nDB_PASSWORD_FILE=\nCONN=mysql://u:pw@h/db\n"), 0o644)
	p := s.Preview(model.Service{ID: "app", Name: "App", Kind: "java", Cwd: cwd, DatabaseID: "db", EnvFile: ".env", Port: 8080})
	joined := "\n" + strings.Join(p.Env, "\n") + "\n"
	for _, want := range []string{"\nJAVA_HOME=", "\nDB_CONNECTION=pgsql\n", "\nDATABASE_URL=postgresql://postgres@127.0.0.1:5544/postgres\n", "\nAPI_URL=http://x\n", "\nPORT=8080\n"} {
		if !strings.Contains(joined, want) {
			t.Errorf("preview lacks %q in %q", want, p.Env)
		}
	}
	if strings.Contains(joined, "abc123") || strings.Contains(joined, "pw@") {
		t.Errorf("preview leaks a secret: %q", p.Env)
	}
	if !strings.Contains(joined, "\nAPI_TOKEN=(hidden)\n") || !strings.Contains(joined, "\nCONN=(hidden)\n") {
		t.Errorf("secrets not hidden: %q", p.Env)
	}
	// the process still gets the real values
	env, _ := s.Environ(model.Service{ID: "app", Kind: "java", Cwd: cwd, EnvFile: ".env"})
	if v, _ := last(env, "API_TOKEN"); v != "abc123" {
		t.Errorf("API_TOKEN = %q", v)
	}
}

func TestMissingEnvFileWarnsButStarts(t *testing.T) {
	s, home, _ := envSetup(t)
	svc := model.Service{ID: "app", Name: "App", Kind: "custom", Command: os.Args[0], Cwd: home, EnvFile: "missing.env"}
	p := s.Preview(svc)
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "not found") {
		t.Fatalf("warnings %q", p.Warnings)
	}
	r, err := s.resolve(svc, s.st.Snapshot())
	if err != nil {
		t.Fatalf("a missing .env must not fail the start: %v", err)
	}
	// prepare logs the warning
	if err := s.prepare("app", r); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Remove("app") }) // closes the log file
	if !hasLog(s, "app", "system", "missing.env") {
		t.Error("no log line about the missing .env file")
	}
}

func TestEnvFileOutsideFolder(t *testing.T) {
	s, home, _ := envSetup(t)
	svc := model.Service{ID: "app", Name: "App", Kind: "custom", Command: os.Args[0], Cwd: home, EnvFile: "../secret.env"}
	if _, err := s.resolve(svc, s.st.Snapshot()); err == nil {
		t.Error("an .env path that leaves the folder must fail the start")
	}
	if p := s.Preview(svc); len(p.Warnings) == 0 {
		t.Error("the preview must warn about it")
	}
}

func TestEnvFileLoadedLogLine(t *testing.T) {
	s, home, _ := envSetup(t)
	os.WriteFile(filepath.Join(home, ".env"), []byte("A=1\nB=2\n"), 0o644)
	svc := model.Service{ID: "app", Name: "App", Kind: "custom", Command: os.Args[0], Cwd: home, EnvFile: ".env"}
	r, err := s.resolve(svc, s.st.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	s.prepare("app", r)
	t.Cleanup(func() { s.Remove("app") })
	if !hasLog(s, "app", "system", "loaded 2 variables from .env") {
		t.Error("no 'loaded' log line")
	}
	if hasLog(s, "app", "system", "=1") {
		t.Error("a value was logged")
	}
}

func TestDisplayValue(t *testing.T) {
	for _, c := range []struct{ k, v, want string }{
		{"APP_NAME", "demo", "demo"},
		{"API_KEY", "x", "(hidden)"},
		{"DB_PASSWORD", "", ""},
		{"jwt_secret", "s", "(hidden)"},
		{"REDIS_URL", "redis://127.0.0.1:6379", "redis://127.0.0.1:6379"},
		{"MAIL", "smtp://user:pw@host:25", "(hidden)"},
		{"MAIL2", "smtp://user@host:25", "smtp://user@host:25"},
	} {
		if got := displayValue(c.k, c.v); got != c.want {
			t.Errorf("%s: %q want %q", c.k, got, c.want)
		}
	}
}
