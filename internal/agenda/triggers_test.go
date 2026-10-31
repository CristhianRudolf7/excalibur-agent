package agenda

import (
	"testing"
	"time"
)

// 21.10 row 3: duplicate events dedupe by id; bursts respect the rate limit.

func TestTriggerMatchAndFire(t *testing.T) {
	e := NewTriggerEngine()
	err := e.AddRule(&TriggerRule{ID: "mail", Filter: TriggerFilter{Types: []string{"mail"}, Contains: []string{"invoice"}},
		Action: ActionNotify, Target: "owner", MaxPerHour: 2, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	hits := e.Ingest(Event{ID: "ev1", Type: "mail", Source: "gmail", Payload: map[string]any{"subject": "your Invoice #3"}}, now)
	if len(hits) != 1 || hits[0].RuleID != "mail" {
		t.Fatalf("filter must match: %+v", hits)
	}
	// Same event id again: dedup, no hit.
	if hits := e.Ingest(Event{ID: "ev1", Type: "mail", Source: "gmail", Payload: map[string]any{"subject": "your Invoice #3"}}, now); len(hits) != 0 {
		t.Fatalf("duplicate event id must not refire: %+v", hits)
	}
	// Non-matching content: no hit.
	if hits := e.Ingest(Event{ID: "ev2", Type: "mail", Source: "gmail", Payload: map[string]any{"subject": "hello"}}, now); len(hits) != 0 {
		t.Fatalf("non-matching event must not fire: %+v", hits)
	}
}

func TestTriggerRateLimit(t *testing.T) {
	e := NewTriggerEngine()
	if err := e.AddRule(&TriggerRule{ID: "rl", Filter: TriggerFilter{Types: []string{"webhook"}},
		Action: ActionStartTask, Target: "t1", MaxPerHour: 2, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		if hits := e.Ingest(Event{ID: string(rune('a' + i)), Type: "webhook"}, now); len(hits) != 1 {
			t.Fatalf("burst under limit must fire: %+v", hits)
		}
	}
	if hits := e.Ingest(Event{ID: "c", Type: "webhook"}, now); len(hits) != 0 {
		t.Fatalf("over-limit burst must not fire: %+v", hits)
	}
	// An hour later the window reopens.
	if hits := e.Ingest(Event{ID: "d", Type: "webhook"}, now.Add(61*time.Minute)); len(hits) != 1 {
		t.Fatalf("window must reopen: %+v", hits)
	}
}

func TestTriggerDisabledAndUnknown(t *testing.T) {
	e := NewTriggerEngine()
	if err := e.AddRule(&TriggerRule{ID: "off", Action: ActionNotify, Target: "x"}); err != nil {
		t.Fatal(err)
	}
	if hits := e.Ingest(Event{ID: "e1", Type: "mail"}, time.Now().UTC()); len(hits) != 0 {
		t.Fatal("disabled rule must not fire")
	}
	if err := e.AddRule(&TriggerRule{ID: "bad", Action: "nope", Target: "x"}); err == nil {
		t.Fatal("unknown action must fail validation")
	}
	if err := e.RemoveRule("missing"); err == nil {
		t.Fatal("removing unknown rule must fail")
	}
}
