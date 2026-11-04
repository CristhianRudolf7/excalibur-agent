//go:build darwin

package robotlab

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func sidecarProcessGroup() platform.ProcessGroup { return platform.DarwinProcessGroup{} }

func lowerSidecarPriority(pid int) error {
	return (platform.DarwinProcessPriority{Nice: 15}).LowerProcess(pid)
}
