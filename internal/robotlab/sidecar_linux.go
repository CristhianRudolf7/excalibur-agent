//go:build linux

package robotlab

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func sidecarProcessGroup() platform.ProcessGroup { return platform.LinuxProcessGroup{} }

func lowerSidecarPriority(pid int) error {
	return (platform.LinuxProcessPriority{Nice: 15}).LowerProcess(pid)
}
