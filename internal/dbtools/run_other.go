//go:build !windows

package dbtools

import "os/exec"

func hideWindow(*exec.Cmd) {}
