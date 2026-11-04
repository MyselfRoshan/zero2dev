//go:build windows

package supervisor

import (
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"unsafe"
)

const (
	createNoWindow                  = 0x08000000
	jobObjectExtendedLimitInfoClass = 9
	jobObjectLimitKillOnJobClose    = 0x00002000
	processSetQuota                 = 0x0100
	processTerminate                = 0x0001
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject    = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJob  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJob = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject = kernel32.NewProc("TerminateJobObject")
)

// Layouts follow the Win32 JOBOBJECT_EXTENDED_LIMIT_INFORMATION structs.
type ioCounters struct{ a, b, c, d, e, f uint64 }
type basicLimitInfo struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}
type extendedLimitInfo struct {
	Basic                 basicLimitInfo
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// configure hides the console window of the child.
func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
}

// tree wraps a Job Object created with KILL_ON_JOB_CLOSE: when the daemon
// dies (even if killed), Windows closes the handle and kills every process
// in the job, so children never outlive zero2dev.
type tree struct {
	mu     sync.Mutex
	job    syscall.Handle
	pid    int
	closed bool
}

// attach creates a job for the freshly started process and assigns it. There
// is a tiny window before assignment in which the child could spawn
// grandchildren outside the job; taskkill /T in kill() covers that case.
func attach(cmd *exec.Cmd) (*tree, error) {
	t := &tree{pid: cmd.Process.Pid}
	h, _, err := procCreateJobObject.Call(0, 0)
	if h == 0 {
		return t, err
	}
	t.job = syscall.Handle(h)
	var info extendedLimitInfo
	info.Basic.LimitFlags = jobObjectLimitKillOnJobClose
	r, _, err := procSetInformationJob.Call(h, jobObjectExtendedLimitInfoClass, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r == 0 {
		syscall.CloseHandle(t.job)
		t.job = 0
		return t, err
	}
	ph, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(cmd.Process.Pid))
	if err != nil {
		syscall.CloseHandle(t.job)
		t.job = 0
		return t, err
	}
	defer syscall.CloseHandle(ph)
	if r, _, err := procAssignProcessToJob.Call(h, uintptr(ph)); r == 0 {
		syscall.CloseHandle(t.job)
		t.job = 0
		return t, err
	}
	return t, nil
}

// terminate: Windows has no portable graceful signal for arbitrary console
// programs, so this ends the whole job immediately.
func (t *tree) terminate() { t.kill() }

func (t *tree) kill() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	if t.job != 0 {
		procTerminateJobObject.Call(uintptr(t.job), 1)
	}
	// Fallback for processes that escaped the job (or if job setup failed).
	c := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(t.pid))
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
	_ = c.Run()
}

func (t *tree) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	if t.job != 0 {
		procTerminateJobObject.Call(uintptr(t.job), 1) // sweep leftovers
		syscall.CloseHandle(t.job)
		t.job = 0
	}
}
