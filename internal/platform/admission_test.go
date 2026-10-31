package platform

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestAdmissionCapAndDefault(t *testing.T) {
	a := NewAdmission(3)
	if a.Cap() != 3 {
		t.Fatalf("Cap = %d, want 3", a.Cap())
	}
	d := DefaultAdmission(DefaultReservedCPUs)
	want := runtime.NumCPU() - DefaultReservedCPUs
	if want < 1 {
		want = 1
	}
	if d.Cap() != want {
		t.Fatalf("DefaultAdmission cap = %d, want %d", d.Cap(), want)
	}
	if NewAdmission(0).Cap() != 1 {
		t.Fatal("non-positive n must mean one slot")
	}
}

func TestAdmissionBlocksAtCap(t *testing.T) {
	a := NewAdmission(2)
	ctx := context.Background()
	r1, err := a.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r1()
	r2, err := a.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r2()
	if a.InUse() != 2 {
		t.Fatalf("InUse = %d, want 2", a.InUse())
	}
	if _, ok := a.TryAcquire(); ok {
		t.Fatal("TryAcquire must fail at cap")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r3, err := a.Acquire(ctx)
		if err != nil {
			return
		}
		r3()
	}()
	select {
	case <-done:
		t.Fatal("Acquire must block while slots are held")
	case <-time.After(100 * time.Millisecond):
	}
	r2()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked Acquire did not proceed after release")
	}
}

func TestAdmissionContextCancel(t *testing.T) {
	a := NewAdmission(1)
	r, err := a.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer r()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := a.Acquire(ctx); err == nil {
		t.Fatal("Acquire must honour ctx expiry")
	}
}

func TestAdmissionClose(t *testing.T) {
	a := NewAdmission(1)
	r, err := a.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan error, 1)
	go func() {
		_, err := a.Acquire(context.Background())
		waiting <- err
	}()
	time.Sleep(50 * time.Millisecond)
	a.Close()
	select {
	case err := <-waiting:
		if err != ErrAdmissionClosed {
			t.Fatalf("blocked Acquire after Close = %v, want ErrAdmissionClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not unblock Acquire")
	}
	r() // held slots stay valid until released
	if _, err := a.Acquire(context.Background()); err != ErrAdmissionClosed {
		t.Fatalf("Acquire after Close = %v", err)
	}
	a.Close() // idempotent
}

func TestAdmissionConcurrent(t *testing.T) {
	a := NewAdmission(4)
	var mu sync.Mutex
	inside, peak := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rel, err := a.Acquire(context.Background())
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer rel()
			mu.Lock()
			inside++
			if inside > peak {
				peak = inside
			}
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			inside--
			mu.Unlock()
		}()
	}
	wg.Wait()
	if peak > 4 {
		t.Fatalf("peak concurrency %d exceeds cap 4", peak)
	}
}
