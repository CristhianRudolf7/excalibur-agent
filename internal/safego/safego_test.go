package safego

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting: %s", msg)
}

func TestGoRunsFunction(t *testing.T) {
	var mu sync.Mutex
	done := false
	Go("test-ok", func(string, any, []byte) {
		t.Error("onFail must not fire on success")
	}, func() {
		mu.Lock()
		defer mu.Unlock()
		done = true
	})
	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return done
	}, "function to run")
}

func TestGoRecoversPanic(t *testing.T) {
	failed := make(chan string, 1)
	var got any
	boom := errors.New("boom")
	Go("test-panic", func(owner string, recovered any, stack []byte) {
		got = recovered
		if len(stack) == 0 {
			t.Error("stack should not be empty")
		}
		failed <- owner
	}, func() { panic(boom) })
	select {
	case owner := <-failed:
		if owner != "test-panic" {
			t.Fatalf("wrong owner %q", owner)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onFail was not called")
	}
	if got != boom {
		t.Fatalf("recovered value: %v", got)
	}
}

func TestGoNilCallbackSurvives(t *testing.T) {
	done := make(chan struct{})
	Go("test-nil", nil, func() {
		panic("unobserved")
	})
	// The process surviving the panic is the assertion; give the
	// guarded goroutine a moment, then finish normally.
	time.Sleep(100 * time.Millisecond)
	close(done)
	<-done
}
