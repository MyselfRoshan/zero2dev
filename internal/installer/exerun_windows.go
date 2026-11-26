//go:build windows

package installer

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

// configureExe hides the console window (the daemon is a GUI-subsystem program) and makes the end
// of the context (timeout) kill the whole process tree.
func configureExe(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		k := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		k.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
		_ = k.Run()
		return cmd.Process.Kill()
	}
}
