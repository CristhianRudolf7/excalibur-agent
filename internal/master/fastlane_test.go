package master

import (
	"context"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func fastIntake(text string, lex *Lexicon, dec Decider) IntakeResult {
	in, _ := Intake(context.Background(), Normalized{Text: text}, func(Normalized) (types.MessageID, error) {
		return types.NewMessageID(), nil
	}, lex, dec, time.Now(), time.UTC)
	return in
}

// Section 6.7: every fast-lane row answers with no model call, with
// and without a decider; ambiguous targets ask; mixed messages split.
func TestFastLaneTable(t *testing.T) {
	newLane := func(withDecider bool, fc *FakeControls) (*FastLane, *FakeDecider) {
		dec := &FakeDecider{Fn: func(text string, pre Preanalysis) Triage {
			if pre.ControlIntent != "" {
				return Triage{Type: TriageQuickAction, Confidence: 0.95}
			}
			return Triage{Type: TriageConversation, Confidence: 0.9}
		}}
		return &FastLane{Controls: fc, HasDecider: withDecider}, dec
	}
	_ = newLane

	t.Run("slash status without decider", func(t *testing.T) {
		fc := newFakeControls()
		fc.seed("run_1", "tool", types.RunRunning)
		lane := &FastLane{Controls: fc}
		in := fastIntake("/status run_1", nil, nil)
		out := lane.Handle(context.Background(), in)
		if !out.Handled || out.Snapshot == nil {
			t.Fatalf("slash status must answer: %+v", out)
		}
	})
	t.Run("natural language without decider goes to master", func(t *testing.T) {
		fc := newFakeControls()
		lane := &FastLane{Controls: fc}
		in := fastIntake("pause lena", nil, nil)
		if out := lane.Handle(context.Background(), in); out.Handled {
			t.Fatalf("no-decider NL must reach the master: %+v", out)
		}
	})
	t.Run("status with resolved target", func(t *testing.T) {
		fc := newFakeControls()
		fc.seed("run_1", "lena", types.RunRunning)
		lane, _ := newLane(true, fc)
		lex := NewLexicon()
		lex.AddAgent("lena", "lena")
		dec := &FakeDecider{Fn: func(string, Preanalysis) Triage {
			return Triage{Type: TriageStatusQuery, Confidence: 0.95}
		}}
		lane.HasDecider = true
		in := fastIntake("what is lena doing", lex, dec)
		out := lane.Handle(context.Background(), in)
		if !out.Handled || out.Snapshot == nil {
			t.Fatalf("resolved status must answer: %+v", out)
		}
	})
	t.Run("ambiguous target asks", func(t *testing.T) {
		fc := newFakeControls()
		fc.Runs["r1"] = api.RunStatusResult{RunID: "r1", State: types.RunRunning, ToolInFlight: "cargo test"}
		fc.Runs["r2"] = api.RunStatusResult{RunID: "r2", State: types.RunRunning, ToolInFlight: "web_read"}
		fc.ByName["lena"] = []string{"r1", "r2"}
		lex := NewLexicon()
		lex.AddAgent("lena", "lena")
		dec := &FakeDecider{Fn: func(string, Preanalysis) Triage {
			return Triage{Type: TriageStatusQuery, Confidence: 0.95}
		}}
		lane := &FastLane{Controls: fc, HasDecider: true}
		in := fastIntake("what is lena doing", lex, dec)
		// Force the mention path: lexicon resolves "lena" to one id,
		// so register the ambiguity at the controls level instead.
		in.Pre.Mentions = []Mention{{Text: "lena", Kind: MentionAgent, Candidates: []string{"lena"}}}
		out := lane.Handle(context.Background(), in)
		if !out.Handled || out.Snapshot != nil {
			t.Fatalf("ambiguous target must ask, not guess: %+v", out)
		}
	})
	t.Run("mixed control plus content", func(t *testing.T) {
		fc := newFakeControls()
		fc.seed("run_1", "exporter", types.RunRunning)
		lex := NewLexicon()
		dec := &FakeDecider{Fn: func(string, Preanalysis) Triage {
			return Triage{Type: TriageQuickAction, Confidence: 0.95}
		}}
		lane := &FastLane{Controls: fc, HasDecider: true}
		in := fastIntake("pause run_1 and change the requirement", lex, dec)
		in.Pre.ControlIntent = "pause"
		in.Pre.ControlTarget = "run_1 and change the requirement"
		out := lane.Handle(context.Background(), in)
		if !out.Handled || out.Rest == "" {
			t.Fatalf("mixed message must run control and return rest: %+v", out)
		}
		if len(fc.Paused) != 1 {
			t.Fatalf("control did not run: %+v", fc.Paused)
		}
	})
	t.Run("stop halts the turn only", func(t *testing.T) {
		fc := newFakeControls()
		lane := &FastLane{Controls: fc}
		in := fastIntake("/stop", nil, nil)
		out := lane.Handle(context.Background(), in)
		if !out.Handled {
			t.Fatalf("/stop must be handled: %+v", out)
		}
		if len(fc.Paused) != 0 {
			t.Fatal("/stop must not pause background work")
		}
	})
}

func TestLooksLikeStatusQuery(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"Sí, créala.", false},
		{"ok dale", false},
		{"¿cómo va?", true},
		{"what is the status", true},
		{"qué está corriendo", true},
		{"Usa la herramienta robot: mira la cámara y dime el estado del brazo. No muevas nada.", false},
		{"dime el estado de las tareas que están corriendo ahora mismo por favor gracias", true},
	} {
		if got := looksLikeStatusQuery(tc.in); got != tc.want {
			t.Errorf("looksLikeStatusQuery(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
