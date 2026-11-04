package main

import (
	"os/exec"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func init() {
	stopClassroomDaemon = func(cmd *exec.Cmd) error {
		return platform.RequestDaemonStop(cmd.Process.Pid, 5*time.Second)
	}
}
