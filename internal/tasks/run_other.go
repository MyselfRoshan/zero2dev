//go:build !windows

package tasks

import (
	"os/exec"
	"syscall"
)

// configure puts the child in its own process group, so the whole tree can be ended.
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
