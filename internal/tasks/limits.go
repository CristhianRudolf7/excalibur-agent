// Limits and budgets (Section 18.4).
//
//	Execution: steps, input and output tokens, USD cost, wall time,
//	tool calls, simultaneous processes, terminal sessions.
//	Task tree: summed budgets, distributed by the parent.
//	User: daily and monthly budget; simultaneous runs.
//	Provider: per-minute quotas and concurrency (5.2).
//	System: RAM, disk, WAL size.
//
// Near a limit the run enters closing mode (7.3); on reaching it the
// run pauses with the budget motive, emits its report with done and
// pending, and the notice is urgent (5.4). The user may extend the
// budget and the run continues where it stopped; otherwise the task
// stays paused until canceled or resumed.
//
// The tracker is pure counters: the engine feeds usage, and the
// tracker answers ok, closing, or exhausted. No goroutines, no model
// calls.
package tasks

import (
	"fmt"
	"time"
)

// ClosingFraction enters closing mode below this remainder of steps
// or budget (Sections 7.3, 18.4, Appendix B: 10%).
const ClosingFraction = 0.10

// Usage is the consumed side of a budget.
type Usage struct {
	Steps        int
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Elapsed      time.Duration
	ToolCalls    int
	Procs        int
	Sessions     int
}

// Limit extends the shared budget with the execution-level caps of
// Section 18.4 the shared Budget struct does not carry.
type Limit struct {
	MaxSteps        int
	MaxTokens       int
	MaxCostUSD      float64
	MaxSeconds      int64
	MaxToolCalls    int
	MaxProcs        int
	MaxSessions     int
	MaxOutputTokens int
}

// Status is the tracker verdict.
type Status int

// Tracker verdicts.
const (
	StatusOK Status = iota
	StatusClosing
	StatusExhausted
)

// Tracker watches one execution against its limit.
type Tracker struct {
	Limit Limit
	Used  Usage
}

// Observe adds one step of usage.
func (t *Tracker) Observe(u Usage) {
	t.Used.Steps += u.Steps
	t.Used.InputTokens += u.InputTokens
	t.Used.OutputTokens += u.OutputTokens
	t.Used.CostUSD += u.CostUSD
	t.Used.Elapsed += u.Elapsed
	t.Used.ToolCalls += u.ToolCalls
	if u.Procs > t.Used.Procs {
		t.Used.Procs = u.Procs
	}
	if u.Sessions > t.Used.Sessions {
		t.Used.Sessions = u.Sessions
	}
}

// Evaluate answers ok, closing (below 10% of steps or budget left),
// or exhausted (any cap reached).
func (t *Tracker) Evaluate() Status {
	if t.exhausted() {
		return StatusExhausted
	}
	if t.closing() {
		return StatusClosing
	}
	return StatusOK
}

func (t *Tracker) exhausted() bool {
	l := t.Limit
	switch {
	case l.MaxSteps > 0 && t.Used.Steps >= l.MaxSteps:
	case l.MaxTokens > 0 && t.Used.InputTokens+t.Used.OutputTokens >= l.MaxTokens:
	case l.MaxOutputTokens > 0 && t.Used.OutputTokens >= l.MaxOutputTokens:
	case l.MaxCostUSD > 0 && t.Used.CostUSD >= l.MaxCostUSD:
	case l.MaxSeconds > 0 && t.Used.Elapsed >= time.Duration(l.MaxSeconds)*time.Second:
	case l.MaxToolCalls > 0 && t.Used.ToolCalls >= l.MaxToolCalls:
	case l.MaxProcs > 0 && t.Used.Procs >= l.MaxProcs:
	case l.MaxSessions > 0 && t.Used.Sessions >= l.MaxSessions:
	default:
		return false
	}
	return true
}

func (t *Tracker) closing() bool {
	l := t.Limit
	if l.MaxSteps > 0 && float64(l.MaxSteps-t.Used.Steps) < ClosingFraction*float64(l.MaxSteps) {
		return true
	}
	if l.MaxCostUSD > 0 && (l.MaxCostUSD-t.Used.CostUSD) < ClosingFraction*l.MaxCostUSD {
		return true
	}
	if l.MaxTokens > 0 {
		left := l.MaxTokens - t.Used.InputTokens - t.Used.OutputTokens
		if float64(left) < ClosingFraction*float64(l.MaxTokens) {
			return true
		}
	}
	if l.MaxSeconds > 0 {
		left := time.Duration(l.MaxSeconds)*time.Second - t.Used.Elapsed
		if left < time.Duration(ClosingFraction*float64(l.MaxSeconds)*float64(time.Second)) {
			return true
		}
	}
	return false
}

// ExhaustedBy names the caps that are exhausted, for the urgent
// notice and the paused(budget) report.
func (t *Tracker) ExhaustedBy() []string {
	var out []string
	l := t.Limit
	if l.MaxSteps > 0 && t.Used.Steps >= l.MaxSteps {
		out = append(out, "steps")
	}
	if l.MaxTokens > 0 && t.Used.InputTokens+t.Used.OutputTokens >= l.MaxTokens {
		out = append(out, "tokens")
	}
	if l.MaxOutputTokens > 0 && t.Used.OutputTokens >= l.MaxOutputTokens {
		out = append(out, "output_tokens")
	}
	if l.MaxCostUSD > 0 && t.Used.CostUSD >= l.MaxCostUSD {
		out = append(out, "cost")
	}
	if l.MaxSeconds > 0 && t.Used.Elapsed >= time.Duration(l.MaxSeconds)*time.Second {
		out = append(out, "time")
	}
	if l.MaxToolCalls > 0 && t.Used.ToolCalls >= l.MaxToolCalls {
		out = append(out, "tool_calls")
	}
	if l.MaxProcs > 0 && t.Used.Procs >= l.MaxProcs {
		out = append(out, "processes")
	}
	if l.MaxSessions > 0 && t.Used.Sessions >= l.MaxSessions {
		out = append(out, "sessions")
	}
	return out
}

