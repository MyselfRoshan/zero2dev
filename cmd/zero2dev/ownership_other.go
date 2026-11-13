//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// The kernel releases this lock on exit, including crashes. Never unlink it:
// concurrent launches must always lock the same inode.
func acquireOwnership(home string) (*os.File, error) {
	if err := os.MkdirAll(home, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(home, ".instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, err
	}
	return f, nil
}
