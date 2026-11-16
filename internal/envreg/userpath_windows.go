//go:build windows

package envreg

import (
	"fmt"
	"syscall"
	"unsafe"
)

// The registry is reached through syscall.NewLazyDLL("advapi32.dll") because
// go.mod has no dependencies (see internal/tray/tray_windows.go). Only the
// user's own HKCU\Environment key is ever opened.

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	pRegOpenKeyExW      = advapi32.NewProc("RegOpenKeyExW")
	pRegQueryValueExW   = advapi32.NewProc("RegQueryValueExW")
	pRegSetValueExW     = advapi32.NewProc("RegSetValueExW")
	pRegDeleteValueW    = advapi32.NewProc("RegDeleteValueW")
	pRegCloseKey        = advapi32.NewProc("RegCloseKey")
	pSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

const (
	hkeyCurrentUser = 0x80000001
	keyQueryValue   = 0x0001
	keySetValue     = 0x0002

	errorFileNotFound = 2
	errorMoreData     = 234

	hwndBroadcast    = 0xFFFF
	wmSettingChange  = 0x001A
	smtoAbortIfHung  = 0x0002
	broadcastTimeout = 2000 // ms per window
)

// hkcuEnv is HKCU\Environment, opened for each call.
type hkcuEnv struct{}

func (hkcuEnv) open(access uint32) (syscall.Handle, error) {
	sub, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return 0, err
	}
	var key syscall.Handle
	r, _, _ := pRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(sub)), 0, uintptr(access), uintptr(unsafe.Pointer(&key)))
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return key, nil
}

func closeKey(key syscall.Handle) { _, _, _ = pRegCloseKey.Call(uintptr(key)) }

func (e hkcuEnv) Get(name string) (string, uint32, bool, error) {
	key, err := e.open(keyQueryValue)
	if err != nil {
		return "", 0, false, err
	}
	defer closeKey(key)
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return "", 0, false, err
	}
	size := uint32(0)
	for range 5 { // the value may grow between the size query and the read
		var typ uint32
		r, _, _ := pRegQueryValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(n)), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
		if r == errorFileNotFound {
			return "", 0, false, nil
		}
		if r != 0 {
			return "", 0, false, syscall.Errno(r)
		}
		buf := make([]uint16, size/2+2)
		got := uint32(len(buf)-1) * 2
		r, _, _ = pRegQueryValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(n)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&got)))
		if r == errorMoreData {
			size = got
			continue
		}
		if r != 0 {
			return "", 0, false, syscall.Errno(r)
		}
		return syscall.UTF16ToString(buf[:got/2]), typ, true, nil
	}
	return "", 0, false, fmt.Errorf("%s keeps changing", name)
}

func (e hkcuEnv) Set(name, value string, typ uint32) error {
	key, err := e.open(keySetValue)
	if err != nil {
		return err
	}
	defer closeKey(key)
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	v, err := syscall.UTF16FromString(value) // includes the terminating NUL
	if err != nil {
		return err
	}
	r, _, _ := pRegSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(n)), 0, uintptr(typ), uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

func (e hkcuEnv) Delete(name string) error {
	key, err := e.open(keySetValue)
	if err != nil {
		return err
	}
	defer closeKey(key)
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	r, _, _ := pRegDeleteValueW.Call(uintptr(key), uintptr(unsafe.Pointer(n)))
	if r != 0 && r != errorFileNotFound {
		return syscall.Errno(r)
	}
	return nil
}

// broadcastEnvChange tells running programs (Explorer, so new terminals) that
// the environment changed. The timeout is short and a failure is ignored: the
// registry is already right and new logons pick it up anyway.
func broadcastEnvChange() {
	p, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	_, _, _ = pSendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(p)),
		smtoAbortIfHung, broadcastTimeout, uintptr(unsafe.Pointer(&result)))
}

// openRegistry is the user's own environment, HKCU\Environment.
func openRegistry() registry { return hkcuEnv{} }
