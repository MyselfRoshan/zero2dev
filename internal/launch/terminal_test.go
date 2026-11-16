package launch

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestTerminalArgs(t *testing.T) {
	dir := filepath.Join("C:", "my projects", "app; calc")
	prog, args, nc := terminalArgs(`C:\wt\wt.exe`, dir)
	if prog != `C:\wt\wt.exe` || nc || !reflect.DeepEqual(args, []string{"-d", filepath.Clean(dir), "cmd.exe", "/k"}) {
		t.Errorf("wt: %q %q %v", prog, args, nc)
	}
	// the folder is one argument, whatever it contains
	if len(args) != 4 || args[1] != filepath.Clean(dir) {
		t.Errorf("dir must be a single argv element: %q", args)
	}
	prog, args, nc = terminalArgs("", dir)
	if prog != "cmd.exe" || !nc || !reflect.DeepEqual(args, []string{"/k"}) {
		t.Errorf("cmd: %q %q %v", prog, args, nc)
	}
}
