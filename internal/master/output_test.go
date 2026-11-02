package master

import (
	"context"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

func TestMasterTurnPublishesPersistedAnswer(t *testing.T) {
	model := &FakeModel{Available_: true, Fn: func(context.Context, string) (string, error) { return "A documented claim.", nil }}
	m := New(Config{}, NewMemStore(), nil, nil, nil, model, nil)
	published := make(chan api.MessageEvent, 1)
	m.SetAssistantOutput(AssistantOutput{Publish: func(ev api.MessageEvent) { published <- ev }})
	m.Turns().Deliver("cnv_answer", IntakeResult{Msg: Normalized{Text: "question"}})
	select {
	case ev := <-published:
		history := m.History("cnv_answer", 10)
		if len(history) != 1 || history[0].Role != "assistant" || history[0].Text != ev.Text || history[0].ID != ev.MessageID {
			t.Fatalf("history %+v event %+v", history, ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not deliver answer")
	}
}
func TestCanceledAssistantIsNotPublished(t *testing.T) {
	m := New(Config{}, nil, nil, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.SendAssistant(ctx, "cnv", "text", nil); err == nil {
		t.Fatal("canceled send succeeded")
	}
	if len(m.History("cnv", 0)) != 0 {
		t.Fatal("persisted canceled message")
	}
}

func TestAssistantOutputCancellationDuringValidation(t *testing.T) {
	m := New(Config{}, nil, nil, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	m.SetAssistantOutput(AssistantOutput{Process: func(context.Context, string, []string) (string, []api.SourceRef, []string) {
		cancel()
		return "late", nil, nil
	}})
	if _, err := m.SendAssistant(ctx, "cnv", "text", nil); err == nil {
		t.Fatal("persisted an answer canceled during validation")
	}
	if len(m.History("cnv", 0)) != 0 {
		t.Fatal("canceled answer is in history")
	}
}
func TestAssistantSourcesAreImmutableInHistory(t *testing.T) {
	m := New(Config{}, nil, nil, nil, nil, nil, nil)
	m.SetAssistantOutput(AssistantOutput{Process: func(context.Context, string, []string) (string, []api.SourceRef, []string) {
		return "claim [1]", []api.SourceRef{{N: 1, Kind: "document", Title: "Original"}}, nil
	}})
	ev, err := m.SendAssistant(context.Background(), "cnv", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	ev.Sources[0].Title = "mutated event"
	history := m.History("cnv", 0)
	if history[0].Sources[0].Title != "Original" {
		t.Fatal("event mutation changed history")
	}
	history[0].Sources[0].Title = "mutated history"
	if m.History("cnv", 0)[0].Sources[0].Title != "Original" {
		t.Fatal("history mutation changed store")
	}
}
