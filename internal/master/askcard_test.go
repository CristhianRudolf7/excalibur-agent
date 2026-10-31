package master

import (
	"testing"
	"time"
)

// Section 6.7: the ask-or-assume matrix.
func TestAskOrAssumeMatrix(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name     string
		doubt    Doubt
		ask      bool
		blocking bool
	}{
		{"mandatory data asks blocking", Doubt{Topic: "target directory", Mandatory: true, Default: "cwd"}, true, true},
		{"irreversible asks blocking", Doubt{Topic: "publish now", Irreversible: true, PrincipalProb: 0.95, Default: "later"}, true, true},
		{"ambiguous costly asks non-blocking", Doubt{Topic: "auth method", PrincipalProb: 0.5, ReworkCostly: true, Default: "oauth"}, true, false},
		{"clear reading assumes", Doubt{Topic: "format", PrincipalProb: 0.9, Default: "csv"}, false, false},
		{"trivial assumes", Doubt{Topic: "wording", Default: "short"}, false, false},
	}
	for _, c := range cases {
		d := AskOrAssume([]Doubt{c.doubt}, now)
		if c.ask && len(d.Questions) != 1 {
			t.Fatalf("%s: want a question, got %+v", c.name, d)
		}
		if c.ask && d.Questions[0].Blocking != c.blocking {
			t.Fatalf("%s: blocking=%v, want %v", c.name, d.Questions[0].Blocking, c.blocking)
		}
		if !c.ask && (len(d.Questions) != 0 || len(d.Assumptions) != 1) {
			t.Fatalf("%s: want one assumption, got %+v", c.name, d)
		}
	}
}

func TestQuestionBudgetAndTimeouts(t *testing.T) {
	now := time.Now()
	var doubts []Doubt
	for i := 0; i < 5; i++ {
		doubts = append(doubts, Doubt{Topic: "d", Mandatory: true, Default: "x"})
	}
	d := AskOrAssume(doubts, now)
	if len(d.Questions) != MaxQuestionsPerMessage {
		t.Fatalf("questions = %d, want at most %d", len(d.Questions), MaxQuestionsPerMessage)
	}
	if len(d.Assumptions) == 0 {
		t.Fatal("overflow doubts must become assumptions, visibly")
	}
	// Non-blocking takes its default after 15 min idle; blocking never.
	nb := Question{Blocking: false, AskedAt: now.Add(-16 * time.Minute)}
	if !DueDefault(nb, now, false) {
		t.Fatal("idle non-blocking question must take its default")
	}
	if DueDefault(nb, now, true) {
		t.Fatal("a run with other work must not take the default early")
	}
	b := Question{Blocking: true, AskedAt: now.Add(-16 * time.Minute)}
	if DueDefault(b, now.Add(48*time.Hour), false) {
		t.Fatal("blocking questions never take their default")
	}
	if !DueReminder(b, now.Add(25*time.Hour)) {
		t.Fatal("blocking waits need the 24h reminder")
	}
}

// Section 6.7: the card keeps the literal text with msg ids, and every
// requirement carries a criterion with a method.
func TestRequestCardInvariant(t *testing.T) {
	c := BuildCard("convert a to b", []string{"msg_1"}, "convert", nil, nil, nil, nil)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(c.Requirements) != 1 || len(c.Requirements[0].Criteria) != 1 {
		t.Fatalf("trivial card must carry the implicit requirement: %+v", c.Requirements)
	}
	bad := BuildCard("", nil, "", []CardRequirement{{Text: "x"}}, nil, nil, nil)
	if err := bad.Validate(); err == nil {
		t.Fatal("card without literal original must be rejected")
	}
	rf := c.ToTypes()
	if rf.Original != "convert a to b" || len(rf.OriginalMessageIDs) != 1 {
		t.Fatalf("types conversion lost the literal: %+v", rf)
	}
}
