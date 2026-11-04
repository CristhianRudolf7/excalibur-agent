//go:build !linux && !darwin && !windows

package pdf

import (
	"os/exec"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func startWorkerProcess(cmd *exec.Cmd, _ platform.ProcessGroup) error { return cmd.Start() }
