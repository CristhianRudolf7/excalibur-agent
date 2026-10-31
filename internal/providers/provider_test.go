package providers

import "testing"

func TestUsageAdd(t *testing.T) {
	var u Usage
	u.Add(Usage{Input: 100, CacheRead: 40, CacheWrite: 10, Output: 20, Reasoning: 5})
	u.Add(Usage{Input: 50, Output: 10})
	if u.Input != 150 || u.CacheRead != 40 || u.CacheWrite != 10 || u.Output != 30 || u.Reasoning != 5 {
		t.Fatalf("bad usage total: %+v", u)
	}
}

func TestProviderErrorRetryable(t *testing.T) {
	for _, kind := range []ErrorKind{KindTransient, KindRateLimited} {
		if !(&ProviderError{Kind: kind}).Retryable() {
			t.Fatalf("%s should be retryable", kind)
		}
	}
	for _, kind := range []ErrorKind{KindAuth, KindInput, KindContentRejected, KindTruncated} {
		if (&ProviderError{Kind: kind}).Retryable() {
			t.Fatalf("%s should not be retryable", kind)
		}
	}
	if err := (&ProviderError{Kind: KindTransient, Provider: "p", Model: "m"}).Unwrap(); err != nil {
		t.Fatal("unwrap of nil cause should be nil")
	}
}
