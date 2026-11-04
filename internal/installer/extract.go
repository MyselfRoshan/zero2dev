package installer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxExtracted caps total extracted bytes (decompression-bomb guard).
const maxExtracted = 4 << 30

var errSlip = errors.New("archive entry escapes the destination (zip-slip), refusing to extract")

// entryTarget maps an archive entry name to a path inside dest after
// stripping `strip` leading components. skip is true for entries that vanish
// entirely (the stripped top-level folder itself). Any entry that is
// absolute, has a drive letter or a ".." component is an error: we refuse the
// whole archive rather than silently dropping it.
func entryTarget(dest, name string, strip int) (target string, skip bool, err error) {
	n := strings.ReplaceAll(name, "\\", "/") // treat backslashes as separators on every OS
	if strings.HasPrefix(n, "/") || filepath.VolumeName(n) != "" || (len(n) > 1 && n[1] == ':') {
		return "", false, errSlip
	}
	var parts []string
	for _, p := range strings.Split(n, "/") {
		switch p {
		case "", ".":
		case "..":
			return "", false, errSlip
		default:
			parts = append(parts, p)
		}
	}
	if len(parts) <= strip {
		return "", true, nil
	}
	target = filepath.Join(append([]string{dest}, parts[strip:]...)...)
	if !within(dest, target) {
		return "", false, errSlip
	}
	return target, false, nil
}

// within reports whether p is dest or below it (lexically).
func within(dest, p string) bool {
	rel, err := filepath.Rel(dest, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ensureDir creates dir after verifying, via the real path of its nearest
// existing ancestor, that no symlink inside dest redirects it outside.
func ensureDir(realDest, dir string) error {
	existing := dir
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		next := filepath.Dir(existing)
		if next == existing {
			break
		}
		existing = next
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil || !within(realDest, real) {
		return errSlip
	}
	return os.MkdirAll(dir, 0o755)
}

type extractor struct {
	realDest string
	dest     string
	strip    int
	written  int64
}

func newExtractor(dest string, strip int) (*extractor, error) {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return nil, err
	}
	return &extractor{realDest: real, dest: dest, strip: strip}, nil
}

func (x *extractor) writeFile(target string, mode os.FileMode, r io.Reader) error {
	if err := ensureDir(x.realDest, filepath.Dir(target)); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o200)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxExtracted-x.written+1))
	x.written += n
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && x.written > maxExtracted {
		err = errors.New("archive expands beyond the size limit")
	}
	return err
}

// extractZip unpacks a .zip. Symlink entries are ignored.
func extractZip(archive, dest string, strip int) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	x, err := newExtractor(dest, strip)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		target, skip, err := entryTarget(dest, f.Name, strip)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		if skip {
			continue
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := ensureDir(x.realDest, target); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			continue
		default:
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = x.writeFile(target, mode, rc)
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// extractTarGz unpacks a .tar.gz. Symlinks are kept when their target stays
// inside dest (Node's npm shims need them); hardlinks and devices are skipped.
func extractTarGz(archive, dest string, strip int) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	x, err := newExtractor(dest, strip)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, skip, err := entryTarget(dest, h.Name, strip)
		if err != nil {
			return fmt.Errorf("%s: %w", h.Name, err)
		}
		if skip {
			continue
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := ensureDir(x.realDest, target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := x.writeFile(target, os.FileMode(h.Mode), tr); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(h.Linkname) || strings.HasPrefix(h.Linkname, "/") {
				return fmt.Errorf("%s: absolute symlink target: %w", h.Name, errSlip)
			}
			if !within(dest, filepath.Join(filepath.Dir(target), filepath.FromSlash(h.Linkname))) {
				return fmt.Errorf("%s: symlink target escapes destination: %w", h.Name, errSlip)
			}
			if err := ensureDir(x.realDest, filepath.Dir(target)); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}
}
