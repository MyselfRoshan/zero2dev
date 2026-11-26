package kinds

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func pythonCtx(t *testing.T) Ctx {
	c := ctx()
	c.Home = t.TempDir()
	c.Bin = filepath.Join(c.Home, "rt", "python-3.13", "python.exe")
	c.Port = "8000"
	c.ServiceID = "svc_py"
	return c
}

func TestBuildPythonRunModes(t *testing.T) {
	c := pythonCtx(t)
	venv := filepath.Join(c.Home, "python", "venvs", "svc_py")
	for _, tc := range []struct {
		name     string
		settings map[string]string
		extra    []string
		args     string
		env      []string
	}{
		{"script", nil, []string{"--debug"}, "main.py --debug", []string{"HOST=127.0.0.1"}},
		{"script path", map[string]string{"script": "src/run.py"}, nil, "src/run.py", nil},
		{"module", map[string]string{"run": "module", "module": "myapp.server"}, []string{"-v"}, "-m myapp.server -v", nil},
		{"uvicorn", map[string]string{"run": "uvicorn"}, []string{"--reload"}, "-m uvicorn --host 127.0.0.1 --port 8000 main:app --reload", nil},
		{"uvicorn host", map[string]string{"run": "uvicorn", "app": "api.main:create_app", "host": "0.0.0.0"}, nil, "-m uvicorn --host 0.0.0.0 --port 8000 api.main:create_app", nil},
		{"flask", map[string]string{"run": "flask", "flaskApp": "myapp:create_app()"}, nil, "-m flask run --host 127.0.0.1 --port 8000", []string{"FLASK_APP=myapp:create_app()"}},
		{"django", map[string]string{"run": "django"}, []string{"--noreload"}, "manage.py runserver 127.0.0.1:8000 --noreload", nil},
	} {
		if err := Validate("python", tc.settings); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		pl := Build("python", tc.settings, tc.extra, c)
		if got := strings.Join(pl.Args, " "); got != tc.args {
			t.Errorf("%s: args %q, want %q", tc.name, got, tc.args)
		}
		for _, e := range append(tc.env, "PYTHONUNBUFFERED=1", "VIRTUAL_ENV="+venv) {
			if !slices.Contains(pl.Env, e) {
				t.Errorf("%s: env lacks %s: %q", tc.name, e, pl.Env)
			}
		}
		if want := VenvPython(venv, true); pl.Program != want {
			t.Errorf("%s: program %q, want %q", tc.name, pl.Program, want)
		}
	}
	for _, bad := range []map[string]string{{"run": "ruby"}, {"script": "a b;c"}, {"run": "module", "module": "a-b"}, {"run": "uvicorn", "app": "main"}, {"host": "a b"}} {
		if Validate("python", bad) == nil {
			t.Errorf("%v must be refused", bad)
		}
	}
	if Validate("python", map[string]string{"run": "module"}) == nil {
		t.Error("a module run needs a module")
	}
}

func TestBuildPythonVirtualEnvironmentStep(t *testing.T) {
	c := pythonCtx(t)
	venv := filepath.Join(c.Home, "python", "venvs", "svc_py")
	py := VenvPython(venv, true)
	pl := Build("python", nil, nil, c)
	if len(pl.Pre) != 1 {
		t.Fatalf("pre %+v", pl.Pre)
	}
	st := pl.Pre[0]
	if st.Program != c.Bin || strings.Join(st.Args, " ") != "-m venv "+venv || st.Skip != py {
		t.Errorf("step %+v", st)
	}
	if len(pl.Warnings) != 0 {
		t.Errorf("warnings %q", pl.Warnings)
	}

	// An environment made from the same Python is kept; one made from another is made again.
	cfg := filepath.Join(venv, "pyvenv.cfg")
	if err := os.MkdirAll(venv, 0o755); err != nil {
		t.Fatal(err)
	}
	same := "home = " + strings.ToUpper(filepath.Dir(c.Bin)) + "\nversion = 3.13.18\n" // paths ignore case
	if err := os.WriteFile(cfg, []byte(same), 0o644); err != nil {
		t.Fatal(err)
	}
	if pl = Build("python", nil, nil, c); pl.Pre[0].Skip != py || len(pl.Warnings) != 0 {
		t.Errorf("same runtime: %+v %q", pl.Pre[0], pl.Warnings)
	}
	other := "home = " + filepath.Join(c.Home, "rt", "python-3.12") + "\n"
	if err := os.WriteFile(cfg, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	pl = Build("python", nil, nil, c)
	if st := pl.Pre[0]; st.Skip != "" || strings.Join(st.Args, " ") != "-m venv --clear "+venv {
		t.Errorf("another runtime: %+v", st)
	}
	if len(pl.Warnings) != 1 || !strings.Contains(pl.Warnings[0], "made again") {
		t.Errorf("warnings %q", pl.Warnings)
	}

	// No runtime known: the service still points at its environment, nothing to run.
	c.Bin = ""
	if pl = Build("python", nil, nil, c); len(pl.Pre) != 0 || pl.Program != py {
		t.Errorf("no runtime: %+v", pl)
	}
	// Arguments written by the user: no environment.
	c.Bin = filepath.Join(c.Home, "rt", "python.exe")
	if pl = BuildCustom("python", []string{"-c", "1"}, c); pl.Program != "" || len(pl.Pre) != 0 {
		t.Errorf("custom: %+v", pl)
	}
}

func TestPythonTasks(t *testing.T) {
	p, ok := Lookup("python")
	if !ok {
		t.Fatal("no python profile")
	}
	got := map[string]Task{}
	for _, tk := range p.Tasks {
		got[tk.ID] = tk
	}
	if tk := got["pip-install"]; tk.Program != "pip" || tk.When != "requirements.txt" || strings.Join(tk.Args, " ") != "install -r requirements.txt" {
		t.Errorf("pip-install %+v", tk)
	}
	if tk := got["pip-list"]; tk.When != "" || tk.Program != "pip" {
		t.Errorf("pip-list %+v", tk)
	}
	for _, id := range []string{"django-migrate", "django-collectstatic"} {
		if tk := got[id]; tk.When != "manage.py" || tk.Program != "python" || tk.Args[0] != "manage.py" {
			t.Errorf("%s %+v", id, tk)
		}
	}
	if got["django-migrate"].Confirm == "" {
		t.Error("a migration asks first")
	}
}
