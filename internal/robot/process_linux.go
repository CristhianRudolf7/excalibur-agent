//go:build linux

package robot

import (
	"os/exec"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func supported() error                { return nil }
func configureProcess(cmd *exec.Cmd)  { platform.LinuxProcessGroup{}.Configure(cmd) }
func stopProcess(cmd *exec.Cmd) error { return (platform.LinuxProcessGroup{}).Kill(cmd.Process.Pid) }
