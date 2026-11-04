package cli

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

func newResourceSampler() platform.ResourceSampler {
	return platform.WindowsResourceSampler{}
}
