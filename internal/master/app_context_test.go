package master

import (
	"context"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

type contextTurnRecorder chan Normalized

func (r contextTurnRecorder) CompleteTurn(_ context.Context, _ string, msg Normalized) (string, []string, error) {
	r <- msg
	return "Answer", nil, nil
}

func TestDeferredIntakePreservesAppContext(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "mixed control"}[mixed], func(t *testing.T) {
			release := make(chan struct{})
			controls := newFakeControls()
			controls.seed("run_1", "teacher", "running")
			decider := &FakeDecider{Fn: func(string, Preanalysis) Triage {
				<-release
				return Triage{Type: TriageQuickAction, Confidence: 0.95}
			}}
			m := New(Config{}, NewMemStore(), NewLexicon(), decider, controls, nil, nil)
			seen := make(contextTurnRecorder, 1)
			m.SetTurnCaller(seen)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = m.Turns().Stop(ctx)
			})
			app := api.AppContext{App: "classroom", CourseID: "course", TopicID: "topic"}
			attachments := []string{"document"}
			text, want := "Explain this", "Explain this"
			if mixed {
				text, want = "pause run_1 and explain this", "explain this"
			}
			replies := make(chan string, 1)
			id, err := m.ReceiveDeferred(api.WithAppContext(context.Background(), app), "client", "message", "conversation", text, attachments, func(reply string) { replies <- reply })
			if err != nil || id == "" {
				t.Fatal(id, err)
			}
			attachments[0] = "changed"
			close(release)
			select {
			case got := <-seen:
				if got.Context == nil || *got.Context != app || got.Text != want || len(got.Attachments) != 1 || got.Attachments[0] != "document" {
					t.Fatalf("deferred turn lost metadata: %+v", got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("deferred turn did not run")
			}
			if mixed {
				select {
				case reply := <-replies:
					if reply == "" {
						t.Fatal("missing fast-lane reply")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("fast-lane reply was not delivered")
				}
			}
		})
	}
}

func TestDeferredIntakeRejectsInvalidContextBeforePersistence(t *testing.T) {
	m := New(Config{}, NewMemStore(), nil, nil, nil, nil, nil)
	_, err := m.ReceiveDeferredWithContext(context.Background(), "client", "message", "conversation", "Explain this", nil, &api.AppContext{App: ""}, nil)
	if err == nil || len(m.TurnHistory("conversation", 0)) != 0 {
		t.Fatal("invalid app context was persisted", err)
	}
}

func TestMessageAppContextDoesNotAliasCallerOrHistory(t *testing.T) {
	store := NewMemStore()
	app := &api.AppContext{App: "classroom", CourseID: "course"}
	if _, err := store.SaveMessage(context.Background(), "conversation", Normalized{Text: "Explain", Context: app}); err != nil {
		t.Fatal(err)
	}
	app.CourseID = "changed"
	history := store.History("conversation", 0)
	if history[0].Context.CourseID != "course" {
		t.Fatal("caller changed stored context")
	}
	history[0].Context.CourseID = "changed again"
	if store.History("conversation", 0)[0].Context.CourseID != "course" {
		t.Fatal("history changed stored context")
	}
}
