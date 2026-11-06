//go:build !windows

package launch

import (
	"os/exec"
	"syscall"
)

// detach starts the program in its own session so it outlives the daemon.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
