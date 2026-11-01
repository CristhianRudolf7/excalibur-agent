// Long-horizon persistence hooks for the step loop (Sections 7.3,
// 18.4, P1-P10).
//
// The step loop owns the finish path; the deterministic rules live in
// internal/tasks (longhorizon.go) so they stay testable without a
// model. This file is the seam: it keeps the explicit milestone plan in
// task state, renders the budget-pacing line for L5, nudges long
// commands to background+poll, and continues the run with a concise
// "what is still unproven" message when the agent tries to finish while
// significant budget remains and executable evidence is still thin.
//
// Nothing here changes the provider wire protocol, the tool surface or
// the bench invocation: prompts travel as volatile notes (L5) and the
// milestone plan rides the pinned working notes (L3), so the stable
// L0-L2 prefix never churns.
package engine

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// longHorizonConfig resolves the effective switches: the driver
// override wins, otherwise the environment-backed defaults apply so
// every mechanism can be ablated without a rebuild.
func (e *Engine) longHorizonConfig() tasks.LongHorizonConfig {
	if e.deps.LongHorizon != nil {
		return *e.deps.LongHorizon
	}
	return tasks.DefaultLongHorizonConfig()
}

// ensureLHMilestones builds the explicit milestone plan once per run
// when the task estimates as large. It pins the plan to the working
// notes so small models do not lose it as history grows, and injects
// one initial note naming the next milestone. Small tasks stay
// untouched: no plan, no notes, no bytes.
func (e *Engine) ensureLHMilestones(run *Run) {
	cfg := e.longHorizonConfig()
	if !cfg.Enable || !cfg.EnableMilestones {
		return
	}
	run.mu.Lock()
	if run.lhInit {
		run.mu.Unlock()
		return
	}
	request := run.Request
	run.mu.Unlock()
	if strings.TrimSpace(request) == "" {
		return
	}
	drafts := tasks.ExtractDraftCriteria(request, 6)
	if !tasks.EstimateLarge(cfg, request, len(drafts)) {
		run.mu.Lock()
		run.lhInit = true
		run.mu.Unlock()
		return
	}
	plan := tasks.BuildMilestones(request, drafts)
	run.mu.Lock()
	run.lhInit = true
	run.lhLarge = true
	run.lhMilestones = plan
	run.mu.Unlock()
	run.addFact("milestone plan ("+strconv.Itoa(len(plan))+" phases): "+lhPlanSummary(plan), 24)
	if next := tasks.NextPending(plan, lhHistoryView(run)); next != nil {
		run.InjectNote("large task: work the milestone plan in order; next milestone " + next.ID + ": " + next.Title + ". Verify each milestone with an executable check.")
	}
}

func lhPlanSummary(plan []tasks.LHMilestone) string {
	parts := make([]string, 0, len(plan))
	for _, m := range plan {
		parts = append(parts, m.ID+" "+truncateLH(m.Title, 60))
	}
	return strings.Join(parts, "; ")
}

// lhHistoryView maps the run history to the verification view for
// milestone checks.
func lhHistoryView(run *Run) []tasks.HistoryTool {
	return historyTools(run.historyCopy())
}

// LongHorizonPacing is the exported pacing line for drivers that own
// no Engine handle (embedded exec/eval): it applies the
// environment-backed defaults so the line can be measured on and off
// without a rebuild. Empty when the master or pacing switch is off.
func (r *Run) LongHorizonPacing(now time.Time) string {
	cfg := tasks.DefaultLongHorizonConfig()
	if !cfg.Enable || !cfg.EnablePacing {
		return ""
	}
	return r.pacingWith(cfg, now)
}

// pacingWith formats the pacing line under an explicit config. The
// caller supplies the resolved switches (driver override or env
// defaults); the run supplies budgets, clocks and milestones.
func (r *Run) pacingWith(cfg tasks.LongHorizonConfig, now time.Time) string {
	r.mu.Lock()
	used := r.Tracker.Used
	limit := r.Tracker.Limit
	started := r.StartedAt
	plan := append([]tasks.LHMilestone(nil), r.lhMilestones...)
	large := r.lhLarge
	r.mu.Unlock()
	elapsed := now.Sub(started)
	if elapsed < 0 {
		elapsed = 0
	}
	msDone, msTotal := 0, 0
	if large && len(plan) > 0 {
		hist := historyTools(r.historyCopy())
		msDone, msTotal = tasks.MilestoneProgress(plan, hist)
	}
	return tasks.FormatPacing(elapsed, limit.MaxSeconds, used.Steps, limit.MaxSteps, msDone, msTotal)
}

