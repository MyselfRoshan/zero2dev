//go:build windows

package winpick

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// The Open and Save as dialogs (IFileOpenDialog / IFileSaveDialog), driven
// the same way as pickFolder.

var (
	clsidFileSaveDialog = guid{0xC0B4E2F3, 0xBA21, 0x4773, [8]byte{0x8D, 0xBA, 0x33, 0x5E, 0xC9, 0x46, 0xEB, 0x8B}}
	iidIFileSaveDialog  = guid{0x84BCCD23, 0x5FDE, 0x4CDB, [8]byte{0xAE, 0xA4, 0xAF, 0x64, 0xB8, 0x3D, 0x78, 0xAB}}
)

const (
	fosOverwritePrompt  = 0x2
	fosNoChangeDir      = 0x8
	fosFileMustExist    = 0x1000
	fosNoReadOnlyReturn = 0x8000
)

// IFileDialog vtable slots used here besides those of pick_windows.go.
const (
	vSetFileTypes        = 4
	vSetFileTypeIndex    = 5
	vSetFileName         = 15
	vSetDefaultExtension = 22
)

// filterSpec is COMDLG_FILTERSPEC.
type filterSpec struct {
	name, spec *uint16
}

// splitStart turns the start path into the folder the dialog opens in and the
// file name it suggests: a folder opens there; a file (existing or not) opens
// in its folder (or the nearest existing parent) with its name filled in.
func splitStart(start string) (dir, name string) {
	start = strings.TrimSpace(start)
	if start == "" || !filepath.IsAbs(start) {
		return "", ""
	}
	start = filepath.Clean(start)
	if st, err := os.Stat(start); err == nil && st.IsDir() {
		return start, ""
	}
	return existingDir(filepath.Dir(start)), filepath.Base(start)
}

// defaultExt is the extension (without the dot) of the first filter's first
// pattern, e.g. "sql" for "*.sql;*.sql.gz"; "" for "*.*" or "*".
func defaultExt(filters []Filter) string {
	if len(filters) == 0 {
		return ""
	}
	first, _, _ := strings.Cut(filters[0].Pattern, ";")
	first = strings.TrimSpace(first)
	ext := strings.TrimPrefix(first, "*.")
	if ext == first || ext == "" || strings.ContainsAny(ext, "*?.") {
		return ""
	}
	return ext
}

func pickFile(o FileOptions) (string, bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	if failed(uint32(hr)) && uint32(hr) != rpcEChangedMode {
		return "", false, fmt.Errorf("CoInitializeEx: 0x%08X", uint32(hr))
	}
	if !failed(uint32(hr)) {
		defer procCoUninitialize.Call()
	}

	clsid, iid := &clsidFileOpenDialog, &iidIFileOpenDialog
	if o.Save {
		clsid, iid = &clsidFileSaveDialog, &iidIFileSaveDialog
	}
	var dlg unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&dlg)))
	if failed(uint32(hr)) {
		return "", false, fmt.Errorf("cannot create the file dialog: 0x%08X", uint32(hr))
	}
	defer syscall.SyscallN(method(dlg, vRelease), uintptr(dlg))

	var opts uint32
	syscall.SyscallN(method(dlg, vGetOptions), uintptr(dlg), uintptr(unsafe.Pointer(&opts)))
	opts |= fosForceFileSystem | fosPathMustExist | fosNoChangeDir
	if o.Save {
		opts |= fosOverwritePrompt | fosNoReadOnlyReturn
	} else {
		opts |= fosFileMustExist
	}
	syscall.SyscallN(method(dlg, vSetOptions), uintptr(dlg), uintptr(opts))
	if o.Title != "" {
		syscall.SyscallN(method(dlg, vSetTitle), uintptr(dlg), uintptr(unsafe.Pointer(utf16(o.Title))))
	}

	// The filter strings must stay alive until the dialog is closed.
	specs := make([]filterSpec, 0, len(o.Filters)+1)
	for _, f := range o.Filters {
		specs = append(specs, filterSpec{utf16(f.Name + " (" + f.Pattern + ")"), utf16(f.Pattern)})
	}
	if len(specs) > 0 {
		specs = append(specs, filterSpec{utf16("All files (*.*)"), utf16("*.*")})
		syscall.SyscallN(method(dlg, vSetFileTypes), uintptr(dlg), uintptr(len(specs)), uintptr(unsafe.Pointer(&specs[0])))
		syscall.SyscallN(method(dlg, vSetFileTypeIndex), uintptr(dlg), 1) // 1-based
	}
	if ext := defaultExt(o.Filters); o.Save && ext != "" {
		syscall.SyscallN(method(dlg, vSetDefaultExtension), uintptr(dlg), uintptr(unsafe.Pointer(utf16(ext))))
	}

	dir, name := splitStart(o.Start)
	if o.DefaultName != "" {
		name = o.DefaultName
	}
	if name != "" {
		syscall.SyscallN(method(dlg, vSetFileName), uintptr(dlg), uintptr(unsafe.Pointer(utf16(name))))
	}
	if dir != "" {
		var item unsafe.Pointer
		hr, _, _ = procSHCreateItem.Call(uintptr(unsafe.Pointer(utf16(dir))), 0,
			uintptr(unsafe.Pointer(&iidIShellItem)), uintptr(unsafe.Pointer(&item)))
		if !failed(uint32(hr)) && item != nil {
			syscall.SyscallN(method(dlg, vSetFolder), uintptr(dlg), uintptr(item))
			syscall.SyscallN(method(item, vRelease), uintptr(item))
		}
	}

	owner := topmostOwner()
	if owner != 0 {
		defer procDestroyWindow.Call(owner)
	}
	hrShow := hres(syscall.SyscallN(method(dlg, vShow), uintptr(dlg), owner))
	runtime.KeepAlive(specs)
	if hrShow == errCancelled {
		return "", false, nil
	}
	if failed(hrShow) {
		return "", false, fmt.Errorf("file dialog failed: 0x%08X", hrShow)
	}

	var item unsafe.Pointer
	if hr := hres(syscall.SyscallN(method(dlg, vGetResult), uintptr(dlg), uintptr(unsafe.Pointer(&item)))); failed(hr) || item == nil {
		return "", false, fmt.Errorf("file dialog returned no result: 0x%08X", hr)
	}
	defer syscall.SyscallN(method(item, vRelease), uintptr(item))
	var res *uint16
	if hr := hres(syscall.SyscallN(method(item, vGetDisplayName), uintptr(item), sigdnFileSysPath, uintptr(unsafe.Pointer(&res)))); failed(hr) || res == nil {
		return "", false, fmt.Errorf("the chosen file has no file system path: 0x%08X", hr)
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(res)))
	return syscall.UTF16ToString(unsafe.Slice(res, 32768)), true, nil
}
