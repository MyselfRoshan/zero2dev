//go:build windows

package tray

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// Raw Win32 (no cgo, no third-party module): a message-only window owns a
// Shell_NotifyIcon icon; its window procedure handles clicks and the menu.

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW         = user32.NewProc("RegisterClassExW")
	pCreateWindowExW          = user32.NewProc("CreateWindowExW")
	pDefWindowProcW           = user32.NewProc("DefWindowProcW")
	pDestroyWindow            = user32.NewProc("DestroyWindow")
	pGetMessageW              = user32.NewProc("GetMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessageW         = user32.NewProc("DispatchMessageW")
	pPostMessageW             = user32.NewProc("PostMessageW")
	pPostQuitMessage          = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	pAppendMenuW              = user32.NewProc("AppendMenuW")
	pSetMenuDefaultItem       = user32.NewProc("SetMenuDefaultItem")
	pTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	pDestroyMenu              = user32.NewProc("DestroyMenu")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	pCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	pDestroyIcon              = user32.NewProc("DestroyIcon")
	pGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	pShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmNull      = 0x0000
	wmDestroy   = 0x0002
	wmClose     = 0x0010
	wmCommand   = 0x0111
	wmLButtonUp = 0x0202
	wmLButtonDb = 0x0203
	wmRButtonUp = 0x0205
	wmApp       = 0x8000
	wmTray      = wmApp + 1
	wmModify    = wmApp + 2 // apply a pending tooltip or balloon on the tray thread

	ninBalloonUserClick = 0x0405

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10
	niifInfo   = 0x01

	mfString    = 0x0000
	mfSeparator = 0x0800

	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080
	tpmRightButton = 0x0002

	hwndMessage = ^uintptr(2) // (HWND)-3
	smCxSmIcon  = 49

	cmdOpen   = 1
	cmdFolder = 2
	cmdQuit   = 3
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

type point struct{ x, y int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type icon struct {
	menu       Menu
	hwnd       uintptr
	hIcon      uintptr
	taskbarMsg uint32
	mu         sync.Mutex
	tip        string
	info       [2]string // pending balloon title, text
	closed     bool
	done       chan struct{}
}

// The window procedure is a package-level callback; it finds the one icon here.
var (
	current   *icon
	wndProcCB uintptr
	classOnce sync.Once
	classErr  error
	className = syscall.StringToUTF16Ptr("Zero2DevTray")
)

func utf16Into(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = append(u[:len(dst)-1], 0)
	}
	copy(dst, u)
}

// Start shows the icon. Clicks call the Menu functions on the tray's own thread.
func Start(m Menu) (Icon, error) {
	if current != nil {
		return nil, fmt.Errorf("tray already started")
	}
	t := &icon{menu: m, tip: m.Tooltip, done: make(chan struct{})}
	ready := make(chan error, 1)
	go t.loop(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

func (t *icon) loop(ready chan<- error) {
	runtime.LockOSThread() // the window and its messages belong to this thread
	defer runtime.UnlockOSThread()
	defer close(t.done)

	classOnce.Do(func() {
		wndProcCB = syscall.NewCallback(wndProc)
		hinst, _, _ := pGetModuleHandleW.Call(0)
		wc := wndClassEx{lpfnWndProc: wndProcCB, hInstance: hinst, lpszClassName: className}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			classErr = fmt.Errorf("RegisterClassEx: %v", err)
		}
	})
	if classErr != nil {
		ready <- classErr
		return
	}
	hinst, _, _ := pGetModuleHandleW.Call(0)
	hwnd, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)),
		0, 0, 0, 0, 0, hwndMessage, 0, hinst, 0)
	if hwnd == 0 {
		ready <- fmt.Errorf("CreateWindowEx: %v", err)
		return
	}
	t.hwnd = hwnd
	current = t
	tb, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("TaskbarCreated"))))
	t.taskbarMsg = uint32(tb)
	t.hIcon = loadIcon()
	if !t.add() {
		pDestroyWindow.Call(hwnd)
		current = nil
		ready <- fmt.Errorf("Shell_NotifyIcon failed")
		return
	}
	ready <- nil

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func loadIcon() uintptr {
	want, _, _ := pGetSystemMetrics.Call(smCxSmIcon)
	if want == 0 {
		want = 16
	}
	data, _, err := iconEntry(iconFile, int(want))
	if err != nil {
		return 0
	}
	h, _, _ := pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, want, want, 0)
	return h
}

