//go:build darwin

package tools

import (
	"syscall"
	"testing"
)

func TestDarwinSignalsAndProcessAttributes(t *testing.T) {
	if sigINT() != syscall.SIGINT || sigTERM() != syscall.SIGTERM || sigKILL() != syscall.SIGKILL {
		t.Fatal("incorrect Unix signal mapping")
	}
	if a := sysProcAttr(); !a.Setpgid || a.Setsid {
		t.Fatalf("foreground attributes: %+v", a)
	}
	if a := detachedSysProcAttr(); !a.Setsid || a.Setpgid {
		t.Fatalf("detached attributes: %+v", a)
	}
}
