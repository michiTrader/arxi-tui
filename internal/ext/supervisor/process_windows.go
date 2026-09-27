//go:build windows

package supervisor

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

// The Windows half of the ported procgroup mechanics (ADR-0001). Windows has no
// process groups in the POSIX sense, so "kill the whole tree" is expressed with
// a Job Object: the child is assigned to a job created with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so closing the job handle terminates the
// child and every process it spawned. This is what makes an unmount actually
// stop a plugin that started helpers of its own, the same guarantee Setpgid + a
// negative-pid signal gives on unix — an orphaned grandchild after unmount is
// the leak both halves exist to prevent.
//
// CREATE_NEW_PROCESS_GROUP additionally lets terminateProcess deliver a
// CTRL_BREAK to the child alone without hitting the host's own console group,
// which is the graceful step before the job-close kill.

const (
	createNewProcessGroup             = 0x00000200
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x00002000
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	createJobObject          = kernel32.NewProc("CreateJobObjectW")
	setInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	assignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	generateConsoleCtrlEvent = kernel32.NewProc("GenerateConsoleCtrlEvent")
	closeHandle              = kernel32.NewProc("CloseHandle")

	// jobs maps a child pid to the job handle that will kill it on close. It is
	// keyed by pid rather than held on the child struct so the platform layer
	// stays a drop-in port with no shared type across the build tag: the unix
	// half needs no such table, and threading a handle field only Windows uses
	// through the portable code would leak the platform into it.
	jobMu sync.Mutex
	jobs  = map[int]syscall.Handle{}
)

type ioCounters struct {
	ReadOperationCount, WriteOperationCount, OtherOperationCount uint64
	ReadTransferCount, WriteTransferCount, OtherTransferCount    uint64
}

type basicLimitInformation struct {
	PerProcessUserTimeLimit, PerJobUserTimeLimit int64
	LimitFlags                                   uint32
	MinimumWorkingSetSize, MaximumWorkingSetSize uintptr
	ActiveProcessLimit                           uint32
	Affinity                                     uintptr
	PriorityClass, SchedulingClass               uint32
}

type extendedLimitInformation struct {
	BasicLimitInformation                                                        basicLimitInformation
	IoInfo                                                                       ioCounters
	ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
}

func prepareProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
	return nil
}

// attachProcess creates the kill-on-close job and assigns the started child to
// it. It runs after Start (the pid must exist to be assigned) and before the
// handshake, so a plugin that never completes the handshake is still inside the
// job the host can close.
func attachProcess(cmd *exec.Cmd) error {
	h, _, e := createJobObject.Call(0, 0)
	if h == 0 {
		return e
	}
	info := extendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	r, _, e := setInformationJobObject.Call(h, jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r == 0 {
		closeHandle.Call(h)
		return e
	}
	// PROCESS_TERMINATE | PROCESS_SET_QUOTA | PROCESS_QUERY_INFORMATION.
	process, err := syscall.OpenProcess(0x0001|0x0100|0x0400, false, uint32(cmd.Process.Pid))
	if err != nil {
		closeHandle.Call(h)
		return err
	}
	defer syscall.CloseHandle(process)
	r, _, e = assignProcessToJobObject.Call(h, uintptr(process))
	if r == 0 {
		closeHandle.Call(h)
		return e
	}
	jobMu.Lock()
	jobs[cmd.Process.Pid] = syscall.Handle(h)
	jobMu.Unlock()
	return nil
}

// releaseProcess closes and forgets the job handle for a child that has already
// exited on its own, so a normal plugin exit does not leak a handle. It does
// NOT kill: closing the handle of an already-dead process is a no-op, and the
// live-kill path is killProcessTree.
func releaseProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	jobMu.Lock()
	h := jobs[cmd.Process.Pid]
	delete(jobs, cmd.Process.Pid)
	jobMu.Unlock()
	if h != 0 {
		closeHandle.Call(uintptr(h))
	}
}

// terminateProcess is the graceful step: a CTRL_BREAK to the child's own
// process group. A plugin may handle it to flush; one that does not is killed
// by killProcessTree after the deadline.
func terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	r, _, e := generateConsoleCtrlEvent.Call(syscall.CTRL_BREAK_EVENT, uintptr(cmd.Process.Pid))
	if r == 0 {
		return e
	}
	return nil
}

// killProcessTree closes the job handle, which terminates the child and every
// process it spawned (KILL_ON_JOB_CLOSE). If the job is somehow gone it falls
// back to killing the single process, so kill never silently no-ops on a live
// child.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	jobMu.Lock()
	h := jobs[cmd.Process.Pid]
	delete(jobs, cmd.Process.Pid)
	jobMu.Unlock()
	if h != 0 {
		r, _, e := closeHandle.Call(uintptr(h))
		if r == 0 {
			return e
		}
		return nil
	}
	return cmd.Process.Kill()
}
