package tools

import (
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP, HideWindow: true}
}

// Detached output is backed by files by the caller. Breakaway from the
// parent's job is requested when that job allows it. A job without
// JOB_OBJECT_LIMIT_BREAKAWAY_OK (CI runners, some IDE and service hosts)
// rejects CREATE_BREAKAWAY_FROM_JOB with ERROR_ACCESS_DENIED, so there the
// child starts detached inside the parent job and survives Excalibur's own
// exit for as long as that outer job lives.
func detachedSysProcAttr() *syscall.SysProcAttr {
	flags := uint32(windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP)
	if jobBreakawayAllowed() {
		flags |= windows.CREATE_BREAKAWAY_FROM_JOB
	}
	return &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
}

var (
	procIsProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
	breakawayOnce      sync.Once
	breakawayAllowed   bool
)

// jobBreakawayAllowed reports whether a child of this process may leave the
// job this process belongs to. A process outside any job may always do so.
func jobBreakawayAllowed() bool {
	breakawayOnce.Do(func() {
		var inJob int32
		if r, _, _ := procIsProcessInJob.Call(uintptr(windows.CurrentProcess()), 0, uintptr(unsafe.Pointer(&inJob))); r == 0 || inJob == 0 {
			breakawayAllowed = true
			return
		}
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		if err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
			return
		}
		breakawayAllowed = info.BasicLimitInformation.LimitFlags&(windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK|windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK) != 0
	})
	return breakawayAllowed
}

type windowsSignal string

func (s windowsSignal) String() string { return string(s) }
func (s windowsSignal) Signal()        {}
func sigINT() os.Signal                { return os.Interrupt }
func sigTERM() os.Signal               { return windowsSignal("terminate") }
func sigKILL() os.Signal               { return os.Kill }
