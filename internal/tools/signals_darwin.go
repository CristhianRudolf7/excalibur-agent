//go:build darwin

package tools

import (
	"os"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// detachedSysProcAttr detaches a background service into its own
// session (W10 run-end services): Setsid creates a new session with
// no controlling terminal, so the child survives the agent's exit. Stdin comes from /dev/null and output
// goes to a log file under /tmp (wired by the caller).
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

func sigINT() os.Signal  { return syscall.SIGINT }
func sigTERM() os.Signal { return syscall.SIGTERM }
func sigKILL() os.Signal { return syscall.SIGKILL }
