//go:build windows

package main

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	pSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
	pAttachConsole         = kernel32.NewProc("AttachConsole")
	pGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	pGetFileType           = kernel32.NewProc("GetFileType")
	pMessageBoxW           = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
)

var attachedConsole bool

const attachParentProcess = ^uintptr(0) // (DWORD)-1

// setupOutput decides where messages go. A console program (go run, tests)
// already has a console. A GUI-subsystem build started from a terminal
// attaches to it; started by double-click it has none, and logs to a file.
func setupOutput() {
	if w, _, _ := pGetConsoleWindow.Call(); w != 0 {
		return // console subsystem: stdout works
	}
	// Output redirected to a file or pipe (zero2dev.exe --version | more, a service
	// manager): the inherited handle is valid even for a GUI program.
	if t, _, _ := pGetFileType.Call(os.Stdout.Fd()); t != 0 { // FILE_TYPE_UNKNOWN = 0
		return
	}
	if r, _, _ := pAttachConsole.Call(attachParentProcess); r != 0 {
		attachedConsole = true
		pSetConsoleCtrlHandler.Call(0, 0) // Undo inherited Ctrl+C ignore mode after attaching.
		if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stdout, os.Stderr = f, f
			outMu.Lock()
			out = f
			outMu.Unlock()
			fmtNewline()
			return
		}
	}
	interactive = false
}

// fmtNewline moves past the prompt the terminal printed when the GUI program
// returned control to it immediately.
func fmtNewline() { os.Stdout.WriteString("\r\n") }

// alert shows a Windows message box (used when there is no terminal).
func alert(title, text string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	const mbIconError, mbSetForeground = 0x10, 0x10000
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconError|mbSetForeground)
}

// AttachConsole resets the handler table, including Go's runtime handler.
func consoleQuit(quit func(), done <-chan struct{}) func() {
	if !attachedConsole {
		return func() {}
	}
	callback := syscall.NewCallback(func(event uint32) uintptr {
		switch event {
		case 0, 1, 2, 5, 6: // Ctrl+C, Break, Close, Logoff, Shutdown
			quit()
			if event != 0 && event != 1 {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
				}
			}
			return 1
		}
		return 0
	})
	pSetConsoleCtrlHandler.Call(callback, 1)
	return func() { pSetConsoleCtrlHandler.Call(callback, 0) }
}
