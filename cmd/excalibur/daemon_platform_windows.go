package main

import (
	"unsafe"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"golang.org/x/sys/windows"
)

// daemonSampler supplies native working-set and CPU counters to the watchdog.
func daemonSampler() platform.ResourceSampler { return platform.WindowsResourceSampler{} }

// physicalRAMBytes queries total physical RAM for the default watchdog budget.
// A failed query leaves the threshold under explicit operator control.
func physicalRAMBytes() uint64 {
	status := struct {
		Length, MemoryLoad                                 uint32
		TotalPhys, AvailPhys, TotalPageFile, AvailPageFile uint64
		TotalVirtual, AvailVirtual, AvailExtendedVirtual   uint64
	}{}
	status.Length = uint32(unsafe.Sizeof(status))
	ok, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&status)))
	if ok == 0 {
		return 0
	}
	return status.TotalPhys
}

// daemonKeyring reports the platform keyring. Keychain and DPAPI are
// future adapters behind the same interface (Section 20.1); until then
// the vault is passphrase-only here.
func daemonKeyring() platform.Keyring { return platform.WindowsKeyring{} }
