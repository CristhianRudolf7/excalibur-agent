package agenda

import (
	"errors"
	"testing"
	"time"
)

// 17.7/17.8 + 21.5: failure classes, backoff policy, idempotency keys,
// reverse-order compensations.

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		err  error
		want FailureClass
	}{
		{errors.New("provider 503 overloaded, retry-after 30"), FailureTransient},
		{errors.New("429 quota exceeded"), FailureTransient},
		{errors.New("connection reset by peer"), FailureTransient},
		{errors.New("credential revoked by provider"), FailurePermanent},
		{errors.New("access denied for key"), FailurePermanent},
		{errors.New("invalid argument: resource not found"), FailureInput},
		{errors.New("schema validation failed"), FailureInput},
		{errors.New("tests failed: 3 red"), FailureLogic},
		{errors.New("criterion not met"), FailureLogic},
	}
	for _, c := range cases {
		if got := ClassifyFailure(c.err); got != c.want {
			t.Fatalf("%q: want %s, have %s", c.err, c.want, got)
		}
	}
}

func TestDecideRetryTransientSchedule(t *testing.T) {
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour}
	for i, w := range want {
		dec := DecideRetry(FailureTransient, i+1, 0)
		if !dec.Retry || dec.Wait != w {
			t.Fatalf("attempt %d: want retry in %v, have %+v", i+1, w, dec)
		}
	}
	if dec := DecideRetry(FailureTransient, MaxTransientAttempts+1, 0); dec.Retry {
		t.Fatal("no retry past the attempt cap")
	}
	if dec := DecideRetry(FailureTransient, 1, 45*time.Second); !dec.Retry || dec.Wait != 45*time.Second {
		t.Fatalf("Retry-After must win: %+v", dec)
	}
	if dec := DecideRetry(FailureInput, 1, 0); dec.Retry {
		t.Fatal("input failures never blind-retry")
	}
	if dec := DecideRetry(FailureLogic, 1, 0); dec.Retry {
		t.Fatal("logic failures correct instead of retrying")
	}
	if dec := DecideRetry(FailurePermanent, 1, 0); dec.Retry {
		t.Fatal("permanent failures pause and notify")
	}
}

func TestIdempotencyKeyStable(t *testing.T) {
	a := IdempotencyKeyFor("inst1", "n1")
	if a != IdempotencyKeyFor("inst1", "n1") {
		t.Fatal("same node+instance must reuse the key")
	}
	if a == IdempotencyKeyFor("inst1", "n2") || a == IdempotencyKeyFor("inst2", "n1") {
		t.Fatal("keys must differ per node and instance")
	}
}

func TestCompensationOrderReverse(t *testing.T) {
	byNode := map[string]Compensation{
		"a": {NodeID: "a", Undo: "undo-a"},
		"b": {NodeID: "b", Undo: "undo-b"},
		"c": {NodeID: "c", Undo: "undo-c"},
	}
	got := CompensationOrder([]string{"a", "b", "c"}, byNode)
	if len(got) != 3 || got[0].NodeID != "c" || got[1].NodeID != "b" || got[2].NodeID != "a" {
		t.Fatalf("compensations run in reverse: %+v", got)
	}
	if got := CompensationOrder([]string{"a", "x"}, byNode); len(got) != 1 || got[0].NodeID != "a" {
		t.Fatalf("nodes without compensation are skipped: %+v", got)
	}
}
