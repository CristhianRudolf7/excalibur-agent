package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	toolfake "github.com/CristhianRudolf7/excalibur-agent/internal/tools/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestArtifactEventsAtTurnEnd(t *testing.T) {
	for _, mode := range []string{"success", "failure", "tool-error", "cancel", "empty"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			var b *DaemonBackend
			b, s := assistantTestWiring(t, false, func(_ context.Context, _ providers.ChatRequest) (providers.ChatResponse, error) {
				calls++
				if calls == 1 {
					return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "render-call", Name: "clock", Arguments: json.RawMessage(`{}`)}}}}, nil
				}
				events := b.Bus.Replay(types.Cursor{})
				count := 0
				for _, ev := range events {
					if ev.Type == api.EventArtifactReady {
						t.Error("ready before final model response")
					}
					if ev.Type == api.EventArtifactCreated {
						count++
					}
				}
				want := 1
				if mode == "empty" {
					want = 0
				}
				if count != want {
					t.Errorf("created during turn=%d want=%d", count, want)
				}
				if mode == "failure" {
					return providers.ChatResponse{}, errors.New("simulated model failure")
				}
				if mode == "cancel" {
					cancel()
					return providers.ChatResponse{}, ctx.Err()
				}
				return textReply("The requested architecture diagram is ready for review."), nil
			})
			fake := toolfake.New("clock")
			result := tools.Result{Status: tools.StatusOK, Output: "rendered"}
			if mode == "tool-error" {
				result.Status = tools.StatusToolError
			}
			if mode != "empty" {
				ref := artifactFixture(t, "architecture")
				result.Artifacts = []tools.ArtifactRef{ref, ref}
			}
			fake.Results = []tools.Result{result}
			s.Tools.Unregister("clock")
			if err := s.Tools.Register(fake); err != nil {
				t.Fatal(err)
			}
			a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
			_, _, err := a.CompleteTurn(ctx, "conversation", master.Normalized{Text: "Create an architecture diagram"})
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error=%v", err)
			}
			created, ready := 0, 0
			for _, ev := range b.Bus.Replay(types.Cursor{}) {
				switch ev.Type {
				case api.EventArtifactCreated:
					created++
					var p api.ArtifactCreatedEvent
					if err := json.Unmarshal(ev.Payload, &p); err != nil {
						t.Fatal(err)
					}
					if p.ConversationID != "conversation" || p.RunID == "" || p.Artifact.ToolCallID != "render-call" || ev.RunID == nil || string(*ev.RunID) != p.RunID {
						t.Fatal(p)
					}
				case api.EventArtifactReady:
					ready++
					var p api.ArtifactReadyEvent
					if err := json.Unmarshal(ev.Payload, &p); err != nil {
						t.Fatal(err)
					}
					if len(p.Artifacts) != 1 || p.Artifacts[0].ToolCallID != "render-call" || p.ConversationID != "conversation" {
						t.Fatal(p)
					}
					// A reconstructed collector cannot announce a second ready event.
					turn := &artifactTurn{store: &artifactStore{journal: s.Jrnl, bus: b.Bus}, conversation: p.ConversationID, run: p.RunID}
					if err := turn.finish(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
			}
			want := 1
			if mode == "empty" {
				want = 0
			}
			if created != want || ready != want {
				t.Fatalf("created=%d ready=%d want=%d", created, ready, want)
			}
			count := 0
			for _, ev := range b.Bus.Replay(types.Cursor{}) {
				if ev.Type == api.EventArtifactReady {
					count++
				}
			}
			if count != want {
				t.Fatalf("duplicate ready=%d", count)
			}
		})
	}
}

func TestArtifactReadyWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &artifactStore{journal: journal.NewMemStore(), bus: telemetry.NewBus(nil)}
	turn := &artifactTurn{store: s, conversation: "conv", run: "run"}
	cancel()
	if err := turn.collect(ctx, "call", []tools.ArtifactRef{artifactFixture(t, "partial")}); err != nil {
		t.Fatal(err)
	}
	if err := turn.finish(ctx); err != nil {
		t.Fatal(err)
	}
	events := s.bus.Replay(types.Cursor{})
	if len(events) != 2 || events[0].Type != api.EventArtifactCreated || events[1].Type != api.EventArtifactReady {
		t.Fatal(events)
	}
}
