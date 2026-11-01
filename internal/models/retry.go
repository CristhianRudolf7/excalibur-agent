package models

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

var retryText = regexp.MustCompile(`(?i)try again in\s+([0-9]+(?:\.[0-9]+)?)\s*(ms|s|seconds?|milliseconds?)\b`)

// retryDelay preserves server delays, adding only positive jitter. The
// exponential fallback is capped before jitter so it never exceeds 30s.
func retryDelay(e *providers.ProviderError, base time.Duration, attempt int) time.Duration {
	delay := e.RetryAfter
	if delay <= 0 {
		if m := retryText.FindStringSubmatch(e.Error()); m != nil {
			n, _ := strconv.ParseFloat(m[1], 64)
			unit := time.Second
			if strings.HasPrefix(strings.ToLower(m[2]), "m") {
				unit = time.Millisecond
			}
			if n > 0 && n < float64(time.Duration(1<<63-1))/float64(unit) {
				delay = time.Duration(n * float64(unit))
			}
		}
	}
	if delay <= 0 {
		delay = min(base, 30*time.Second)
		for i := 1; i < attempt && delay < 30*time.Second; i++ {
			delay = min(delay*2, 30*time.Second)
		}
		return min(delay+time.Duration(rand.Float64()*0.1*float64(delay)), 30*time.Second)
	}
	return delay + time.Duration(rand.Float64()*0.1*float64(min(delay, time.Second)))
}

func budgetRetry(err error) bool {
	var e *providers.ProviderError
	if !errors.As(err, &e) {
		return false
	}
	if e.Kind == providers.KindRateLimited {
		return true
	}
	// Transport failures retain bounded retries. Only server overloads
	// keep waiting until the caller's wall-clock deadline.
	return e.Kind == providers.KindTransient && overloadStatus.MatchString(e.Error())
}

var overloadStatus = regexp.MustCompile(`\bhttp 5[0-9]{2}\b`)

// serverDelay reads the provider's reset durations without changing the
// provider error contract. When multiple limits apply, honor the latest.
func serverDelay(h http.Header, now time.Time) time.Duration {
	var delay time.Duration
	for name, values := range h {
		lower := strings.ToLower(name)
		if lower != "retry-after" && !strings.HasPrefix(lower, "x-ratelimit-reset-") {
			continue
		}
		for _, raw := range values {
			raw = strings.TrimSpace(raw)
			var d time.Duration
			if parsed, err := time.ParseDuration(raw); err == nil {
				d = parsed
			} else if date, err := http.ParseTime(raw); err == nil {
				d = date.Sub(now)
			} else if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds > 0 && seconds < 1e12 {
				if lower != "retry-after" && seconds > 1e9 {
					d = time.Unix(0, int64(seconds*1e9)).Sub(now)
				} else {
					d = time.Duration(seconds * float64(time.Second))
				}
			}
			delay = max(delay, d)
		}
	}
	return delay
}

type retryStatsKey struct{}
type retryAttemptKey struct{}
type retryWaitStats struct {
	Count    int
	Duration time.Duration
}
