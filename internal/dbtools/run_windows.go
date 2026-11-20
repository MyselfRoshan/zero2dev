//go:build windows

package dbtools

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// hideWindow keeps the console programs (mariadb.exe, psql.exe ...) from
// flashing a console window: the daemon is a GUI-subsystem program.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
