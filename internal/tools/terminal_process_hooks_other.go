//go:build !linux && !darwin && !windows

package tools

import (
	"os"
	"os/exec"
)

func attachSessionProcess(proc *os.Process) (func(), error)              { return func() {}, nil }
func signalSessionProcess(proc *os.Process, sig os.Signal) (bool, error) { return false, nil }

func prepareSessionProcess(c *exec.Cmd)             {}
func ignoreCompletedSessionSignal(exited bool) bool { return false }
