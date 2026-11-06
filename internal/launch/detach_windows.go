//go:build windows

package launch

import (
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000 // no console for .cmd wrappers such as code.cmd; GUI windows still open
)

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | createNoWindow}
}