// lhProgressCheck injects the "next milestone" nudge every N steps on
// large tasks. It fires only when a milestone is still pending so
// finished plans stay silent.
func (e *Engine) lhProgressCheck(run *Run) {
	cfg := e.longHorizonConfig()
	if !cfg.Enable || !cfg.EnableMilestones {
		return
	}
	every := cfg.MilestoneEvery
	if every < 1 {
		every = 8
	}
	run.mu.Lock()
	if !run.lhLarge || len(run.lhMilestones) == 0 {
		run.mu.Unlock()
		return
	}
	steps := run.Steps
	plan := append([]tasks.LHMilestone(nil), run.lhMilestones...)
	run.mu.Unlock()
	if steps%every != 0 {
		return
	}
	hist := historyTools(run.historyCopy())
	note := tasks.NextMilestoneNote(plan, hist)
	if note == "" {
		return
	}
	run.InjectNote(note)
}

// lhShouldContinue decides the time- and evidence-aware continuation:
// when significant budget remains and executable evidence is still
// thin (fresh-verification gaps, exercise gaps, or the deliverable
// never exercised end to end), the run continues with a concise "what
// is still unproven" message instead of finishing shallow. It fires
// both when the deterministic gate would otherwise let the run
// complete (vacuous pass) and when the gate keeps failing but the
// honest end would land while time remains. Returns are bounded by
// MaxContinuations; after the bound the run enters closing mode
// instead of continuing.
func (e *Engine) lhShouldContinue(run *Run, gate tasks.GateReport) (bool, string) {
	cfg := e.longHorizonConfig()
	if !cfg.Enable || !cfg.EnableContinuation {
		return false, ""
	}
	_ = gate
	run.mu.Lock()
	request := run.Request
	limit := run.Tracker.Limit
	used := run.Tracker.Used
	started := run.StartedAt
	trips := run.lhTrips
	closing := run.lhClosing
	drafts := append([]tasks.DraftCriterion(nil), run.drafts...)
	run.mu.Unlock()
	if closing {
		return false, ""
	}
	maxTrips := cfg.MaxContinuations
	if maxTrips < 0 {
		maxTrips = 0
	}
	if trips >= maxTrips {
		return false, ""
	}
	// Significant budget left: wall time when capped, steps when not.
	now := time.Now().UTC()
	elapsed := now.Sub(started)
	if elapsed < 0 {
		elapsed = 0
	}
	// Prefer the run heartbeat clock when the wall clock has not
	// advanced (tests pin StartedAt without advancing time).
	run.mu.Lock()
	hbElapsed := run.HeartbeatAt.Sub(run.StartedAt)
	run.mu.Unlock()
	if hbElapsed > elapsed {
		elapsed = hbElapsed
	}
	if !tasks.SignificantBudgetLeft(cfg, limit.MaxSeconds, elapsed, used.Steps, limit.MaxSteps) {
		return false, ""
	}
	// Legacy runs without an instruction (empty request, no drafts)
	// keep the pre-long-horizon finish path: without a request there
	// is no checkable "unproven" to name.
	if strings.TrimSpace(request) == "" && len(drafts) == 0 {
		return false, ""
	}
	hist := historyTools(run.historyCopy())
	gaps := tasks.ContinuationGaps(hist, drafts, 1)
	if len(gaps) == 0 {
		return false, ""
	}
	run.mu.Lock()
	run.lhTrips++
	reachedBound := run.lhTrips >= maxTrips
	if reachedBound {
		run.lhClosing = true
	}
	run.mu.Unlock()
	return true, tasks.UnprovenNote(gaps)
}

// lhBackgroundNudge steers long-running exec calls to background+poll:
// a long pattern launched without backgrounding earns one volatile
// note per step naming the rerun discipline. Bounded to one note per
// step so the context stays small.
func (e *Engine) lhBackgroundNudge(run *Run, batch []providers.ToolCall, results []tools.Result) {
	cfg := e.longHorizonConfig()
	if !cfg.Enable || !cfg.EnableBackgroundNudge {
		return
	}
	for _, tc := range batch {
		if strings.ToLower(strings.TrimSpace(tc.Name)) != "exec" {
			continue
		}
		var body struct {
			Command    string `json:"command"`
			Background bool   `json:"background"`
		}
		if err := json.Unmarshal(tc.Arguments, &body); err != nil {
			continue
		}
		if strings.TrimSpace(body.Command) == "" {
			continue
		}
		if !tasks.IsLongCommand(body.Command, string(tc.Arguments), body.Background) {
			continue
		}
		run.InjectNote(tasks.BackgroundHint(body.Command))
		return
	}
}

// lhClosingNow reports whether the run exhausted its continuation
// bound and must render closing mode (tool_choice none) from here on.
func (e *Engine) lhClosingNow(run *Run) bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.lhClosing
}

// LongHorizonState exposes the milestone plan and continuation counters
// for trial records and status readers. It returns a copy so callers
// hold no lock.
func (r *Run) LongHorizonState() (plan []tasks.LHMilestone, trips int, closing, large, init bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	plan = append([]tasks.LHMilestone(nil), r.lhMilestones...)
	return plan, r.lhTrips, r.lhClosing, r.lhLarge, r.lhInit
}

func truncateLH(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
