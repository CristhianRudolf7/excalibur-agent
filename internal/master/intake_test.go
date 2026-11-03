package master

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func testLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	return loc
}

// Section 6.7: the deterministic corpus — commands, ids, paths, URLs,
// relative dates (including a DST fold), mentions with candidates.
func TestPreanalysisCorpus(t *testing.T) {
	lex := NewLexicon()
	lex.AddProject("gastos", "", "prj_gastos")
	lex.AddAgent("Lena", "agt_lena_1")
	lex.AddAgent("Lena", "agt_lena_2")
	lex.AddTask("T45", "export csv", "tsk_45")
	// DST fold: the US falls back on 2026-11-01 at 02:00; "tomorrow
	// at 9" from Oct 30 must resolve to Oct 31 09:00 EDT (UTC-4).
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	loc := testLoc(t)
	cases := []struct {
		text string
		want func(t *testing.T, p Preanalysis)
	}{
		{"/status lena", func(t *testing.T, p Preanalysis) {
			if !p.IsCommand || p.Command != "status" || p.CommandArgs != "lena" {
				t.Fatalf("slash not parsed: %+v", p)
			}
		}},
		{"pause T45 and change R4", func(t *testing.T, p Preanalysis) {
			if len(p.TaskRefs) != 1 || p.TaskRefs[0] != "T45" {
				t.Fatalf("task ref: %+v", p.TaskRefs)
			}
			if p.ControlIntent != "pause" {
				t.Fatalf("intent = %q, want pause", p.ControlIntent)
			}
		}},
		{"look at /tmp/a b/src/main.go and ./rel/file.txt", func(t *testing.T, p Preanalysis) {
			if len(p.Paths) < 3 {
				t.Fatalf("paths = %v, want 3", p.Paths)
			}
		}},
		{"see https://example.com/x?q=1 for details", func(t *testing.T, p Preanalysis) {
			if len(p.URLs) != 1 {
				t.Fatalf("urls = %v", p.URLs)
			}
		}},
		{"remind me tomorrow at 9", func(t *testing.T, p Preanalysis) {
			if len(p.Temporal) != 1 {
				t.Fatalf("temporal = %+v", p.Temporal)
			}
			at := p.Temporal[0].At
			if at.Day() != 31 || at.Month() != time.October || at.Hour() != 9 {
				t.Fatalf("tomorrow at 9 = %v, want Oct 31 09:00", at)
			}
			if _, off := at.Zone(); off != -4*3600 {
				t.Fatalf("offset = %d, want EDT (-14400) before the fold", off)
			}
		}},
		{"what is Lena doing", func(t *testing.T, p Preanalysis) {
			found := false
			for _, m := range p.Mentions {
				if m.Kind == MentionAgent && m.Ambiguous() && len(m.Candidates) == 2 {
					found = true
				}
			}
			if !found {
				t.Fatalf("ambiguous Lena must return both candidates: %+v", p.Mentions)
			}
		}},
		{"in gastos and tesis, update serde", func(t *testing.T, p Preanalysis) {
			if len(p.Mentions) == 0 {
				t.Fatalf("no project mentions: %+v", p.Mentions)
			}
		}},
		{"remember that I always use sqlite", func(t *testing.T, p Preanalysis) {
			if !p.HasMandate {
				t.Fatal("durable instruction not flagged")
			}
		}},
	}
	for _, c := range cases {
		p := Preanalyze(c.text, lex, now, loc)
		c.want(t, p)
	}
}

