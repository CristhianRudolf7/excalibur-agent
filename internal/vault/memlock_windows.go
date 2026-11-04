package vault

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// lockMemory pins the DEK pages with VirtualLock when the working-set
// quota allows it. Failure is best-effort and never fatal (Section 20.1).
func lockMemory(b []byte) {
	if len(b) == 0 {
		return
	}
	_ = windows.VirtualLock(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
}

// unlockMemory releases a previous VirtualLock.
func unlockMemory(b []byte) {
	if len(b) == 0 {
		return
	}
	_ = windows.VirtualUnlock(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
}
