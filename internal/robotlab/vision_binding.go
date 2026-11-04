package robotlab

import "context"

// NewTool composes the tool and an inert compatibility hook.
// Assign during composition before accepting requests.
func NewTool(sidecar Sidecar, publisher Publisher, asker VisionAsker, hook *func(context.Context, string, string) (any, error)) *Tool {
	loop := &VisionLoop{Sidecar: sidecar, Publisher: publisher, Asker: asker}
	if hook != nil {
		*hook = func(ctx context.Context, session, instruction string) (any, error) {
			return loop.VisionStep(ctx, session, instruction)
		}
	}
	return &Tool{Sidecar: sidecar, Publisher: publisher, Vision: loop}
}
