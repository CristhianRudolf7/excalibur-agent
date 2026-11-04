package platform

import (
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsPriorityProcess(t *testing.T) {
	cmd := processTestCommand(t.TempDir(), "leaf")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	defer cmd.Process.Kill()
	if err := (WindowsProcessPriority{}).LowerProcess(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	class, err := windows.GetPriorityClass(h)
	if err != nil || class != windows.BELOW_NORMAL_PRIORITY_CLASS {
		t.Fatalf("class %x, %v", class, err)
	}
	if err := (WindowsProcessPriority{}).LowerProcess(0); err == nil {
		t.Fatal("invalid pid accepted")
	}
}

func TestWindowsPriorityThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	get := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetThreadPriority")
	before, _, _ := get.Call(uintptr(windows.CurrentThread()))
	defer setThreadPriority.Call(uintptr(windows.CurrentThread()), before)
	if err := (WindowsProcessPriority{}).LowerCurrentThread(); err != nil {
		t.Fatal(err)
	}
	after, _, err := get.Call(uintptr(windows.CurrentThread()))
	if int32(after) != -1 {
		t.Fatalf("priority %d: %v", int32(after), err)
	}
}
