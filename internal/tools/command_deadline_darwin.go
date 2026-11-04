package tools

import (
	"context"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

func init() {
	// Pass the script directly to its selected shell, preserving quoting and
	// avoiding any dependency on GNU timeout or a second shell invocation.
	wrapCommandDeadline = func(command string, _ time.Duration) string { return command }
	armSessionDeadline = armDarwinSessionDeadline
}

func armDarwinSessionDeadline(ctx context.Context, s *Session) {
	bound, _ := ctx.Value(commandDeadlineKey{}).(time.Duration)
	pid := s.rootPID()
	if bound <= 0 || pid <= 0 || !s.snapshot() {
		return
	}
	timer := time.NewTimer(bound)
	safego.Go("tools/terminal-deadline", nil, func() {
		defer timer.Stop()
		select {
		case <-s.done:
			return
		case <-timer.C:
			// Cancel the entire group, including descendants that ignore TERM.
			// Keep the completed session available for exec_io and exit records.
			stopGroup(pid)
		}
	})
}
