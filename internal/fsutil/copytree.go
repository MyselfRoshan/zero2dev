package fsutil

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// CopyTree copies src into dst: folders, files with their modes, and symlinks
// (best effort: a symlink that cannot be created is skipped). dst is created
// when missing; files already in dst are overwritten.
func CopyTree(src, dst string) error { return CopyTreeProgress(src, dst, nil) }

// CopyTreeProgress is CopyTree that reports progress: fn (may be nil) is
// called with the bytes copied so far and the total size of src (measured
// first with DirSize), after every file and once at the end.
func CopyTreeProgress(src, dst string, fn func(done, total int64)) error {
	var total, done int64
	if fn != nil {
		var err error
		if total, err = DirSize(src); err != nil {
			return err
		}
	}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case info.Mode()&os.ModeSymlink != 0:
			l, err := os.Readlink(p)
			if err != nil {
				return err
			}
			_ = os.Symlink(l, target) // symlinks may be unsupported (no privilege on Windows)
			return nil
		default:
			n, err := copyFile(p, target, info.Mode().Perm())
			if err != nil {
				return err
			}
			done += n
			if fn != nil {
				fn(done, total)
			}
			return nil
		}
	})
	if err == nil && fn != nil {
		fn(total, total)
	}
	return err
}

func copyFile(src, dst string, perm os.FileMode) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm|0o200)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if err != nil {
		out.Close()
		return n, err
	}
	return n, out.Close()
}

// DirSize is the total size in bytes of the regular files under path (a
// single file is its own size). A missing path is 0 with no error.
func DirSize(path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}
