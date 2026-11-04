//go:build darwin

package pdf

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func workerPlatform() (platform.ProcessGroup, platform.ResourceSampler) {
	return platform.DarwinProcessGroup{}, platform.DarwinResourceSampler{}
}
