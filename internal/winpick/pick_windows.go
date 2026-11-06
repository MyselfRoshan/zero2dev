//go:build windows

package winpick

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

const supported = true

// The modern Vista+ dialog (IFileOpenDialog with FOS_PICKFOLDERS), driven
// through raw COM vtable calls so no cgo or third-party module is needed.

var (
	ole32    = syscall.NewLazyDLL("ole32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	procSHCreateItem     = shell32.NewProc("SHCreateItemFromParsingName")

	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
)

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

var (
	clsidFileOpenDialog = guid{0xDC1C5A9C, 0xE88A, 0x4DDE, [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = guid{0xD57C7288, 0xD4AD, 0x4768, [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidIShellItem       = guid{0x43826D1E, 0xE718, 0x42EE, [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
	clsctxInprocServer      = 0x1
	rpcEChangedMode         = 0x80010106
	errCancelled            = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)

	fosPickFolders     = 0x20
	fosForceFileSystem = 0x40
	fosPathMustExist   = 0x800
	sigdnFileSysPath   = 0x80058000

	wsPopup        = 0x80000000
	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080
	smCxScreen     = 0
	smCyScreen     = 1
)

// vtable slots (IUnknown 0-2, IModalWindow::Show 3, then IFileDialog).
const (
	vRelease        = 2
	vShow           = 3
	vSetOptions     = 9
	vGetOptions     = 10
	vSetFolder      = 12
	vSetTitle       = 17
	vSetOkLabel     = 18
	vGetResult      = 20
	vGetDisplayName = 5 // IShellItem
)

// method returns the address of vtable slot i on obj.
func method(obj unsafe.Pointer, i int) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	return *(*uintptr)(unsafe.Add(vtbl, uintptr(i)*unsafe.Sizeof(uintptr(0))))
}

// Pin pointer arguments and convert them at SyscallN so they stay valid
// and visible to Go for the duration of the native call.
func call(obj unsafe.Pointer, i int) uint32 {
	var pinned runtime.Pinner
	pinned.Pin(obj)
	defer pinned.Unpin()
	r, _, _ := syscall.SyscallN(method(obj, i), uintptr(obj))
	return uint32(r)
}

func callPtr(obj unsafe.Pointer, i int, arg unsafe.Pointer) uint32 {
	var pinned runtime.Pinner
	pinned.Pin(obj)
	pinned.Pin(arg)
	defer pinned.Unpin()
	r, _, _ := syscall.SyscallN(method(obj, i), uintptr(obj), uintptr(arg))
	return uint32(r)
}

func callValue(obj unsafe.Pointer, i int, arg uintptr) uint32 {
	var pinned runtime.Pinner
	pinned.Pin(obj)
	defer pinned.Unpin()
	r, _, _ := syscall.SyscallN(method(obj, i), uintptr(obj), arg)
	return uint32(r)
}

func callValuePtr(obj unsafe.Pointer, i int, arg uintptr, ptr unsafe.Pointer) uint32 {
	var pinned runtime.Pinner
	pinned.Pin(obj)
	pinned.Pin(ptr)
	defer pinned.Unpin()
	r, _, _ := syscall.SyscallN(method(obj, i), uintptr(obj), arg, uintptr(ptr))
	return uint32(r)
}

func failed(hr uint32) bool { return int32(hr) < 0 }

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil { // NUL inside the string
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}

func pickFolder(title, start string) (string, bool, error) {
	// COM single-threaded apartment and the dialog's window must stay on one OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	if failed(uint32(hr)) && uint32(hr) != rpcEChangedMode {
		return "", false, fmt.Errorf("CoInitializeEx: 0x%08X", uint32(hr))
	}
	if !failed(uint32(hr)) {
		defer procCoUninitialize.Call()
	}

	var dlg unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if failed(uint32(hr)) {
		return "", false, fmt.Errorf("cannot create the folder dialog: 0x%08X", uint32(hr))
	}
	defer call(dlg, vRelease)

	var opts uint32
	callPtr(dlg, vGetOptions, unsafe.Pointer(&opts))
	callValue(dlg, vSetOptions, uintptr(opts|fosPickFolders|fosForceFileSystem|fosPathMustExist))
	if title != "" {
		callPtr(dlg, vSetTitle, unsafe.Pointer(utf16(title)))
	}
	callPtr(dlg, vSetOkLabel, unsafe.Pointer(utf16("Select folder")))
	if dir := existingDir(start); dir != "" {
		var item unsafe.Pointer
		hr, _, _ = procSHCreateItem.Call(uintptr(unsafe.Pointer(utf16(dir))), 0,
			uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item)))
		if !failed(uint32(hr)) && item != nil {
			callPtr(dlg, vSetFolder, item)
			call(item, vRelease)
		}
	}

	owner := topmostOwner()
	if owner != 0 {
		defer procDestroyWindow.Call(owner)
	}
	hrShow := callValue(dlg, vShow, owner)
	if hrShow == errCancelled {
		return "", false, nil
	}
	if failed(hrShow) {
		return "", false, fmt.Errorf("folder dialog failed: 0x%08X", hrShow)
	}

	var item unsafe.Pointer
	if hr := callPtr(dlg, vGetResult, unsafe.Pointer(&item)); failed(hr) || item == nil {
		return "", false, fmt.Errorf("folder dialog returned no result: 0x%08X", hr)
	}
	defer call(item, vRelease)
	var name *uint16
	if hr := callValuePtr(item, vGetDisplayName, sigdnFileSysPath, unsafe.Pointer(&name)); failed(hr) || name == nil {
		return "", false, fmt.Errorf("the chosen folder has no file system path: 0x%08X", hr)
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
	return syscall.UTF16ToString(unsafe.Slice(name, 32768)), true, nil
}

// topmostOwner creates an invisible, always-on-top window in the middle of
// the screen to own the dialog. The daemon has no window of its own, and a
// dialog without an owner would often open behind the browser; owned by a
// topmost window it opens on top, centred. It also borrows the foreground
// from the browser (the user just clicked there) so the dialog gets the
// keyboard focus.
func topmostOwner() uintptr {
	cx, _, _ := procGetSystemMetrics.Call(smCxScreen)
	cy, _, _ := procGetSystemMetrics.Call(smCyScreen)
	h, _, _ := procCreateWindowExW.Call(wsExTopmost|wsExToolWindow,
		uintptr(unsafe.Pointer(utf16("STATIC"))), uintptr(unsafe.Pointer(utf16("Zero2Dev"))),
		wsPopup, cx/2, cy/2, 0, 0, 0, 0, 0, 0)
	if h == 0 {
		return 0
	}
	if fg, _, _ := procGetForegroundWindow.Call(); fg != 0 {
		fgThread, _, _ := procGetWindowThreadProcessId.Call(fg, 0)
		me, _, _ := procGetCurrentThreadId.Call()
		if fgThread != 0 && fgThread != me {
			procAttachThreadInput.Call(me, fgThread, 1)
			procSetForegroundWindow.Call(h)
			procAttachThreadInput.Call(me, fgThread, 0)
		} else {
			procSetForegroundWindow.Call(h)
		}
	}
	return h
}

// existingDir returns start if it is an existing folder, else its nearest
// existing parent, else "".
func existingDir(start string) string {
	if start == "" || !filepath.IsAbs(start) {
		return ""
	}
	p := filepath.Clean(start)
	for {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
}
