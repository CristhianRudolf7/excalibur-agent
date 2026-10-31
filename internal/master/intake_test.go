package master

import (
	"context"
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

// Section 6.7: triage never adds an LLM call. Intake uses the decider
// (or defers to the master turn) and the fast lane calls no model.
func TestTriageAddsNoLLMCall(t *testing.T) {
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
	if !in.Decided || dec.Calls != 1 {
		t.Fatalf("decider must run exactly once, calls=%d", dec.Calls)
	}
	if model.Calls != 0 {
		t.Fatalf("intake must not call the model, calls=%d", model.Calls)
	}
}
