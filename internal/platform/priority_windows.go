package platform

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsProcessPriority lowers tool and background-thread priority
// (Sections 3.2, 5.2, 13.4). The zero value uses BELOW_NORMAL_PRIORITY_CLASS.
type WindowsProcessPriority struct{}

var _ ProcessPriority = WindowsProcessPriority{}

var setProcessInformation = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetProcessInformation")
var setThreadPriority = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadPriority")

// LowerProcess applies below-normal CPU scheduling and best-effort low memory
// priority and power throttling. Optional policies never fail the CPU step.
func (WindowsProcessPriority) LowerProcess(pid int) error {
	if pid <= 0 {
		return errors.New("platform: LowerProcess requires a positive pid")
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	// Never raise a process already in the idle class.
	class, err := windows.GetPriorityClass(h)
	if err != nil {
		return err
	}
	if class != windows.IDLE_PRIORITY_CLASS {
		if err := windows.SetPriorityClass(h, windows.BELOW_NORMAL_PRIORITY_CLASS); err != nil {
			return err
		}
	}
	if setProcessInformation.Find() == nil {
		memory := uint32(2) // MEMORY_PRIORITY_LOW; ProcessMemoryPriority = 0.
		_, _, _ = setProcessInformation.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&memory)), unsafe.Sizeof(memory))
		// PROCESS_POWER_THROTTLING_CURRENT_VERSION and EXECUTION_SPEED.
		power := struct{ Version, ControlMask, StateMask uint32 }{1, 1, 1}
		_, _, _ = setProcessInformation.Call(uintptr(h), 4, uintptr(unsafe.Pointer(&power)), unsafe.Sizeof(power))
	}
	return nil
}

// LowerCurrentThread lowers only the calling thread. The caller must hold
// runtime.LockOSThread, as CPU pool workers do, until its work is complete.
func (WindowsProcessPriority) LowerCurrentThread() error {
	// THREAD_PRIORITY_BELOW_NORMAL is the signed value -1.
	ok, _, err := setThreadPriority.Call(uintptr(windows.CurrentThread()), ^uintptr(0))
	if ok == 0 {
		return err
	}
	return nil
}
