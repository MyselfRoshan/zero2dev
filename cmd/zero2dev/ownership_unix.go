//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

// Keep the lock file in place: unlinking it would allow two owners of different inodes.
func lockHome(home string) (func(), error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(home, "instance.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