// Ordinary work defers classification to the master turn.
func TestOrdinaryWorkSkipsTriage(t *testing.T) {
	lex := NewLexicon()
	dec := &FakeDecider{Fn: func(text string, pre Preanalysis) Triage {
		return Triage{Type: TriageTask, Confidence: 0.9}
	}}
	model := &FakeModel{Available_: true}
	now := time.Now()
	in, err := Intake(context.Background(), Normalized{Text: "do the thing in /tmp/x"}, func(Normalized) (types.MessageID, error) {
		return types.NewMessageID(), nil
	}, lex, dec, now, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if in.Decided || dec.Calls != 0 || in.Triage.Type != TriageConversation {
		t.Fatalf("ordinary work must skip the decider: %+v, calls=%d", in, dec.Calls)
	}
	if model.Calls != 0 {
		t.Fatalf("intake must not call the model, calls=%d", model.Calls)
	}
}

func TestIntakeCandidateVocabulary(t *testing.T) {
	t.Setenv("EXCALIBUR_INTAKE_GATED_TRIAGE", "true")
	var candidates []string
	// Exercise every existing English and Spanish control/status spelling.
	for _, verb := range controlVerbs {
		for _, word := range verb.words {
			candidates = append(candidates, strings.TrimSpace(word)+" lena")
		}
	}
	candidates = append(candidates, statusWords...)
	candidates = append(candidates, "pausa T2", "¿cómo va R3?", "cancel everything", "cancela todo", "T12", "R3", "tsk_01ARZ3NDEKTSV4RRFFQ69G5FAV", "run_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	for _, text := range candidates {
		t.Run(text, func(t *testing.T) {
			dec := &FakeDecider{}
			in := fastIntake(text, nil, dec)
			if in.Pre.IsCommand {
				if in.Decided || dec.Calls != 0 {
					t.Fatalf("slash command classified: %+v", in)
				}
			} else if !in.Decided || dec.Calls != 1 {
				t.Fatalf("candidate skipped classification: %+v, calls=%d", in, dec.Calls)
			}
		})
	}
}

func TestIntakeGateSwitch(t *testing.T) {
	for _, value := range []string{"", "true", "false", "0", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("EXCALIBUR_INTAKE_GATED_TRIAGE", value)
			dec := &FakeDecider{}
			in := fastIntake("Hello", nil, dec)
			want := value == "false" || value == "0"
			if in.Decided != want || (dec.Calls == 1) != want {
				t.Fatalf("switch=%q: %+v calls=%d", value, in, dec.Calls)
			}
		})
	}
}

// Deliberately ignore context to verify that intake enforces its own bound.
type uncooperativeIntakeDecider struct {
	release  <-chan struct{}
	finished chan<- struct{}
}

func (d uncooperativeIntakeDecider) Classify(context.Context, string, Preanalysis) Triage {
	<-d.release
	close(d.finished)
	return Triage{Type: TriageQuickAction, Confidence: 1}
}

func TestIntakeTimeoutFallsBackWithoutLateControls(t *testing.T) {
	t.Setenv("EXCALIBUR_INTAKE_DECIDER_TIMEOUT", "20ms")
	release, finished := make(chan struct{}), make(chan struct{})
	dec := uncooperativeIntakeDecider{release, finished}
	start := time.Now()
	in := fastIntake("pause run_1", nil, dec)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("intake waited %s", elapsed)
	}
	if in.Decided || in.Triage.Type != TriageStatusQuery {
		t.Fatalf("timeout did not use deterministic fallback: %+v", in)
	}
	controls := newFakeControls()
	controls.seed("run_1", "worker", types.RunRunning)
	lane := &FastLane{Controls: controls, HasDecider: true}
	if out := lane.Handle(context.Background(), in); out.Handled {
		t.Fatalf("timeout must reach master: %+v", out)
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("late decider could not finish")
	}
	if len(controls.Paused) != 0 {
		t.Fatal("late result executed a control")
	}
}

func TestIntakeDeciderTimeoutSetting(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"", 2500 * time.Millisecond}, {"invalid", 2500 * time.Millisecond}, {"-1s", 2500 * time.Millisecond},
		{"0", 0}, {"20ms", 20 * time.Millisecond}, {"1s", time.Second},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("EXCALIBUR_INTAKE_DECIDER_TIMEOUT", tc.value)
			if got := intakeDeciderTimeout(); got != tc.want {
				t.Fatalf("timeout=%s want %s", got, tc.want)
			}
		})
	}
}

func TestIntakeCanceledContextSkipsDecider(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dec := &FakeDecider{}
	tri, decided := classifyIntake(ctx, dec, "pause R3", Preanalysis{ControlIntent: "pause", ControlTarget: "R3"})
	if decided || dec.Calls != 0 || tri.Type != TriageStatusQuery {
		t.Fatalf("canceled intake classified: %+v, calls=%d", tri, dec.Calls)
	}
}

func TestIntakeTimeoutDisabled(t *testing.T) {
	t.Setenv("EXCALIBUR_INTAKE_DECIDER_TIMEOUT", "0")
	dec := &FakeDecider{}
	in := fastIntake("pause R3", nil, dec)
	if !in.Decided || dec.Calls != 1 {
		t.Fatalf("disabled deadline: %+v, calls=%d", in, dec.Calls)
	}
}
