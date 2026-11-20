package supervisor

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"zero2dev/internal/kinds"
	"zero2dev/internal/model"
	"zero2dev/internal/store"
)

// pythonSetup is a supervisor with a (fake) Python runtime and a Python service "py1".
func pythonSetup(t *testing.T) (*Supervisor, model.Service, string) {
	t.Helper()
	s, home, _ := envSetup(t)
	bin := filepath.Join(home, "rt", "python-3.13", "python.exe")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := model.Service{ID: "py1", Name: "API", Kind: "python", Cwd: home, Port: 8000, Settings: map[string]string{"run": "uvicorn"}}
	s.st.Update(func(d *store.Data) error {
		d.Runtimes = append(d.Runtimes, model.Runtime{ID: "rt_py", Kind: "python", Version: "3.13.18", Name: "Python 3.13.18", Binary: bin, Default: true})
		d.Services = append(d.Services, svc)
		return nil
	})
	t.Cleanup(func() { s.Remove("py1") })
	return s, svc, bin
}

func TestPythonServiceRunsInItsVirtualEnvironment(t *testing.T) {
	s, svc, bin := pythonSetup(t)
	win := runtime.GOOS == "windows"
	venv := kinds.VenvDir(s.home, "py1")
	py := kinds.VenvPython(venv, win)

	// Before the first start the environment does not exist; the preview still shows its python
	// and does not complain about it.
	p := s.Preview(svc)
	if p.Program != py {
		t.Fatalf("program %q, want %q", p.Program, py)
	}
	for _, w := range p.Warnings {
		if strings.Contains(w, "not found") {
			t.Errorf("warning %q", w)
		}
	}
	if strings.Join(p.Args, " ") != "-m uvicorn --host 127.0.0.1 --port 8000 main:app" {
		t.Errorf("args %q", p.Args)
	}
	r, err := s.resolve(svc, s.st.Snapshot())
	if err != nil {
		t.Fatalf("the missing environment must not refuse the start: %v", err)
	}
	if len(r.pre) != 1 || r.pre[0].Program != bin || r.pre[0].Skip != py {
		t.Fatalf("pre %+v", r.pre)
	}
	var path, virt string
	for _, kv := range r.env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") && path == "" {
			path = v
		} else if ok && k == "VIRTUAL_ENV" {
			virt = v
		}
	}
	if !strings.HasPrefix(path, kinds.VenvScripts(venv, win)+string(os.PathListSeparator)+filepath.Dir(bin)) {
		t.Errorf("PATH must start with the environment's scripts, then the runtime: %q", path)
	}
	if virt != venv {
		t.Errorf("VIRTUAL_ENV %q", virt)
	}

	// PrepareService makes it (the fake runner stands in for python -m venv); a second call skips.
	runs := 0
	s.PreRun = func(_ context.Context, b string, args []string, _ string, _ []string, _ io.Writer) error {
		runs++
		if b != bin || strings.Join(args, " ") != "-m venv "+venv {
			t.Errorf("ran %s %q", b, args)
		}
		os.MkdirAll(filepath.Dir(py), 0o755)
		return os.WriteFile(py, []byte("x"), 0o755)
	}
	for i := 0; i < 2; i++ {
		if err := s.PrepareService(svc); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 {
		t.Fatalf("the environment must be made once, ran %d times", runs)
	}

	// The tool lookup finds pip in the environment first.
	pip := filepath.Join(filepath.Dir(py), "pip")
	if win {
		pip += ".exe"
	}
	os.WriteFile(pip, []byte("x"), 0o755)
	env, _ := s.Environ(svc)
	if got, err := FindProgram("pip", env); err != nil || got != pip {
		t.Errorf("pip: %q %v", got, err)
	}

	// Removing the service removes its environment.
	if err := s.Remove("py1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(venv); !os.IsNotExist(err) {
		t.Errorf("the virtual environment must be deleted with the service: %v", err)
	}
}

func TestPythonCustomArgsUseNoEnvironment(t *testing.T) {
	s, svc, bin := pythonSetup(t)
	svc.CustomArgs, svc.Args = true, []string{"-c", "print(1)"}
	r, err := s.resolve(svc, s.st.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if r.bin != bin || len(r.pre) != 0 {
		t.Errorf("bin %q pre %+v", r.bin, r.pre)
	}
}
