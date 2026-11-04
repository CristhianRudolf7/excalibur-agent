package codeintel

import (
	"context"
	"time"
)

// windowsClockStep is the coarsest step of the Windows monotonic clock: it
// advances with the timer interrupt (15.6 ms by default), so a deadline that
// falls inside the current tick is not yet "past" when read from time.Now.
const windowsClockStep = 16 * time.Millisecond

func diagramIndexContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Timer delivery and the clock itself can lag a short deadline on
	// Windows; treat a deadline within one clock step as reached.
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < windowsClockStep {
		return context.DeadlineExceeded
	}
	return nil
}
