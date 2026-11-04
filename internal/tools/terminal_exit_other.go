//go:build !linux && !darwin && !windows

package tools

import (
	"os/exec"
	"syscall"
)

// exitSignalOf returns the terminating signal of a finished process.
func exitSignalOf(ee *exec.ExitError) (int, bool) {
	ws, ok := ee.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		return 0, false
	}
	return int(ws.Signal()), true
}

func shellExitSignal(code int) string {
	if code > 128 {
		return signalNames[code-128]
	}
	return ""
}
