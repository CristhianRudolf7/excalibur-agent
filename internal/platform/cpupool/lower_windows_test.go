package cpupool

import (
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsLowerCurrentThread(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	get := kernel.NewProc("GetThreadPriority")
	before, _, _ := get.Call(uintptr(windows.CurrentThread()))
	defer kernel.NewProc("SetThreadPriority").Call(uintptr(windows.CurrentThread()), before)
	if err := lowerCurrentThread(); err != nil {
		t.Fatal(err)
	}
	got, _, _ := get.Call(uintptr(windows.CurrentThread()))
	if int32(got) != -1 {
		t.Fatalf("priority %d, want -1", int32(got))
	}
}
