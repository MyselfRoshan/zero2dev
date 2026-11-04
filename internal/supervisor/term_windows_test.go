//go:build windows

package supervisor

func ignoreTerm() {}

func pidAlive(pid int) bool { return false }
