//go:build !windows

package supervisor

import (
	"os/signal"
	"syscall"
)

func ignoreTerm() { signal.Ignore(syscall.SIGTERM) }

func pidAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }
