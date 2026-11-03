package telemetry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestEphemeralNeverStoredOrReplayed(t *testing.T) {
	sink, err := OpenSink(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	bus := NewBus(sink)
	ordinary := bus.Subscribe(1)
	calls := 0
	unsubscribe := bus.SubscribeEphemeral(func(e types.Event) {
		calls++
		if e.Seq != 0 || e.Type != "robotlab.frame" {
			t.Error("ephemeral envelope")
		}
	})
	if err := bus.PublishEphemeral("robotlab.frame", map[string]any{"session": "s"}); err != nil {
		t.Fatal(err)
	}
	unsubscribe()
	_ = bus.PublishEphemeral("robotlab.frame", nil)
	if calls != 1 || bus.Len() != 0 || len(bus.Replay(types.Cursor{})) != 0 {
		t.Fatal("ephemeral event retained")
	}
	select {
	case <-ordinary:
		t.Fatal("ephemeral reached ordinary queue")
	default:
	}
	raw, err := os.ReadFile(filepath.Join(sink.Dir(), "events.jsonl"))
	if err != nil || len(raw) != 0 {
		t.Fatalf("frame persisted: %q %v", raw, err)
	}
	ev, err := bus.Publish("robotlab.focus", nil, nil, map[string]any{"view": "scene"})
	if err != nil || ev.Seq != 1 {
		t.Fatalf("sequence: %+v %v", ev, err)
	}
}
func TestLiveSubscriptionReleased(t *testing.T) {
	bus := NewBus(nil)
	_, unsubscribe := bus.SubscribeLive(2)
	if len(bus.subs) != 1 {
		t.Fatal("subscriber missing")
	}
	unsubscribe()
	unsubscribe()
	if len(bus.subs) != 0 {
		t.Fatal("subscriber leaked")
	}
}
