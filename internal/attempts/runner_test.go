package attempts

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunConcurrentKeepsOrderAndIsolatesErrors(t *testing.T) {
	work := []string{"w0", "w1", "w2"}
	logs := []string{"l0", "l1", "l2"}
	var live atomic.Int32
	var peak atomic.Int32
	fn := func(ctx context.Context, idx int, w, l string) (Outcome, error) {
		if w != work[idx] || l != logs[idx] {
			return Outcome{}, errors.New("wrong dirs")
		}
		cur := live.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		live.Add(-1)
		if idx == 1 {
			return Outcome{}, errors.New("boom")
		}
		return Outcome{State: "completed", Verified: int64(idx)}, nil
	}
	out := RunConcurrent(context.Background(), 2, work, logs, fn)
	if len(out) != 3 {
		t.Fatalf("outcomes = %d, want 3", len(out))
	}
	if out[1].State != "failed" {
		t.Fatalf("attempt 1 = %+v, want failed", out[1])
	}
	if out[0].Verified != 0 || out[2].Verified != 2 {
		t.Fatalf("order broken: %+v", out)
	}
	if peak.Load() > 2 {
		t.Fatalf("peak concurrency = %d, want <= 2", peak.Load())
	}
}
