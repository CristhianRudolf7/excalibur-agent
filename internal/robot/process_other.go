//go:build !linux

package robot

import (
	"errors"
	"os/exec"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func supported() error                { return errors.ErrUnsupported }
func configureProcess(cmd *exec.Cmd)  { platform.OtherProcessGroup{}.Configure(cmd) }
func stopProcess(cmd *exec.Cmd) error { return (platform.OtherProcessGroup{}).Kill(cmd.Process.Pid) }
