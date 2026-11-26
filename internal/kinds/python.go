package kinds

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// A Python service runs in a virtual environment of its own at <home>\python\venvs\<serviceId>:
// the packages of one project never mix with another's or with the runtime's. It is made by a
// pre-step before the first start (python -m venv), made again when the service's runtime changed
// (pyvenv.cfg says which Python it was made from) and removed with the service.

// VenvDir is the virtual environment of a service.
func VenvDir(home, serviceID string) string {
	return filepath.Join(home, "python", "venvs", serviceID)
}

// VenvScripts is the folder of a virtual environment that holds python, pip and the tools its
// packages install (Scripts on Windows, bin elsewhere).
func VenvScripts(dir string, windows bool) string {
	if windows {
		return filepath.Join(dir, "Scripts")
	}
	return filepath.Join(dir, "bin")
}

// VenvPython is the interpreter a service of the venv runs.
func VenvPython(dir string, windows bool) string {
	if windows {
		return filepath.Join(VenvScripts(dir, true), "python.exe")
	}
	return filepath.Join(VenvScripts(dir, false), "python")
}

// pythonPlan makes the service run the interpreter of its virtual environment, with
// VIRTUAL_ENV set (the supervisor puts the Scripts folder first on PATH), and adds the pre-step that
// makes the environment from the runtime's python (c.Bin).
func pythonPlan(pl *Plan, c Ctx) {
	dir := VenvDir(c.Home, c.ServiceID)
	py := VenvPython(dir, c.Windows)
	pl.Program = py
	pl.Env = append(pl.Env, "VIRTUAL_ENV="+dir)
	if c.Bin == "" {
		return
	}
	step := PreStep{Label: "Creating the virtual environment", Program: c.Bin, Args: []string{"-m", "venv", dir}, Skip: py, Timeout: 300}
	if home, ok := venvHome(dir); ok && !samePath(home, filepath.Dir(c.Bin)) {
		// The environment was made from another Python (the service's runtime changed or was
		// replaced): its packages may not fit this one, so it is made again.
		step.Args = []string{"-m", "venv", "--clear", dir}
		step.Skip = ""
		pl.Warnings = append(pl.Warnings, "The Python of this service changed (the virtual environment was made from "+home+"): the virtual environment is made again, install the packages again (pip install -r requirements.txt)")
	}
	pl.Pre = append(pl.Pre, step)
}

// venvHome reads the "home" line of <dir>\pyvenv.cfg: the folder of the Python the environment
// was made from. ok is false when there is no such file or line.
func venvHome(dir string) (string, bool) {
	f, err := os.Open(filepath.Join(dir, "pyvenv.cfg"))
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "home") {
			if v = strings.TrimSpace(v); v != "" {
				return v, true
			}
		}
	}
	return "", false
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.ToSlash(filepath.Clean(a)), filepath.ToSlash(filepath.Clean(b)))
}
