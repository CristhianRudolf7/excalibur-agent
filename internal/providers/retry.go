package providers

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Retry tuning for provider calls (Section 19.5). Values are initial
// and calibrated later; see docs/decisions/0003-*.
const (
	// DefaultMaxAttempts bounds one call including the first try.
	DefaultMaxAttempts = 3
	// DefaultBaseDelay is the first backoff step; it doubles per retry.
	DefaultBaseDelay = 500 * time.Millisecond
	// MaxRetryAfter caps a honored Retry-After value.
	MaxRetryAfter = 60 * time.Second
)

// ParseRetryAfter parses a Retry-After header value: either
// delta-seconds or an HTTP date. It returns 0 when the value is
// missing or unparseable. The result is capped at MaxRetryAfter.
func ParseRetryAfter(header http.Header) time.Duration {
	raw := strings.TrimSpace(header.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if secs, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if secs < 0 {
			return 0
		}
		return min(time.Duration(secs)*time.Second, MaxRetryAfter)
	}
	if t, err := http.ParseTime(raw); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return min(d, MaxRetryAfter)
	}
	return 0
}

// Backoff returns the wait before retry number n (1-based): base * 2^(n-1)
// plus uniform jitter in [0, base). It is a pure function so tests can
// assert its bounds without sleeping.
func Backoff(base time.Duration, n int) time.Duration {
	if n < 1 {
		n = 1
	}
	wait := base << (n - 1)
	return wait + time.Duration(rand.Int64N(int64(base)+1))
}

// WithRetry runs op until it succeeds, fails non-retryably, or runs
// out of attempts. Retryable ProviderErrors wait honoring their
// RetryAfter first, falling back to Backoff(baseDelay, attempt).
// A done context aborts the wait and returns the context error
// unwrapped, so cancellation is never mistaken for a failure.
func WithRetry(ctx context.Context, maxAttempts int, baseDelay time.Duration, op func(context.Context) error) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if baseDelay <= 0 {
		baseDelay = DefaultBaseDelay
	}
	var err error
	for attempt := 1; ; attempt++ {
		err = op(ctx)
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		perr, ok := err.(*ProviderError)
		if !ok || !perr.Retryable() {
			return err
		}
		if attempt >= maxAttempts {
			return err
		}
		wait := perr.RetryAfter
		if wait <= 0 {
			wait = Backoff(baseDelay, attempt)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// SleepContext waits for d or until ctx is done. Adapters use it for
// Retry-After handling in streaming paths where WithRetry cannot
// rewind an already-started stream.
func SleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
