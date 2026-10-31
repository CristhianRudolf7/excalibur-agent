package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
)

// Section 5.2: P0 keeps a reserved slot and 10% of the minute quota
// under background saturation; lower lanes wait for higher ones; P3
// yields while the user is active.

func granted(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

func TestGatewayP0Reservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := orchestrator.NewGateway(3, 100)
	g.Start(ctx)
	// Two background slots; the third stays free for P0.
	b1 := g.Request(ctx, orchestrator.LaneP1)
	if !granted(b1) {
		t.Fatal("first background slot must grant")
	}
	b2 := g.Request(ctx, orchestrator.LaneP1)
	if !granted(b2) {
		t.Fatal("second background slot must grant")
	}
	b3 := g.Request(ctx, orchestrator.LaneP1)
	if granted(b3) {
		t.Fatal("background must not take the P0-reserved slot")
	}
	// The master turn takes it immediately.
	p0 := g.Request(ctx, orchestrator.LaneP0)
	if !granted(p0) {
		t.Fatal("P0 must obtain the reserved slot under saturation")
	}
	stats := g.Stats()
	if stats.InFlight != 3 || stats.Pending[orchestrator.LaneP1] != 1 {
		t.Fatalf("bad gateway stats: %+v", stats)
	}
	// Freeing the P0 slot alone is not enough: the reserve stays
	// free. Once background work also drains, the waiter proceeds.
	g.Release()
	if granted(b3) {
		t.Fatal("reserve must stay free for P0")
	}
	g.Release()
	if !granted(b3) {
		t.Fatal("drained slots must go to the waiting background lane")
	}
}

func TestGatewayLaneOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := orchestrator.NewGateway(2, 100)
	g.Start(ctx)
	low := g.Request(ctx, orchestrator.LaneP2)
	if !granted(low) {
		t.Fatal("first request must grant")
	}
	p1 := g.Request(ctx, orchestrator.LaneP1)
	p3 := g.Request(ctx, orchestrator.LaneP3)
	g.Release()
	// One slot, two waiters: P1 (higher lane) wins over P3.
	if !granted(p1) {
		t.Fatal("higher lane must win the freed slot")
	}
	if granted(p3) {
		t.Fatal("P3 must keep waiting behind P1")
	}
	g.Release()
	if !granted(p3) {
		t.Fatal("P3 takes the slot once higher lanes are empty")
	}
}

func TestGatewayP3YieldsWhenUserActive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := orchestrator.NewGateway(2, 100)
	g.Start(ctx)
	g.SetUserActive(true)
	p3 := g.Request(ctx, orchestrator.LaneP3)
	if granted(p3) {
		t.Fatal("P3 must yield while the user is active")
	}
	g.SetUserActive(false)
	if !granted(p3) {
		t.Fatal("P3 resumes when the user goes idle")
	}
}

func TestGatewayMinuteQuota(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 10 calls a minute: background lanes keep 10% free (9 usable).
	g := orchestrator.NewGateway(100, 10)
	g.Start(ctx)
	var held []<-chan struct{}
	for i := 0; i < 9; i++ {
		ch := g.Request(ctx, orchestrator.LaneP1)
		if !granted(ch) {
			t.Fatalf("background call %d must grant", i)
		}
		held = append(held, ch)
		_ = held
		g.Release()
	}
	tenth := g.Request(ctx, orchestrator.LaneP1)
	if granted(tenth) {
		t.Fatal("background must leave 10% of the minute quota for P0")
	}
	if !granted(g.Request(ctx, orchestrator.LaneP0)) {
		t.Fatal("P0 must use the reserved quota")
	}
}
