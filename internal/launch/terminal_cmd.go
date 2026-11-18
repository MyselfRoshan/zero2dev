package launch

import "path/filepath"

// terminalArgs are the fixed arguments that open a console in dir. Nothing
// here is built from user text: dir is one argv element, never part of a shell
// string. With Windows Terminal (wtPath is wt.exe) it is
// `wt.exe -d <dir> cmd.exe /k`; without it `cmd.exe /k` in a new console window
// (the caller sets the working directory).
func terminalArgs(wtPath, dir string) (program string, args []string, newConsole bool) {
	if wtPath != "" {
		return wtPath, []string{"-d", filepath.Clean(dir), "cmd.exe", "/k"}, false
	}
	return "cmd.exe", []string{"/k"}, true
}
