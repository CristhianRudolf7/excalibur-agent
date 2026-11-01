package models

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

var retryText = regexp.MustCompile(`(?i)try again in\s+([0-9]+(?:\.[0-9]+)?)\s*(ms|s|seconds?|milliseconds?)\b`)

// Provider retry v2 (EXCALIBUR_PROVIDER_RETRY_V2, default ON).
//
// Rate limits, overloads (HTTP 5xx), connection resets and request timeouts
// are retried with exponential backoff: 1 s, 2 s, 4 s and so on, capped at
// 60 s, each step jittered down by up to 20 %. A server Retry-After (or a
// "try again in" hint) replaces the computed step. Waiting never outlives the
// caller's wall budget: the gateway stops retrying when the next wait would
// end after the deadline. Network failures get at most transportRetryAttempts
// tries per endpoint; rate limits and overloads keep retrying until the wall
// budget runs out, as before. Setting the switch to 0 restores the legacy
// schedule (capped at 30 s, network failures bounded by the attempt budget).
const (
	retryV2BaseDelay = time.Second
	retryV2MaxDelay  = 60 * time.Second
	// transportRetryAttempts counts the first try: network failures are
	// retried at 1 s, 2 s, 4 s, 8 s and 16 s, then the error is returned.
	transportRetryAttempts = 6
	// DefaultModelRequestTimeout bounds one provider HTTP request
	// (EXCALIBUR_MODEL_REQUEST_TIMEOUT, seconds or a Go duration; 0 disables).
	DefaultModelRequestTimeout = 300 * time.Second
)

// ProviderRetryV2Enabled reports EXCALIBUR_PROVIDER_RETRY_V2 (default ON).
func ProviderRetryV2Enabled() bool { return os.Getenv("EXCALIBUR_PROVIDER_RETRY_V2") != "0" }

// ModelRequestTimeout reads EXCALIBUR_MODEL_REQUEST_TIMEOUT. Unparseable
// values keep the default; "0" disables the per-request deadline.
func ModelRequestTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("EXCALIBUR_MODEL_REQUEST_TIMEOUT"))
	if raw == "" {
		return DefaultModelRequestTimeout
	}
	if d, err := time.ParseDuration(raw); err == nil && d >= 0 {
		return d
	}
	if secs, err := strconv.ParseFloat(raw, 64); err == nil && secs >= 0 && secs < 1e9 {
		return time.Duration(secs * float64(time.Second))
	}
	return DefaultModelRequestTimeout
}

// retryDelay returns the wait before the next try. attempt is the 1-based
// number of the failed try. Server guidance (Retry-After or a "try again in"
// hint) is always honored; the exponential step applies only without it.
func retryDelay(e *providers.ProviderError, base time.Duration, attempt int) time.Duration {
	if ProviderRetryV2Enabled() {
		return retryDelayV2(e, attempt)
	}
	delay := serverHint(e)
	if delay <= 0 {
		delay = min(base, 30*time.Second)
		for i := 1; i < attempt && delay < 30*time.Second; i++ {
			delay = min(delay*2, 30*time.Second)
		}
		return min(delay+time.Duration(rand.Float64()*0.1*float64(delay)), 30*time.Second)
	}
	return delay + time.Duration(rand.Float64()*0.1*float64(min(delay, time.Second)))
}

// retryDelayV2 implements the v2 schedule documented on the constants above.
func retryDelayV2(e *providers.ProviderError, attempt int) time.Duration {
	if delay := serverHint(e); delay > 0 {
		return delay + time.Duration(rand.Float64()*0.1*float64(min(delay, time.Second)))
	}
	wait := retryV2BaseDelay
	for i := 1; i < attempt && wait < retryV2MaxDelay; i++ {
		wait *= 2
	}
	wait = min(wait, retryV2MaxDelay)
	// Jitter moves each step down by up to 20 %, so the nominal schedule
	// (1 s, 2 s, 4 s, ... 60 s) is never exceeded.
	return wait - time.Duration(rand.Float64()*0.2*float64(wait))
}

// serverHint returns the provider's own wait: the Retry-After value carried
// by the error, or a "try again in" hint in its text. Zero when none.
func serverHint(e *providers.ProviderError) time.Duration {
	delay := e.RetryAfter
	if delay > 0 {
		return delay
	}
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
	return delay
}

// budgetRetry reports whether an error class is retried past the attempt
// budget under the legacy policy (v2 off).
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

// budgetRetryAllowed reports whether a failed try (1-based attempt) may be
// retried beyond the configured attempt budget. Under v2, rate limits and
// overloads retry until the wall budget ends, and network failures (resets,
// timeouts, decode errors) retry up to transportRetryAttempts tries.
func budgetRetryAllowed(err error, attempt int) bool {
	if !ProviderRetryV2Enabled() {
		return budgetRetry(err)
	}
	var e *providers.ProviderError
	if !errors.As(err, &e) {
		return false
	}
	switch e.Kind {
	case providers.KindRateLimited:
		return true
	case providers.KindTransient:
		if overloadStatus.MatchString(e.Error()) {
			return true
		}
		return attempt < transportRetryAttempts
	}
	return false
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
	// Switches counts credential switches to the fallback key in this call.
	Switches int
}
