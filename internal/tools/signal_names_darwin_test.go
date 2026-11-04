package tools

import (
	"syscall"
	"testing"
)

func TestDarwinSignalNamesMatchKernelNumbers(t *testing.T) {
	for sig, want := range map[syscall.Signal]string{
		syscall.SIGBUS: "SIGBUS", syscall.SIGSYS: "SIGSYS", syscall.SIGEMT: "SIGEMT",
		syscall.SIGUSR1: "SIGUSR1", syscall.SIGUSR2: "SIGUSR2", syscall.SIGKILL: "SIGKILL",
		syscall.SIGTERM: "SIGTERM", syscall.SIGSEGV: "SIGSEGV", syscall.SIGPIPE: "SIGPIPE",
	} {
		if got := signalNames[int(sig)]; got != want {
			t.Errorf("signal %d: got %q, want %q", int(sig), got, want)
		}
	}
	if got := shellExitSignal(128 + int(syscall.SIGBUS)); got != "SIGBUS" {
		t.Errorf("shellExitSignal(SIGBUS) = %q", got)
	}
}
