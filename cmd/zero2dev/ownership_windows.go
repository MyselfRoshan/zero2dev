//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// An exclusive file handle is released by the kernel when the process exits.
func acquireOwnership(home string) (*os.File, error) {
	if err := os.MkdirAll(home, 0755); err != nil {
		return nil, err
	}
	path, err := syscall.UTF16PtrFromString(filepath.Join(home, ".instance.lock"))
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(path, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, syscall.Errno(32)) {
			return nil, nil
		}
		return nil, err
	}
	return os.NewFile(uintptr(h), "instance lock"), nil
}
