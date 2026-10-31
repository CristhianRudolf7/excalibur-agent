package daemon

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Section 23.5 `avail`: receipt-ack and fast-lane latency with 0, 4
// and 8 background runs, with the CPU saturated by tool-like work and
// with the provider down. The interactive path never waits on
// admission, so loaded latencies must stay near unloaded ones; the
// suite records p50/p95 as evidence and enforces generous absolute
// ceilings so shared CI machines pass deterministically.

type availRow struct {
	name     string
	bgRuns   int
	saturate bool
	ackP95   time.Duration
	fastP95  time.Duration
	ackP50   time.Duration
	fastP50  time.Duration
}

func seedBackground(t *testing.T, orch *orchestrator.Orchestrator, n int) {
	t.Helper()
	mag, err := orch.RegisterMaster(fmt.Sprintf("m-%d", n), types.GeneralScope())
	if err != nil {
		// One master per orchestrator; reuse on later calls.
		return
	}
	for i := 0; i < n; i++ {
		card := orchestrator.DelegationCard{
			Name: fmt.Sprintf("bg-%d", i), Description: "background load",
			Goal: "burn", Deliverable: "heat", Scope: types.GeneralScope(),
			Done: []string{"done"}, Report: "r",
		}
		if _, _, err := orch.Spawn(mag.ID, card); err != nil {
			t.Fatalf("spawn bg: %v", err)
		}
	}
}

func burnCPUs(stop *atomic.Bool) {
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			x := 0.0
			for !stop.Load() {
				for j := 0; j < 1000; j++ {
					x += float64(j) * 1.000001
				}
			}
			_ = x
		}()
	}
}

func pct(durs []time.Duration, q float64) time.Duration {
	if len(durs) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), durs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(q * float64(len(s)-1))
	return s[idx]
}

func TestAvailSuite(t *testing.T) {
	const samples = 30
	cases := []struct {
		name     string
		bgRuns   int
		saturate bool
	}{
		{"idle", 0, false},
		{"loaded-4", 4, false},
		{"loaded-8", 8, false},
		{"loaded-8-saturated", 8, true},
	}
	var rows []availRow
	for _, c := range cases {
		bus := telemetry.NewBus(nil)
		store := orchestrator.NewMemoryStore()
		orch := orchestrator.New(orchestrator.DefaultConfig(), store, orchestrator.NoopJournal{})
		seedBackground(t, orch, c.bgRuns)
		ctrls := &OrchControl{Orch: orch, Store: store}
		// Provider down: nil model degrades to the deterministic
		// receipt while the fast lane keeps answering (Section 5.3.7).
		m := master.New(master.Config{}, master.NewMemStore(), master.NewLexicon(), nil, ctrls, nil, nil)
		backend := NewDaemonBackend(m, ctrls, bus)
		g := gateway.New(gateway.DefaultConfig(), backend, gateway.NewMemoryBlobs(), bus)
		d := New(DefaultConfig(), Deps{Gate: g, Master: m, Orch: orch, Bus: bus})
		addr, err := d.Start(context.Background())
		if err != nil {
			t.Fatalf("%s: start: %v", c.name, err)
		}
		func() {
			defer func() { _ = d.Stop(context.Background()) }()
			var stop atomic.Bool
			if c.saturate {
				burnCPUs(&stop)
				defer stop.Store(true)
			}
			_ = addr
			var acks, fasts []time.Duration
			for i := 0; i < samples; i++ {
				cnv := fmt.Sprintf("cnv-%s-%d", c.name, i)
				start := time.Now()
				if _, _, err := backend.ConversationSend(context.Background(), "cli", fmt.Sprintf("k-%d", i), cnv, "hello"); err != nil {
					t.Fatalf("%s: send: %v", c.name, err)
				}
				acks = append(acks, time.Since(start))
				f0 := time.Now()
				if _, err := ctrls.Snapshot(mustAnyRun(t, ctrls)); err != nil {
					// No runs seeded (idle case): snapshot the
					// empty list path instead.
					_ = ctrls.ListRuns()
				}
				fasts = append(fasts, time.Since(f0))
			}
			rows = append(rows, availRow{
				name: c.name, bgRuns: c.bgRuns, saturate: c.saturate,
				ackP50: pct(acks, 0.5), ackP95: pct(acks, 0.95),
				fastP50: pct(fasts, 0.5), fastP95: pct(fasts, 0.95),
			})
		}()
	}
	for _, r := range rows {
		t.Logf("avail/%s: ack p50=%v p95=%v fast p50=%v p95=%v",
			r.name, r.ackP50, r.ackP95, r.fastP50, r.fastP95)
		if r.ackP95 > 2*time.Second {
			t.Fatalf("avail/%s: ack p95 %v exceeds 2s", r.name, r.ackP95)
		}
		if r.fastP95 > 2*time.Second {
			t.Fatalf("avail/%s: fast p95 %v exceeds 2s", r.name, r.fastP95)
		}
	}
}

func mustAnyRun(t *testing.T, ctrls *OrchControl) string {
	t.Helper()
	rows := ctrls.ListRuns()
	if len(rows) == 0 {
		return "run_missing"
	}
	return rows[0].RunID
}
