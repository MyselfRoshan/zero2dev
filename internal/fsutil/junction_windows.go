//go:build windows

package fsutil

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf16"
)

const (
	fsctlSetReparsePoint = 0x000900A4
	reparseTagMountPoint = 0xA0000003
)

// Junction makes link a directory junction that points at the directory
// target. A junction needs no administrator rights or developer mode (unlike a
// symbolic link) and is followed by every program. link must not exist yet;
// target is made absolute.
func Junction(link, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	if err := setMountPoint(link, abs); err != nil {
		_ = os.Remove(link)
		return fmt.Errorf("junction %s -> %s: %w", link, abs, err)
	}
	return nil
}

func setMountPoint(link, target string) error {
	p, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)

	// REPARSE_DATA_BUFFER, MountPointReparseBuffer: the substitute name is the
	// NT path (\??\C:\...), the print name the plain one; both end in NUL.
	sub := utf16.Encode([]rune(`\??\` + target))
	prn := utf16.Encode([]rune(target))
	pathBytes := 2 * (len(sub) + 1 + len(prn) + 1)
	buf := make([]byte, 8+8+pathBytes)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], reparseTagMountPoint)
	le.PutUint16(buf[4:], uint16(8+pathBytes)) // ReparseDataLength
	le.PutUint16(buf[8:], 0)                   // SubstituteNameOffset
	le.PutUint16(buf[10:], uint16(2*len(sub))) // SubstituteNameLength
	le.PutUint16(buf[12:], uint16(2*(len(sub)+1)))
	le.PutUint16(buf[14:], uint16(2*len(prn)))
	o := 16
	for _, c := range sub {
		le.PutUint16(buf[o:], c)
		o += 2
	}
	o += 2
	for _, c := range prn {
		le.PutUint16(buf[o:], c)
		o += 2
	}
	var n uint32
	return syscall.DeviceIoControl(h, fsctlSetReparsePoint, &buf[0], uint32(len(buf)), nil, 0, &n, nil)
}
