//go:build windows

package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// The registry is reached through advapi32.dll with the standard library only
// (go.mod has no dependencies), like internal/envreg. Only keys below
// HKCU\Software\ are ever opened.

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	pRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	pRegCreateKeyExW = advapi32.NewProc("RegCreateKeyExW")
	pRegEnumValueW   = advapi32.NewProc("RegEnumValueW")
	pRegEnumKeyExW   = advapi32.NewProc("RegEnumKeyExW")
	pRegSetValueExW  = advapi32.NewProc("RegSetValueExW")
	pRegDeleteTreeW  = advapi32.NewProc("RegDeleteTreeW")
	pRegDeleteKeyW   = advapi32.NewProc("RegDeleteKeyW")
	pRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019
	keyWrite        = 0x20006

	errFileNotFound = 2
	errMoreData     = 234
	errNoMoreItems  = 259
)

type hkcu struct{}

func systemRegistry() Registry { return hkcu{} }

func checkPath(path string) error {
	if !strings.HasPrefix(strings.ToLower(path), `software\`) || strings.Contains(path, `..`) {
		return fmt.Errorf("registry path %s is not allowed", path)
	}
	return nil
}

func openKey(path string, access uint32) (syscall.Handle, bool, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, false, err
	}
	var h syscall.Handle
	r, _, _ := pRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(p)), 0, uintptr(access), uintptr(unsafe.Pointer(&h)))
	if r == errFileNotFound {
		return 0, false, nil
	}
	if r != 0 {
		return 0, false, syscall.Errno(r)
	}
	return h, true, nil
}

func closeKey(h syscall.Handle) { _, _, _ = pRegCloseKey.Call(uintptr(h)) }

func (hkcu) ReadTree(path string) ([]RegValue, bool, error) {
	if err := checkPath(path); err != nil {
		return nil, false, err
	}
	var out []RegValue
	ok, err := readKey(path, "", &out)
	return out, ok, err
}

func readKey(root, sub string, out *[]RegValue) (bool, error) {
	full := root
	if sub != "" {
		full += `\` + sub
	}
	h, ok, err := openKey(full, keyRead)
	if err != nil || !ok {
		return ok, err
	}
	defer closeKey(h)
	name := make([]uint16, 16384)
	data := make([]byte, 64*1024)
	for i := 0; ; i++ {
		nameLen, dataLen, typ := uint32(len(name)), uint32(len(data)), uint32(0)
		r, _, _ := pRegEnumValueW.Call(uintptr(h), uintptr(i), uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nameLen)),
			0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&dataLen)))
		if r == errNoMoreItems {
			break
		}
		if r == errMoreData {
			data = make([]byte, dataLen+2)
			i-- // read the same value again
			continue
		}
		if r != 0 {
			return true, syscall.Errno(r)
		}
		*out = append(*out, RegValue{Key: sub, Name: syscall.UTF16ToString(name[:nameLen]), Type: typ, Data: append([]byte(nil), data[:dataLen]...)})
	}
	var subs []string
	kname := make([]uint16, 256)
	for i := 0; ; i++ {
		n := uint32(len(kname))
		r, _, _ := pRegEnumKeyExW.Call(uintptr(h), uintptr(i), uintptr(unsafe.Pointer(&kname[0])), uintptr(unsafe.Pointer(&n)), 0, 0, 0, 0)
		if r == errNoMoreItems {
			break
		}
		if r != 0 {
			return true, syscall.Errno(r)
		}
		subs = append(subs, syscall.UTF16ToString(kname[:n]))
	}
	for _, s := range subs {
		next := s
		if sub != "" {
			next = sub + `\` + s
		}
		if _, err := readKey(root, next, out); err != nil {
			return true, err
		}
	}
	return true, nil
}

func (hkcu) WriteValues(path string, vals []RegValue) error {
	if err := checkPath(path); err != nil {
		return err
	}
	byKey := map[string][]RegValue{}
	order := []string{""}
	for _, v := range vals {
		k := strings.Trim(v.Key, `\`)
		if _, ok := byKey[k]; !ok && k != "" {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], v)
	}
	for _, k := range order {
		full := path
		if k != "" {
			full += `\` + k
		}
		p, err := syscall.UTF16PtrFromString(full)
		if err != nil {
			return err
		}
		var h syscall.Handle
		r, _, _ := pRegCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(p)), 0, 0, 0, uintptr(keyWrite), 0, uintptr(unsafe.Pointer(&h)), 0)
		if r != 0 {
			return fmt.Errorf("cannot create HKCU\\%s: %w", full, syscall.Errno(r))
		}
		for _, v := range byKey[k] {
			n, err := syscall.UTF16PtrFromString(v.Name)
			if err != nil {
				closeKey(h)
				return err
			}
			var dp uintptr
			if len(v.Data) > 0 {
				dp = uintptr(unsafe.Pointer(&v.Data[0]))
			}
			r, _, _ := pRegSetValueExW.Call(uintptr(h), uintptr(unsafe.Pointer(n)), 0, uintptr(v.Type), dp, uintptr(len(v.Data)))
			if r != 0 {
				closeKey(h)
				return fmt.Errorf("cannot write %s in HKCU\\%s: %w", v.Name, full, syscall.Errno(r))
			}
		}
		closeKey(h)
	}
	return nil
}

func (hkcu) DeleteTree(path string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, _ := pRegDeleteTreeW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(p)))
	if r != 0 && r != errFileNotFound {
		return syscall.Errno(r)
	}
	// RegDeleteTree empties the key; remove the key itself too.
	r, _, _ = pRegDeleteKeyW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(p)))
	if r != 0 && r != errFileNotFound {
		return syscall.Errno(r)
	}
	return nil
}

var pGetDiskFreeSpaceExW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// diskFree is the free bytes available to this user on the drive of path
// (its nearest existing folder), or 0 when unknown.
func diskFree(path string) int64 {
	for p := path; p != ""; {
		if _, err := os.Stat(p); err == nil {
			break
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
		path = p
	}
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	var avail, total, free uint64
	r, _, _ := pGetDiskFreeSpaceExW.Call(uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if r == 0 {
		return 0
	}
	return int64(avail)
}

// processRunning reports whether a process with this image name (Code.exe)
// runs in this session, from a Toolhelp snapshot of the process list.
func processRunning(image string) bool {
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(snap)
	var pe syscall.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = syscall.Process32First(snap, &pe); err == nil; err = syscall.Process32Next(snap, &pe) {
		if strings.EqualFold(syscall.UTF16ToString(pe.ExeFile[:]), image) {
			return true
		}
	}
	return false
}
