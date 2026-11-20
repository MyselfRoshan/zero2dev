//go:build !windows

package fsutil

import (
	"os"
	"path/filepath"
)

// Junction makes link point at the directory target. Outside Windows it is a
// symbolic link, which needs no rights there.
func Junction(link, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	return os.Symlink(abs, link)
}
