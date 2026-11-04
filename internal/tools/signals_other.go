//go:build !linux && !darwin && !windows

package tools

import (
	"os"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr { return nil }

// detachedSysProcAttr has no detachable session primitive on
// non-Linux stubs: background sessions keep the foreground behaviour
// so the binary still builds on darwin/windows (Section 3.4).
func detachedSysProcAttr() *syscall.SysProcAttr { return nil }

func sigINT() os.Signal  { return os.Interrupt }
func sigTERM() os.Signal { return os.Interrupt }
func sigKILL() os.Signal { return os.Kill }
