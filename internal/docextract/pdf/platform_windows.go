package pdf

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func workerPlatform() (platform.ProcessGroup, platform.ResourceSampler) {
	return platform.WindowsProcessGroup{}, platform.WindowsResourceSampler{}
}
