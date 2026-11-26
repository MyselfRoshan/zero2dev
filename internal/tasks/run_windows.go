//go:build windows

package tasks

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNoWindow = 0x08000000

// configure keeps the console programs (npm, composer, go ...) from flashing a console window (the
// daemon is a GUI-subsystem program) and makes a cancel end the whole process tree.
func configure(cmd *exec.Cmd) {
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
