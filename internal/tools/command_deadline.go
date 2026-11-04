package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type commandDeadlineKey struct{}

// Darwin replaces these hooks with a native process-group timer because
// stock macOS has no GNU timeout. Other platforms retain the shell wrapper.
var wrapCommandDeadline = func(command string, bound time.Duration) string {
	return fmt.Sprintf("timeout --signal=TERM --kill-after=1s %.3fs '%s' -c '%s'", max(bound.Seconds(), 0.001), strings.ReplaceAll(DetectShell(), "'", "'\"'\"'"), strings.ReplaceAll(command, "'", "'\"'\"'"))
}

var armSessionDeadline = func(context.Context, *Session) {}

// WithCommandDeadline limits the actual terminal process, including sessions
// which outlive the initial exec response. A zero bound enables cancellation
// of the initial wait without limiting the process lifetime.
func WithCommandDeadline(ctx context.Context, bound time.Duration) context.Context {
	return context.WithValue(ctx, commandDeadlineKey{}, bound)
}
