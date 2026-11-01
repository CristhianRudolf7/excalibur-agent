package tools

import (
	"context"
	"time"
)

type commandDeadlineKey struct{}

// WithCommandDeadline limits the actual terminal process, including sessions
// which outlive the initial exec response. A zero bound enables cancellation
// of the initial wait without limiting the process lifetime.
func WithCommandDeadline(ctx context.Context, bound time.Duration) context.Context {
	return context.WithValue(ctx, commandDeadlineKey{}, bound)
}
