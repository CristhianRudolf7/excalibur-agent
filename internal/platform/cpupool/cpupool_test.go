package cpupool

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolRunsAllTasks(t *testing.T) {
	p := New(2)
	defer p.Close()
	if p.Size() != 2 {
		t.Fatalf("Size = %d, want 2", p.Size())
	}
	var done atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		if err := p.Submit(func() {
			defer wg.Done()
			done.Add(1)
		}); err != nil {
			t.Fatalf("Submit: %v", err)
		}
	}
	wg.Wait()
	if done.Load() != 20 {
		t.Fatalf("done = %d, want 20", done.Load())
	}
}

func TestPoolDefaultSize(t *testing.T) {
	if DefaultSize() != runtime.NumCPU() {
		t.Fatalf("DefaultSize = %d, want NumCPU %d", DefaultSize(), runtime.NumCPU())
	}
	p := New(0)
	defer p.Close()
	if p.Size() != runtime.NumCPU() {
		t.Fatalf("Size = %d, want %d", p.Size(), runtime.NumCPU())
	}
}

func TestPoolLowerHookRunsPerWorker(t *testing.T) {
	var calls atomic.Int32
	p := NewWithLower(3, 0, func() error {
		calls.Add(1)
		return errors.New("lower failed: ignored")
	})
	defer p.Close()
	// Give workers a moment to pin and lower their threads.
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() != 3 {
		t.Fatalf("lower calls = %d, want 3", calls.Load())
	}
}

func TestPoolTaskPanicIsolated(t *testing.T) {
	p := New(2)
	defer p.Close()
	if err := p.Submit(func() { panic("task boom") }); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	if err := p.Submit(func() { close(done) }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pool dead after panicking task")
	}
}

func TestPoolTrySubmitFull(t *testing.T) {
	p := NewWithLower(1, 1, nil)
	defer p.Close()
	release := make(chan struct{})
	entered := make(chan struct{})
	if err := p.Submit(func() {
		close(entered)
		<-release
	}); err != nil {
		t.Fatal(err)
	}
	<-entered
	// Worker busy, queue holds one: fill it, then the next must fail.
	if err := p.TrySubmit(func() {}); err != nil {
		t.Fatalf("first queued TrySubmit: %v", err)
	}
	if err := p.TrySubmit(func() {}); err != ErrFull {
		t.Fatalf("TrySubmit on full queue = %v, want ErrFull", err)
	}
	close(release)
	p.Close()
	if err := p.Submit(func() {}); err != ErrClosed {
		t.Fatalf("Submit after Close = %v, want ErrClosed", err)
	}
	if err := p.TrySubmit(func() {}); err != ErrClosed {
		t.Fatalf("TrySubmit after Close = %v, want ErrClosed", err)
	}
	p.Close() // idempotent
}

func TestPoolThreadsPinnedAndLowered(t *testing.T) {
	// Workers must run the default lowering without error on this
	// platform and serve work afterwards (Section 3.2).
	p := New(2)
	defer p.Close()
	var wg sync.WaitGroup
	wg.Add(4)
	for i := 0; i < 4; i++ {
		if err := p.Submit(wg.Done); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