// RemainingBudget caps what a child may receive: a child never gets
// more than what is left to the parent (Section 18.4).
func RemainingBudget(l Limit, used Usage) Limit {
	sub := func(max, u int) int {
		if max <= 0 {
			return 0
		}
		if left := max - u; left > 0 {
			return left
		}
		return 0
	}
	out := Limit{
		MaxSteps:        sub(l.MaxSteps, used.Steps),
		MaxTokens:       sub(l.MaxTokens, used.InputTokens+used.OutputTokens),
		MaxToolCalls:    sub(l.MaxToolCalls, used.ToolCalls),
		MaxProcs:        l.MaxProcs,
		MaxSessions:     l.MaxSessions,
		MaxOutputTokens: sub(l.MaxOutputTokens, used.OutputTokens),
	}
	out.MaxSeconds = l.MaxSeconds - int64(used.Elapsed/time.Second)
	if out.MaxSeconds < 0 {
		out.MaxSeconds = 0
	}
	out.MaxCostUSD = l.MaxCostUSD - used.CostUSD
	if out.MaxCostUSD < 0 {
		out.MaxCostUSD = 0
	}
	return out
}

// Distribute splits a parent remainder across children: every child
// share is capped by what is left, and the shares never exceed the
// remainder on steps, tokens or cost.
func Distribute(parent Limit, used Usage, shares []float64) []Limit {
	rest := RemainingBudget(parent, used)
	total := 0.0
	for _, s := range shares {
		total += s
	}
	out := make([]Limit, len(shares))
	for i, s := range shares {
		f := 0.0
		if total > 0 {
			f = s / total
		}
		out[i] = Limit{
			MaxSteps:        int(f * float64(rest.MaxSteps)),
			MaxTokens:       int(f * float64(rest.MaxTokens)),
			MaxCostUSD:      f * rest.MaxCostUSD,
			MaxSeconds:      int64(f * float64(rest.MaxSeconds)),
			MaxToolCalls:    int(f * float64(rest.MaxToolCalls)),
			MaxProcs:        rest.MaxProcs,
			MaxSessions:     rest.MaxSessions,
			MaxOutputTokens: int(f * float64(rest.MaxOutputTokens)),
		}
	}
	return out
}

// UserBudget watches the daily and monthly spend plus the simulta-
// neous runs of Section 18.4.
type UserBudget struct {
	DailyUSD   float64
	MonthlyUSD float64
	SpentDay   float64
	SpentMonth float64
	MaxRuns    int
	Runs       int
}

// Check reports whether a new spend or run fits the user budget.
func (b *UserBudget) Check(cost float64, runs int) error {
	if b.DailyUSD > 0 && b.SpentDay+cost > b.DailyUSD {
		return fmt.Errorf("tasks: daily budget %.2f USD exhausted", b.DailyUSD)
	}
	if b.MonthlyUSD > 0 && b.SpentMonth+cost > b.MonthlyUSD {
		return fmt.Errorf("tasks: monthly budget %.2f USD exhausted", b.MonthlyUSD)
	}
	if b.MaxRuns > 0 && b.Runs+runs > b.MaxRuns {
		return fmt.Errorf("tasks: too many simultaneous runs (%d/%d)", b.Runs+runs, b.MaxRuns)
	}
	return nil
}

// ProviderQuota watches per-minute calls and concurrency (5.2).
type ProviderQuota struct {
	PerMinute int
	MaxPar    int
	used      []time.Time
	par       int
}

// Take records one call at now; it reports false when the
// per-minute quota or the concurrency cap is full.
func (q *ProviderQuota) Take(now time.Time) bool {
	cut := now.Add(-time.Minute)
	kept := q.used[:0]
	for _, t := range q.used {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	q.used = kept
	if q.PerMinute > 0 && len(q.used) >= q.PerMinute {
		return false
	}
	if q.MaxPar > 0 && q.par >= q.MaxPar {
		return false
	}
	q.used = append(q.used, now)
	q.par++
	return true
}

// Release frees one concurrency slot.
func (q *ProviderQuota) Release() {
	if q.par > 0 {
		q.par--
	}
}

// SystemLimits watches RAM, disk and WAL size (Section 18.4). The
// WAL alert threshold defaults to 256 MiB (Section 4.3).
type SystemLimits struct {
	MaxRAMBytes  int64
	MaxDiskBytes int64
	MaxWALBytes  int64
}

// Check reports which system cap the current readings breach.
func (s SystemLimits) Check(ram, disk, wal int64) []string {
	maxWAL := s.MaxWALBytes
	if maxWAL == 0 {
		maxWAL = 256 << 20
	}
	var out []string
	if s.MaxRAMBytes > 0 && ram > s.MaxRAMBytes {
		out = append(out, "ram")
	}
	if s.MaxDiskBytes > 0 && disk > s.MaxDiskBytes {
		out = append(out, "disk")
	}
	if wal > maxWAL {
		out = append(out, "wal")
	}
	return out
}
