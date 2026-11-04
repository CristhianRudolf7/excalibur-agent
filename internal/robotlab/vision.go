package robotlab

import "context"

// VisionLoop preserves the composition API used by embedded entry points.
// Training observes images through Tool look/media actions instead.
type VisionLoop struct {
	Sidecar   Sidecar
	Publisher Publisher
	Asker     VisionAsker
}

// VisionStep is a compatibility hook with no side effects.
func (*VisionLoop) VisionStep(context.Context, string, string) (any, error) {
	return nil, unavailable("Use the robot_lab look action during a training session")
}
