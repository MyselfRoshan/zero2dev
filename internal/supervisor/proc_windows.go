//go:build windows

package supervisor

import (
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	createNoWindow                  = 0x08000000
	jobObjectExtendedLimitInfoClass = 9
	jobObjectLimitKillOnJobClose    = 0x00002000
	processSetQuota                 = 0x0100
	processTerminate                = 0x0001
	processQueryLimitedInformation  = 0x1000
	jobBasicAccountingClass         = 1
	jobBasicProcessIDListClass      = 3
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject     = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJob   = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJob  = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject  = kernel32.NewProc("TerminateJobObject")
	procQueryInformationJob = kernel32.NewProc("QueryInformationJobObject")
	procGetProcessMemory    = kernel32.NewProc("K32GetProcessMemoryInfo")
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

// JOBOBJECT_BASIC_ACCOUNTING_INFORMATION (class 1): times are in 100 ns units.
type jobAccounting struct {
	TotalUserTime, TotalKernelTime                 int64
	ThisPeriodTotalUserTime, ThisPeriodTotalKernel int64
	TotalPageFaultCount, TotalProcesses            uint32
	ActiveProcesses, TotalTerminatedProcesses      uint32
}

// JOBOBJECT_BASIC_PROCESS_ID_LIST (class 3), with room for 512 processes (a service tree is far smaller).
type jobPIDList struct {
	Assigned, InList uint32
	IDs              [512]uintptr
}

// PROCESS_MEMORY_COUNTERS (K32GetProcessMemoryInfo).
type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// usage reads the job's accounting: the CPU time (user plus kernel) of every process that ever ran
// in it, exited ones included (so it only grows), and the working set summed over the processes in
// it now. ok is false when there is no job (the taskkill fallback) or the tree is closed.
func (t *tree) usage() (cpuTime time.Duration, memBytes uint64, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.job == 0 {
		return 0, 0, false
	}
	var acc jobAccounting
	if r, _, _ := procQueryInformationJob.Call(uintptr(t.job), jobBasicAccountingClass,
		uintptr(unsafe.Pointer(&acc)), unsafe.Sizeof(acc), 0); r == 0 {
		return 0, 0, false
	}
	cpuTime = time.Duration(acc.TotalUserTime+acc.TotalKernelTime) * 100 // 100 ns units
	var list jobPIDList
	// ERROR_MORE_DATA (more than 512 processes) still fills the buffer: use what is there.
	procQueryInformationJob.Call(uintptr(t.job), jobBasicProcessIDListClass,
		uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), 0)
	n := min(int(list.InList), len(list.IDs))
	for _, pid := range list.IDs[:n] {
		h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
		if err != nil {
			continue // exited in the meantime
		}
		var pmc processMemoryCounters
		pmc.Cb = uint32(unsafe.Sizeof(pmc))
		if r, _, _ := procGetProcessMemory.Call(uintptr(h), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.Cb)); r != 0 {
			memBytes += uint64(pmc.WorkingSetSize)
		}
		syscall.CloseHandle(h)
	}
	return cpuTime, memBytes, true
}
