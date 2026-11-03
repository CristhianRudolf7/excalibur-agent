//go:build !linux

package robotlab

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func sidecarProcessGroup() platform.ProcessGroup { return platform.OtherProcessGroup{} }

func lowerSidecarPriority(pid int) error {
	return (platform.OtherProcessPriority{}).LowerProcess(pid)
}
