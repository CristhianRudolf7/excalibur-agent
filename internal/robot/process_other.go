//go:build !linux && !darwin && !windows

package robot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func supported() error                { return errors.ErrUnsupported }
func configureProcess(cmd *exec.Cmd)  { platform.OtherProcessGroup{}.Configure(cmd) }
func stopProcess(cmd *exec.Cmd) error { return (platform.OtherProcessGroup{}).Kill(cmd.Process.Pid) }

func defaultRobotPython() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local/share/excalibur/robot-venv/bin/python")
}
func defaultRobotScript() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local/share/excalibur/robot-bridge/server.py")
}
func startRobotProcess(cmd *exec.Cmd) error { return cmd.Start() }
func cleanupRobotProcess(*exec.Cmd)         {}
