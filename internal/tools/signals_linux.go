//go:build linux

package tools

import (
	"os"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

func sigINT() os.Signal  { return syscall.SIGINT }
func sigTERM() os.Signal { return syscall.SIGTERM }
func sigKILL() os.Signal { return syscall.SIGKILL }
