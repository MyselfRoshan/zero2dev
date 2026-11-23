//go:build !windows

package installer

import (
	"os/exec"
	"syscall"
)

// configureExe puts the child in its own process group, so the whole tree can be ended.
func configureExe(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
