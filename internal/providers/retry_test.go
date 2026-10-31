package providers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	secs := http.Header{"Retry-After": {"2"}}
	if got := ParseRetryAfter(secs); got != 2*time.Second {
		t.Fatalf("delta-seconds: got %v", got)
	}
	future := http.Header{"Retry-After": {time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)}}
	if got := ParseRetryAfter(future); got != MaxRetryAfter {
		t.Fatalf("HTTP-date must cap at %v, got %v", MaxRetryAfter, got)
	}
	past := http.Header{"Retry-After": {time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)}}
	if got := ParseRetryAfter(past); got != 0 {
		t.Fatalf("past date: got %v", got)
	}
	for _, h := range []http.Header{{}, {"Retry-After": {"soon"}}, {"Retry-After": {"-3"}}} {
		if got := ParseRetryAfter(h); got != 0 {
			t.Fatalf("bad value must give 0, got %v", got)
		}
	}
}

func TestBackoffBounds(t *testing.T) {
	base := 500 * time.Millisecond
	for n := 1; n <= 4; n++ {
		lo := base << (n - 1)
		for range 50 {
			got := Backoff(base, n)
			if got < lo || got > lo+base {
				t.Fatalf("attempt %d: %v outside [%v, %v]", n, got, lo, lo+base)
			}
		}
	}
}

func TestWithRetrySuccessAfterTransient(t *testing.T) {
	ctx := context.Background()
	calls := 0
	err := WithRetry(ctx, 3, time.Millisecond, func(context.Context) error {
		calls++
		if calls < 3 {
			return &ProviderError{Kind: KindTransient, Err: errors.New("boom")}
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestWithRetryStopsOnNonRetryable(t *testing.T) {
	calls := 0
	err := WithRetry(context.Background(), 3, time.Millisecond, func(context.Context) error {
		calls++
		return &ProviderError{Kind: KindAuth, Err: errors.New("no")}
	})
	if err == nil || calls != 1 {
		t.Fatalf("must not retry auth errors: err=%v calls=%d", err, calls)
	}
}

func TestWithRetryExhaustsAttempts(t *testing.T) {
	calls := 0
	err := WithRetry(context.Background(), 2, time.Millisecond, func(context.Context) error {
		calls++
		return &ProviderError{Kind: KindRateLimited, Err: errors.New("slow")}
	})
	if err == nil || calls != 2 {
		t.Fatalf("must stop after 2 attempts: err=%v calls=%d", err, calls)
	}
}

func TestWithRetryHonorsRetryAfter(t *testing.T) {
	start := time.Now()
	err := WithRetry(context.Background(), 2, time.Millisecond, func(context.Context) error {
		return &ProviderError{Kind: KindRateLimited, RetryAfter: 50 * time.Millisecond, Err: errors.New("slow")}
	})
	if err == nil || time.Since(start) < 50*time.Millisecond {
		t.Fatalf("Retry-After not honored: err=%v elapsed=%v", err, time.Since(start))
	}
}

func TestWithRetryContextCancelWins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := WithRetry(ctx, 3, time.Hour, func(context.Context) error {
		return &ProviderError{Kind: KindTransient, Err: errors.New("boom")}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("must return context error, got %v", err)
	}
	if _, ok := err.(*ProviderError); ok {
		t.Fatal("cancellation must not be wrapped as ProviderError")
	}
}

func TestSleepContext(t *testing.T) {
	if err := SleepContext(context.Background(), 0); err != nil {
		t.Fatalf("zero wait on live ctx must be nil, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ctx must win, got %v", err)
	}
}
