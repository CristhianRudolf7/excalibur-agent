//go:build !linux && !darwin && !windows

package automations

import (
	"errors"
	"os/exec"
)

func configureScriptProcess(*exec.Cmd) error { return errors.ErrUnsupported }

func cleanupScriptProcess(*exec.Cmd) {}

func scriptInvocation(binary string) (string, []string, error) { return binary, nil, nil }
func runScriptProcess(cmd *exec.Cmd) error                     { return cmd.Run() }