func (t *icon) data() *notifyIconData {
	d := &notifyIconData{hWnd: t.hwnd, uID: 1}
	d.cbSize = uint32(unsafe.Sizeof(*d))
	return d
}

func (t *icon) add() bool {
	d := t.data()
	d.uFlags = nifMessage | nifIcon | nifTip
	d.uCallbackMessage = wmTray
	d.hIcon = t.hIcon
	t.mu.Lock()
	utf16Into(d.szTip[:], t.tip)
	t.mu.Unlock()
	r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(d)))
	return r != 0
}

func (t *icon) remove() {
	d := t.data()
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(d)))
	if t.hIcon != 0 {
		pDestroyIcon.Call(t.hIcon)
		t.hIcon = 0
	}
}

func (t *icon) applyPending() {
	t.mu.Lock()
	tip, info := t.tip, t.info
	t.info = [2]string{}
	t.mu.Unlock()
	d := t.data()
	d.uFlags = nifTip
	utf16Into(d.szTip[:], tip)
	if info[1] != "" {
		d.uFlags |= nifInfo
		utf16Into(d.szInfoTitle[:], info[0])
		utf16Into(d.szInfo[:], info[1])
		d.dwInfoFlags = niifInfo
	}
	pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(d)))
}

func (t *icon) SetTooltip(text string) {
	t.mu.Lock()
	changed := t.tip != text && !t.closed
	t.tip = text
	t.mu.Unlock()
	if changed {
		pPostMessageW.Call(t.hwnd, wmModify, 0, 0)
	}
}

func (t *icon) Notify(title, text string) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.info = [2]string{title, text}
	t.mu.Unlock()
	pPostMessageW.Call(t.hwnd, wmModify, 0, 0)
}

func (t *icon) Close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.mu.Unlock()
	pPostMessageW.Call(t.hwnd, wmClose, 0, 0)
	<-t.done
}

func (t *icon) showMenu() {
	m, _, _ := pCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer pDestroyMenu.Call(m)
	add := func(id uintptr, text string) {
		pAppendMenuW.Call(m, mfString, id, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))))
	}
	add(cmdOpen, "Open dashboard")
	if t.menu.OnFolder != nil {
		ft := t.menu.FolderText
		if ft == "" {
			ft = "Open data folder"
		}
		add(cmdFolder, ft)
	}
	pAppendMenuW.Call(m, mfSeparator, 0, 0)
	add(cmdQuit, "Quit Zero2Dev")
	pSetMenuDefaultItem.Call(m, cmdOpen, 0)
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	pSetForegroundWindow.Call(t.hwnd) // so the menu closes when clicking elsewhere
	cmd, _, _ := pTrackPopupMenu.Call(m, tpmReturnCmd|tpmNoNotify|tpmRightButton, uintptr(p.x), uintptr(p.y), 0, t.hwnd, 0)
	pPostMessageW.Call(t.hwnd, wmNull, 0, 0)
	t.run(int(cmd))
}

func (t *icon) run(cmd int) {
	call := func(f func()) {
		if f != nil {
			go f() // never block the tray thread
		}
	}
	switch cmd {
	case cmdOpen:
		call(t.menu.OnOpen)
	case cmdFolder:
		call(t.menu.OnFolder)
	case cmdQuit:
		call(t.menu.OnQuit)
	}
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	t := current
	if t == nil || hwnd != t.hwnd {
		r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return r
	}
	switch message {
	case wmTray:
		switch uint32(lParam) {
		case wmLButtonUp, wmLButtonDb, ninBalloonUserClick:
			t.run(cmdOpen)
		case wmRButtonUp:
			t.showMenu()
		}
		return 0
	case wmModify:
		t.applyPending()
		return 0
	case wmClose:
		t.remove()
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		current = nil
		pPostQuitMessage.Call(0)
		return 0
	}
	if t.taskbarMsg != 0 && message == t.taskbarMsg { // Explorer restarted: show the icon again
		t.add()
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}
