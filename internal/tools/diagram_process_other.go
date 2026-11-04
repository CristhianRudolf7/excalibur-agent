//go:build !linux && !darwin && !windows

package tools

import "os/exec"

func killDiagramProcess(cmd *exec.Cmd) error { return cmd.Process.Kill() }
