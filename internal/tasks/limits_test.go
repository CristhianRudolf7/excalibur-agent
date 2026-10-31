// Limits tests (Section 18.4; verification row 18.5.4): reaching
// each limit (steps, tokens, cost, time, processes, sessions) and
// the tree budget trips closing mode then paused(budget) with an
// urgent notice; extending the budget resumes; a child never gets
// more than the parent remainder.
package tasks

import (
	"testing"
	"time"
)

func limitAll(n int, cost float64, secs int64) Limit {
	return Limit{
		MaxSteps: n, MaxTokens: 10 * n, MaxCostUSD: cost,
		MaxSeconds: secs, MaxToolCalls: 4 * n, MaxProcs: 2,
		MaxSessions: 2, MaxOutputTokens: 5 * n,
	}
}

func TestClosingThenExhausted(t *testing.T) {
	tr := &Tracker{Limit: limitAll(100, 10, 3600)}
	tr.Observe(Usage{Steps: 91}) // 9% left: closing mode
	if got := tr.Evaluate(); got != StatusClosing {
		t.Fatalf("9%% left must close, got %v", got)
	}
	tr.Observe(Usage{Steps: 9})
	if got := tr.Evaluate(); got != StatusExhausted {
		t.Fatalf("cap reached must exhaust, got %v", got)
	}
	if by := tr.ExhaustedBy(); len(by) != 1 || by[0] != "steps" {
		t.Fatalf("exhausted-by must name steps: %v", by)
	}
}

func TestEachLimitExhausts(t *testing.T) {
	cases := []struct {
		name string
		use  Usage
		want string
	}{
		{"tokens", Usage{InputTokens: 600, OutputTokens: 400}, "tokens"},
		{"cost", Usage{CostUSD: 10}, "cost"},
		{"time", Usage{Elapsed: time.Hour}, "time"},
		{"tool_calls", Usage{ToolCalls: 400}, "tool_calls"},
		{"processes", Usage{Procs: 2}, "processes"},
		{"sessions", Usage{Sessions: 2}, "sessions"},
	}
	for _, tc := range cases {
		tr := &Tracker{Limit: limitAll(100, 10, 3600)}
		tr.Observe(tc.use)
		if got := tr.Evaluate(); got != StatusExhausted {
			t.Fatalf("%s: must exhaust, got %v", tc.name, got)
		}
		found := false
		for _, b := range tr.ExhaustedBy() {
			if b == tc.want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: exhausted-by must name %q: %v", tc.name, tc.want, tr.ExhaustedBy())
		}
	}
}

func TestExtendBudgetResumes(t *testing.T) {
	tr := &Tracker{Limit: limitAll(100, 10, 3600)}
	tr.Observe(Usage{Steps: 100})
	if tr.Evaluate() != StatusExhausted {
		t.Fatal("must exhaust at the cap")
	}
	tr.Limit.MaxSteps = 200 // user extends the budget
	if tr.Evaluate() != StatusOK {
		t.Fatal("extended budget must resume where it stopped")
	}
}

func TestChildNeverExceedsParentRemainder(t *testing.T) {
	parent := limitAll(100, 10, 3600)
	used := Usage{Steps: 70, InputTokens: 200, OutputTokens: 100, CostUSD: 7}
	rest := RemainingBudget(parent, used)
	if rest.MaxSteps != 30 || rest.MaxCostUSD != 3 {
		t.Fatalf("remainder wrong: %+v", rest)
	}
	kids := Distribute(parent, used, []float64{1, 1})
	if kids[0].MaxSteps+kids[1].MaxSteps > rest.MaxSteps {
		t.Fatalf("shares exceed the remainder: %+v", kids)
	}
	for i, k := range kids {
		if k.MaxSteps > rest.MaxSteps || k.MaxCostUSD > rest.MaxCostUSD || k.MaxTokens > rest.MaxTokens {
			t.Fatalf("child %d exceeds the parent remainder: %+v", i, k)
		}
	}
}

func TestUserBudget(t *testing.T) {
	b := &UserBudget{DailyUSD: 5, MonthlyUSD: 100, SpentDay: 4.5, MaxRuns: 2, Runs: 2}
	if err := b.Check(1, 0); err == nil {
		t.Fatal("daily overspend must fail")
	}
	if err := b.Check(0, 1); err == nil {
		t.Fatal("run overflow must fail")
	}
	if err := b.Check(0.25, 0); err != nil {
		t.Fatalf("fitting spend must pass: %v", err)
	}
}

func TestProviderQuota(t *testing.T) {
	q := &ProviderQuota{PerMinute: 2, MaxPar: 2}
	now := time.Now()
	if !q.Take(now) {
		t.Fatal("quota must admit the first call")
	}
	if !q.Take(now) {
		t.Fatal("quota must admit the second call")
	}
	if q.Take(now) {
		t.Fatal("third call inside the minute must wait")
	}
	q.Release()
	q.Release()
	if q.Take(now.Add(61*time.Second)) != true {
		t.Fatal("quota must reset after a minute")
	}
}

func TestSystemLimits(t *testing.T) {
	s := SystemLimits{MaxRAMBytes: 1 << 30, MaxDiskBytes: 1 << 30}
	if got := s.Check(1<<20, 1<<20, 1<<20); len(got) != 0 {
		t.Fatalf("healthy system must pass: %v", got)
	}
	if got := s.Check(2<<30, 1<<20, 300<<20); len(got) != 2 {
		t.Fatalf("ram and WAL breach expected: %v", got)
	}
}
