package migrate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func itoa(n int) string { return strconv.Itoa(n) }

func exists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Lstat(p)
	return err == nil
}

// hasContent reports whether p is a file with bytes or a folder with anything in it.
func hasContent(p string) bool {
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	if !st.IsDir() {
		return st.Size() > 0
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	names, _ := f.Readdirnames(1)
	return len(names) > 0
}

// skipSet turns top-level names into a lookup (case-insensitive).
func skipSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	s := map[string]bool{}
	for _, n := range names {
		s[strings.ToLower(n)] = true
	}
	return s
}

// skipped reports whether rel (relative to the copied root) is left out:
// only top-level names are matched.
func skipped(skip map[string]bool, rel string) bool {
	if skip == nil || rel == "." {
		return false
	}
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	return skip[strings.ToLower(first)]
}

// treeStats counts the regular files below p (or p itself) and their bytes.
func treeStats(p string, skip []string) (files int, bytes int64, err error) {
	sk := skipSet(skip)
	err = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(p, path)
		if skipped(sk, rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			files++
			bytes += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	return files, bytes, err
}

func treeSize(p string, skip []string) (int64, error) {
	_, n, err := treeStats(p, skip)
	return n, err
}

// copyPath copies a folder or a file src to dst: folders, files (mode and
// modification time) and symlinks (best effort). prog (may be nil) gets the
// bytes of every file copied.
func copyPath(src, dst string, skip []string, prog func(int64)) error {
	sk := skipSet(skip)
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if skipped(sk, rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := dst
		if rel != "." {
			target = filepath.Join(dst, rel)
		}
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
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.Symlink(l, target) // may need a privilege on Windows
			return nil
		case !info.Mode().IsRegular():
			return nil
		default:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			n, err := copyFile(p, target, info)
			if err != nil {
				return err
			}
			if prog != nil {
				prog(n)
			}
			return nil
		}
	})
}

func copyFile(src, dst string, info fs.FileInfo) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm()|0o200)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, err
	}
	_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	return n, nil
}

// verify checks that dst holds as many files and bytes as src.
func verify(src, dst string, skip []string) error {
	sf, sb, err := treeStats(src, skip)
	if err != nil {
		return err
	}
	df, db, err := treeStats(dst, nil)
	if err != nil {
		return err
	}
	if sf != df || sb != db {
		return fmt.Errorf("the copy in %s is incomplete (%d files, %d bytes instead of %d files, %d bytes)", dst, df, db, sf, sb)
	}
	return nil
}

// defaultSameVolume compares the drive letters (or UNC shares) of a and b.
func defaultSameVolume(a, b string) bool {
	va, vb := filepath.VolumeName(absPath(a)), filepath.VolumeName(absPath(b))
	return strings.EqualFold(va, vb)
}

func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// Move strategies.
const (
	moveRename = "rename"
	moveCopy   = "copy" // copy, verify, then delete the original
)

// moveStrategy is how src is moved to dst: a rename on one volume, else a
// copy that is checked before the original is deleted.
func (m *Manager) moveStrategy(src, dst string) string {
	if m.sameVolume(src, dst) {
		return moveRename
	}
	return moveCopy
}

// movePath moves src (a folder or a file) to dst, which must not exist. On
// failure src is as it was and dst does not exist.
func (m *Manager) movePath(src, dst string, prog func(int64)) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if m.moveStrategy(src, dst) == moveRename {
		size, _ := treeSize(src, nil)
		if err := os.Rename(src, dst); err == nil {
			if prog != nil {
				prog(size)
			}
			return nil
		}
		// A rename can still fail on one volume (a mount point, a junction):
		// fall back to copying.
	}
	if err := copyPath(src, dst, nil, prog); err != nil {
		_ = os.RemoveAll(dst)
		return fmt.Errorf("cannot copy %s: %w", src, err)
	}
	if err := verify(src, dst, nil); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		// Part of the original may be gone: put it back from the full copy.
		if rerr := copyPath(dst, src, nil, nil); rerr != nil {
			return fmt.Errorf("cannot remove %s: %v; putting it back also failed (%v), a full copy is in %s", src, err, rerr, dst)
		}
		_ = os.RemoveAll(dst)
		return fmt.Errorf("cannot remove %s (is a program using it?): %w", src, err)
	}
	return nil
}
