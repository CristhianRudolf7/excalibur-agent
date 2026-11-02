package daemon

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

func TestChangesHistoryAndEvent(t *testing.T) {
	ctx := context.Background()
	bus := telemetry.NewBus(nil)
	m := master.New(master.Config{}, nil, nil, nil, nil, nil, nil)
	b := NewDaemonBackend(m, nil, bus)
	h := workspace.NewHistory()
	b.AttachServices(BackendServices{History: h})
	h.Record("a", nil, []byte("new\n"), "child", "")
	h.Record("b", nil, []byte("unrelated"), "other", "")
	ev, err := m.SendAssistant(ctx, "conversation", "Done", []string{"master", "child"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Changes) != 1 || ev.Changes[0].Path != "a" {
		t.Fatalf("event: %+v", ev)
	}
	history, err := b.ConversationHistory(ctx, "conversation", "", 0)
	if err != nil || len(history) != 1 || !reflect.DeepEqual(ev.Changes, history[0].Changes) {
		t.Fatalf("history: %+v %v", history, err)
	}
	var found bool
	for _, event := range bus.Replay(types.Cursor{}) {
		if event.Type != types.EventMessageCreated {
			continue
		}
		var payload api.MessageEvent
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ev.Changes, payload.Changes) {
			t.Fatalf("payload: %+v", payload)
		}
		found = true
	}
	if !found {
		t.Fatal("missing message event")
	}
	*ev.Changes[0].Content = "mutated"
	ev.Changes[0].Hunks[0].Lines[0] = "+mutated"
	history[0].Changes[0].Hunks[0].Lines[0] = "+also mutated"
	got := m.History("conversation", 0)[0].Changes[0]
	if *got.Content != "new\n" || got.Hunks[0].Lines[0] != "+new" {
		t.Fatalf("mutable history: %+v", got)
	}
}
