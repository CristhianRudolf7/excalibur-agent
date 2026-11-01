package attempts

import (
	"context"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// RunFunc executes one attempt in its isolated working directory and
// reports its own executable evidence as an Outcome.
type RunFunc func(ctx context.Context, index int, workDir, logDir string) (Outcome, error)

// RunConcurrent executes fn for every work/log dir pair, bounding
// simultaneous attempts to maxConcurrency (at least 1). Results keep
// launch order; a RunFunc error becomes a failed Outcome so one bad
// attempt never hides the rest.
func RunConcurrent(ctx context.Context, maxConcurrency int, workDirs, logDirs []string, fn RunFunc) []Outcome {
	n := len(workDirs)
	if len(logDirs) < n {
		n = len(logDirs)
	}
	out := make([]Outcome, n)
	if n == 0 {
		return out
	}
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}
	if maxConcurrency > n {
		maxConcurrency = n
	}
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		idx := i
		safego.Go("attempts/run", nil, func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				out[idx] = Outcome{Index: idx, WorkDir: workDirs[idx], State: "failed", Summary: "attempt canceled before start: " + ctx.Err().Error()}
				return
			}
			oc, err := fn(ctx, idx, workDirs[idx], logDirs[idx])
			if err != nil {
				oc = Outcome{Index: idx, WorkDir: workDirs[idx], LogDir: logDirs[idx], State: "failed", Summary: err.Error()}
			}
			oc.Index = idx
			if oc.WorkDir == "" {
				oc.WorkDir = workDirs[idx]
			}
			if oc.LogDir == "" && idx < len(logDirs) {
				oc.LogDir = logDirs[idx]
			}
			out[idx] = oc
		})
	}
	done := make(chan struct{})
	safego.Go("attempts/wait", nil, func() {
		defer close(done)
		wg.Wait()
	})
	select {
	case <-done:
	case <-ctx.Done():
		// The shared deadline fired while attempts were still
		// running: wait for the in-flight attempts to report (they
		// observe the same ctx) instead of returning partial rows.
		<-done
	}
	return out
}
