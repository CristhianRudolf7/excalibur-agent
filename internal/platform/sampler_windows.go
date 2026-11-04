package platform

import (
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WindowsResourceSampler reads working-set RAM, process CPU time and the
// process-tree size (Sections 5.2, 22.2). Windows has no OOM-kill counter.
type WindowsResourceSampler struct{}

var _ ResourceSampler = WindowsResourceSampler{}
var getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

// processMemoryCounters matches PROCESS_MEMORY_COUNTERS on both word sizes.
type processMemoryCounters struct {
	Size, PageFaultCount                               uint32
	PeakWorkingSetSize, WorkingSetSize                 uintptr
	QuotaPeakPagedPoolUsage, QuotaPagedPoolUsage       uintptr
	QuotaPeakNonPagedPoolUsage, QuotaNonPagedPoolUsage uintptr
	PagefileUsage, PeakPagefileUsage                   uintptr
}

// Sample treats pid <= 0 as the calling process, like the Linux backend.
// RSS and CPU describe pid; Processes includes pid and its descendants.
func (WindowsResourceSampler) Sample(pid int) (ResourceSample, error) {
	if pid <= 0 {
		pid = os.Getpid()
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return ResourceSample{}, err
	}
	defer windows.CloseHandle(h)
	counters := processMemoryCounters{}
	counters.Size = uint32(unsafe.Sizeof(counters))
	ok, _, err := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	if ok == 0 {
		return ResourceSample{}, err
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return ResourceSample{}, err
	}
	entries, err := processSnapshot()
	if err != nil {
		return ResourceSample{}, err
	}
	ticks := func(ft windows.Filetime) uint64 { return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime) }
	return ResourceSample{At: time.Now(), RSSBytes: uint64(counters.WorkingSetSize), CPUSeconds: float64(ticks(kernel)+ticks(user)) / 1e7, Processes: len(processTreePostorder(uint32(pid), entries))}, nil
}
