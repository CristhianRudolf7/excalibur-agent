//go:build !linux

package automations

import (
	"errors"
	"os/exec"
)

func configureScriptProcess(*exec.Cmd) error { return errors.ErrUnsupported }

func cleanupScriptProcess(*exec.Cmd) {}
