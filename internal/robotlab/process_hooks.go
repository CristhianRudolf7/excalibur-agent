package robotlab

import (
	"os/exec"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// Windows starts suspended until job assignment; existing backends need no hook.
func startSidecarCommand(cmd *exec.Cmd, group platform.ProcessGroup) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	if starter, ok := group.(interface{ Started(*exec.Cmd) error }); ok {
		if err := starter.Started(cmd); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	return nil
}
func cleanupSidecarGroup(group platform.ProcessGroup) {
	if closer, ok := group.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}
