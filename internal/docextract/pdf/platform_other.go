//go:build !linux && !darwin && !windows

package pdf

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func workerPlatform() (platform.ProcessGroup, platform.ResourceSampler) { return nil, nil }
