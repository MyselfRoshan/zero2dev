package supervisor

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zero2dev/internal/kinds"
)

type preCall struct {
	bin  string
	args []string
	dir  string
}

func newPreSup(t *testing.T) (*Supervisor, *[]preCall) {
	s, _, _ := envSetup(t)
	t.Cleanup(func() { s.Remove("svc") }) // closes the service's log file (Windows cannot delete it open)
	var calls []preCall
	s.PreRun = func(_ context.Context, bin string, args []string, dir string, env []string, out io.Writer) error {
		calls = append(calls, preCall{bin, args, dir})
		io.WriteString(out, "line one\nline two\n")
		return nil
	}
	return s, &calls
}

func TestRunPreRunsStepsInOrderAndSkipsDoneOnes(t *testing.T) {
	s, calls := newPreSup(t)
	tools := t.TempDir()
	prog := filepath.Join(tools, "tool.exe")
	if err := os.WriteFile(prog, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	done := filepath.Join(tools, "done.marker")
	if err := os.WriteFile(done, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &resolved{workdir: tools, pre: []kinds.PreStep{
		{Label: "First", Program: prog, Args: []string{"a", "b"}},
		{Label: "Skipped", Program: prog, Skip: done},
		{Label: "Third", Program: prog, Skip: filepath.Join(tools, "missing.marker"), Dir: tools},
	}}
	if err := s.runPre("svc", r); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || (*calls)[0].args[0] != "a" || (*calls)[0].dir != tools {
		t.Fatalf("calls %+v", *calls)
	}
	var log []string
	for _, l := range s.Logs("svc", 50) {
		log = append(log, l.Text)
	}
	got := strings.Join(log, "\n")
	for _, want := range []string{"First: tool.exe a b", "line one", "Third:"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "Skipped") {
		t.Errorf("a done step must not run or log: %s", got)
	}
}

func TestRunPreFailureNamesTheStep(t *testing.T) {
	s, _ := newPreSup(t)
	s.PreRun = func(context.Context, string, []string, string, []string, io.Writer) error { return os.ErrPermission }
	prog := filepath.Join(t.TempDir(), "x.exe")
	os.WriteFile(prog, []byte("x"), 0o755)
	err := s.runPre("svc", &resolved{pre: []kinds.PreStep{{Label: "Creating the virtual environment", Program: prog}}})
	if err == nil || !strings.Contains(err.Error(), "Creating the virtual environment failed") {
		t.Fatalf("%v", err)
	}
	// A program that is not there is a clear error too, before anything runs.
	err = s.runPre("svc", &resolved{env: []string{"PATH=" + t.TempDir()}, pre: []kinds.PreStep{{Label: "Corepack", Program: "corepack"}}})
	if err == nil || !strings.Contains(err.Error(), "corepack was not found on the service's PATH") {
		t.Fatalf("%v", err)
	}
}

func TestFindProgram(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	ext := ""
	if os.PathSeparator == '\\' {
		ext = ".exe"
	}
	for _, d := range []string{a, b} {
		if err := os.WriteFile(filepath.Join(d, "pip"+ext), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"Path=" + a + string(os.PathListSeparator) + b}
	got, err := FindProgram("pip", env)
	if err != nil || filepath.Dir(got) != a {
		t.Fatalf("the first PATH entry wins: %q %v", got, err)
	}
	if _, err := FindProgram("nope", env); err == nil {
		t.Fatal("missing")
	}
	for _, bad := range []string{"", `..\x`, "a/b", "x\ny"} {
		if _, err := FindProgram(bad, env); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	abs := filepath.Join(b, "pip"+ext)
	if got, err := FindProgram(abs, nil); err != nil || got != abs {
		t.Fatalf("absolute: %q %v", got, err)
	}
	if _, err := FindProgram(filepath.Join(b, "missing"), nil); err == nil {
		t.Fatal("absolute but missing")
	}
}
