package fsutil

import (
	"os"
	"path/filepath"
	"strings"
)

// LinkTarget returns where the junction or symbolic link at path points, and
// false when path is no link (a real folder, a file, or nothing).
func LinkTarget(path string) (string, bool) {
	t, err := os.Readlink(path)
	if err != nil {
		return "", false
	}
	return strings.TrimPrefix(t, `\??\`), true
}

// SamePath reports whether a and b name the same place (case-insensitive on
// Windows paths, which is all Zero2Dev compares here).
func SamePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
