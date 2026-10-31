package workspace

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Section 16.6 row 1: two runs race for one file; the loser waits
// 30 s (shortened here) and gets the holder plus its activity.
func TestLeasesWaitReportsHolder(t *testing.T) {
	m := NewLeaseManager(WithLeaseTimings(60*time.Millisecond, time.Minute))
	rel, err := m.Acquire("/proj/a.go", "run-1", "editing main loop")
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	start := time.Now()
	_, err = m.Acquire("/proj/a.go", "run-2", "fixing imports")
	took := time.Since(start)
	var held *HeldError
	if !errors.As(err, &held) {
		t.Fatalf("want *HeldError, got %v", err)
	}
	if held.Holder != "run-1" || held.Activity != "editing main loop" {
		t.Fatalf("holder report wrong: %+v", held)
	}
	if took < 50*time.Millisecond {
		t.Fatalf("must wait the budget before reporting, waited %v", took)
	}
}

// Section 16.2: the orchestrator heartbeat renews leases of running
// runs (even during long tools); expiry without heartbeat is 10 min.
func TestLeasesHeartbeatRenewal(t *testing.T) {
	now := time.Now()
	m := NewLeaseManager(
		WithLeaseTimings(time.Second, 10*time.Minute),
		WithLeaseClock(func() time.Time { return now }),
	)
	rel, err := m.Acquire("/proj/a.go", "run-1", "long tool")
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	// 9 minutes pass with heartbeats: still held.
	for i := 0; i < 9; i++ {
		now = now.Add(time.Minute)
		m.Heartbeat("run-1")
	}
	now = now.Add(9 * time.Minute)
	if _, ok := m.Holding("/proj/a.go"); !ok {
		t.Fatal("heartbeat must renew during long tools")
	}
	m.Expire()
	if _, ok := m.Holding("/proj/a.go"); !ok {
		t.Fatal("must still be held right after expire sweep within budget")
	}
	// 10 minutes without heartbeat: expired.
	now = now.Add(10*time.Minute + time.Second)
	if n := m.Expire(); n != 1 {
		t.Fatalf("want 1 expiry, got %d", n)
	}
	if _, ok := m.Holding("/proj/a.go"); ok {
		t.Fatal("must expire 10 min without heartbeat")
	}
}

// Section 16.2/16.6: pause, block and user-wait release leases; tool
// and subagent waits keep them; resume re-acquires.
func TestLeasesStateReleases(t *testing.T) {
	m := NewLeaseManager(WithLeaseTimings(time.Second, 10*time.Minute))
	mustAcquire := func() {
		if _, err := m.Acquire("/proj/a.go", "run-1", "work"); err != nil {
			t.Fatal(err)
		}
	}
	mustAcquire()
	m.NotifyRunState("run-1", types.RunPaused, "")
	if _, ok := m.Holding("/proj/a.go"); ok {
		t.Fatal("paused must release leases")
	}
	mustAcquire()
	m.NotifyRunState("run-1", types.RunBlocked, "")
	if _, ok := m.Holding("/proj/a.go"); ok {
		t.Fatal("blocked must release leases")
	}
	mustAcquire()
	m.NotifyRunState("run-1", types.RunWaiting, types.WaitUser)
	if _, ok := m.Holding("/proj/a.go"); ok {
		t.Fatal("waiting(user) must release leases")
	}
	// Re-acquire after resume, like a real resume would.
	mustAcquire()
	m.NotifyRunState("run-1", types.RunRunning, "")
	if _, ok := m.Holding("/proj/a.go"); !ok {
		t.Fatal("running must keep leases")
	}
	m.NotifyRunState("run-1", types.RunWaiting, types.WaitTool)
	if _, ok := m.Holding("/proj/a.go"); !ok {
		t.Fatal("waiting(tool) must keep leases during long tools")
	}
	m.NotifyRunState("run-1", types.RunWaiting, types.WaitSubagents)
	if _, ok := m.Holding("/proj/a.go"); !ok {
		t.Fatal("waiting(subagents) must keep leases")
	}
	m.NotifyRunState("run-1", types.RunCompleted, "")
	if _, ok := m.Holding("/proj/a.go"); ok {
		t.Fatal("terminal runs must release leases")
	}
}

// Section 16.2: expired holders stop blocking; the table over time
// is observable via Snapshot.
func TestLeaseSnapshotAndExpiry(t *testing.T) {
	now := time.Now()
	cur := now
	m := NewLeaseManager(
		WithLeaseTimings(time.Second, time.Minute),
		WithLeaseClock(func() time.Time { return cur }),
	)
	if _, err := m.Acquire("/b.go", "r1", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire("/a.go", "r2", "y"); err != nil {
		t.Fatal(err)
	}
	snap := m.Snapshot()
	if len(snap) != 2 || snap[0].Path != "/a.go" || snap[1].Path != "/b.go" {
		t.Fatalf("snapshot must be sorted by path: %+v", snap)
	}
	cur = cur.Add(2 * time.Minute)
	if n := m.Expire(); n != 2 {
		t.Fatalf("want 2 expiries, got %d", n)
	}
	if len(m.Snapshot()) != 0 {
		t.Fatal("table must be empty after expiry")
	}
}

// Section 16.2: release is holder-scoped and idempotent; concurrent
// acquirers serialize.
func TestLeasesSerializeConcurrent(t *testing.T) {
	m := NewLeaseManager(WithLeaseTimings(5*time.Second, time.Minute))
	rel, err := m.Acquire("/x.go", "r1", "")
	if err != nil {
		t.Fatal(err)
	}
	// Wrong holder cannot release.
	m.Release("/x.go", "r2")
	if _, ok := m.Holding("/x.go"); !ok {
		t.Fatal("wrong holder must not release")
	}
	var wg sync.WaitGroup
	done := make(chan string, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		r, err := m.Acquire("/x.go", "r2", "")
		if err != nil {
			done <- "err:" + err.Error()
			return
		}
		defer r()
		done <- "ok"
	}()
	rel()
	wg.Wait()
	if got := <-done; got != "ok" {
		t.Fatalf("second acquirer must proceed after release, got %q", got)
	}
}
