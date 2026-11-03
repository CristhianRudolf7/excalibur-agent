package master

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntakeUsesCurrentProfileZone(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprint("deferred=", deferred), func(t *testing.T) {
			t.Setenv("EXCALIBUR_INTAKE_GATED_TRIAGE", "0")
			store := NewMemStore()
			observed := make(chan Preanalysis, 4)
			// The channel synchronization avoids a FakeDecider counter read race.
			dec := &FakeDecider{Fn: func(_ string, pre Preanalysis) Triage {
				observed <- pre
				return Triage{Type: TriageConversation, Confidence: 1}
			}}
			m := New(Config{}, store, nil, dec, nil, nil, nil)
			var current atomic.Value
			current.Store("UTC")
			m.SetUserZoneResolver(func(context.Context) (string, error) { return current.Load().(string), nil })
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = m.Turns().Stop(ctx)
			})
			for i, zone := range []string{"UTC", "America/Lima", "Europe/Madrid"} {
				current.Store(zone)
				if deferred {
					if _, err := m.ReceiveDeferred(context.Background(), "client", fmt.Sprint(i), fmt.Sprint(i), "remind me tomorrow at 8 am", nil, nil); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, _, err := m.Receive(context.Background(), "client", fmt.Sprint(i), fmt.Sprint(i), "remind me tomorrow at 8 am"); err != nil {
						t.Fatal(err)
					}
				}
				select {
				case pre := <-observed:
					if len(pre.Temporal) != 1 || pre.Temporal[0].At.Location().String() != zone || pre.Temporal[0].At.Hour() != 8 {
						t.Fatalf("zone %s: %+v", zone, pre.Temporal)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("no intake")
				}
				rows := store.History(fmt.Sprint(i), 0)
				if len(rows) == 0 || rows[0].UserTimezone != zone {
					t.Fatalf("persisted zone %s: %+v", zone, rows)
				}
			}
		})
	}
}
