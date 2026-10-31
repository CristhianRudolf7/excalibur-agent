package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Deterministic status snapshots of Section 15.6: everything comes
// from the database and the orchestrator memory, never from a model.
// The fast lane serves them without a master turn.

// RunStatus is one node of the agent-tree snapshot.
type RunStatus struct {
	RunID         types.RunID
	AgentID       types.AgentID
	Name          string
	Profile       string
	Level         int
	State         types.RunState
	Reason        string
	QueuePosition int
	Step          int
	WaitingModel  bool
	Tool          string
	ToolArgs      string
	ToolElapsed   time.Duration
	LiveTail      string
	LastProgress  string
	LastReport    string
	PlanDone      int
	PlanTotal     int
	Milestone     string
	Blocked       []string
	Questions     []types.Question
	Budget        types.Budget
	Used          Usage
	BudgetUsedPct float64
	// Reminder tells the compiler to inject the Section 15.6
	// one-line report reminder in L5: 15 steps or 10 minutes without
	// a report or plan change.
	Reminder bool
	Children []*RunStatus
}

// Status returns the snapshot of the subtree rooted at run.
func (o *Orchestrator) Status(runID types.RunID) (*RunStatus, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	rec, err := o.store.GetRun(runID)
	if err != nil {
		return nil, err
	}
	return o.statusLocked(rec), nil
}

// StatusAgent returns the snapshot of the subtree rooted at agent.
func (o *Orchestrator) StatusAgent(agentID types.AgentID) (*RunStatus, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	agent, err := o.store.GetAgent(agentID)
	if err != nil {
		return nil, err
	}
	rec, ok := o.agentLiveRunLocked(agentID)
	if !ok {
		// Agent without a run: identity with no execution yet.
		st := &RunStatus{AgentID: agent.ID, Name: agent.Name, Profile: agent.Profile, Level: agent.Level}
		for _, c := range o.store.Children(agentID) {
			if child, err := o.statusAgentLocked(c.ID); err == nil {
				st.Children = append(st.Children, child)
			}
		}
		return st, nil
	}
	_ = agent
	return o.statusLocked(rec), nil
}

func (o *Orchestrator) statusAgentLocked(agentID types.AgentID) (*RunStatus, error) {
	rec, ok := o.agentLiveRunLocked(agentID)
	if !ok {
		agent, err := o.store.GetAgent(agentID)
		if err != nil {
			return nil, err
		}
		st := &RunStatus{AgentID: agent.ID, Name: agent.Name, Profile: agent.Profile, Level: agent.Level}
		for _, c := range o.store.Children(agentID) {
			if child, err := o.statusAgentLocked(c.ID); err == nil {
				st.Children = append(st.Children, child)
			}
		}
		return st, nil
	}
	return o.statusLocked(rec), nil
}

func (o *Orchestrator) statusLocked(rec RunRecord) *RunStatus {
	agent, _ := o.store.GetAgent(rec.Run.AgentID)
	st := &RunStatus{
		RunID:         rec.Run.ID,
		AgentID:       rec.Run.AgentID,
		Name:          agent.Name,
		Profile:       agent.Profile,
		Level:         agent.Level,
		State:         rec.Run.State,
		QueuePosition: rec.Run.QueuePosition,
		Budget:        rec.Run.Budget,
		Used:          o.store.GetUsage(rec.Run.ID),
		LastProgress:  rec.Run.LastProgress,
		Questions:     o.store.PendingQuestions(rec.Run.ID),
	}
	switch {
	case rec.Run.WaitReason != nil:
		st.Reason = string(*rec.Run.WaitReason)
	case rec.Run.PauseReason != nil:
		st.Reason = string(*rec.Run.PauseReason)
	case rec.Run.BlockReason != nil:
		st.Reason = string(*rec.Run.BlockReason)
	}
	if runner, ok := o.runners.Lookup(rec.Run.ID); ok {
		snap := runner.Snapshot()
		st.Step = snap.StepsDone
		st.WaitingModel = snap.WaitingModel
		st.Used = snap.Usage
		if snap.HasTool {
			st.Tool = snap.ToolName
			st.ToolArgs = summarizeArgs(snap.ToolArgs)
			if !snap.ToolStarted.IsZero() {
				st.ToolElapsed = o.cfg.Clock().Sub(snap.ToolStarted)
			}
			st.LiveTail = snap.ToolLiveTail
		}
		if snap.LastProgress != "" {
			st.LastProgress = snap.LastProgress
		}
		st.PlanDone, st.PlanTotal, st.Milestone = snap.PlanDone, snap.PlanTotal, snap.PlanMilestone
		sinceReport := snap.StepsDone - snap.LastReportStep
		sincePlan := snap.StepsDone - snap.LastPlanStep
		lastNarrative := o.lastReportLocked(rec.Run.ID)
		if !snap.LastPlanAt.IsZero() && snap.LastPlanAt.After(lastNarrative) {
			lastNarrative = snap.LastPlanAt
		}
		if (o.cfg.ReportReminderSteps > 0 && sinceReport >= o.cfg.ReportReminderSteps && sincePlan >= o.cfg.ReportReminderSteps) ||
			(o.cfg.ReportReminderSecs > 0 && o.cfg.Clock().Sub(lastNarrative).Seconds() >= float64(o.cfg.ReportReminderSecs)) {
			if rec.Run.State == types.RunRunning {
				st.Reminder = true
			}
		}
	}
	if o.hooks.PlanProgress != nil {
		if done, total, ms := o.hooks.PlanProgress(rec.Run.ID); total > 0 {
			st.PlanDone, st.PlanTotal, st.Milestone = done, total, ms
		}
	}
	if rep, ok := o.store.LatestReport(rec.Run.ID); ok {
		st.LastReport = rep.Summary
		if st.LastProgress == "" {
			st.LastProgress = rep.Summary
		}
	}
	for _, msg := range o.store.PendingMessages(rec.Run.AgentID) {
		if msg.Type == MsgBlocked {
			st.Blocked = append(st.Blocked, msg.Payload)
		}
	}
	st.BudgetUsedPct = budgetUsedPct(rec.Run.Budget, st.Used)
	for _, c := range o.store.Children(rec.Run.AgentID) {
		if child, err := o.statusAgentLocked(c.ID); err == nil {
			st.Children = append(st.Children, child)
		}
	}
	return st
}

func (o *Orchestrator) lastReportLocked(run types.RunID) time.Time {
	if rep, ok := o.store.LatestReport(run); ok {
		return rep.CreatedAt
	}
	if rec, err := o.store.GetRun(run); err == nil {
		return rec.Run.CreatedAt
	}
	return o.cfg.Clock()
}

func summarizeArgs(args string) string {
	const max = 120
	if len(args) <= max {
		return args
	}
	return args[:max] + "…"
}

func budgetUsedPct(b types.Budget, u Usage) float64 {
	max := 0.0
	if b.MaxSteps > 0 {
		max = frac(float64(u.Steps)/float64(b.MaxSteps), max)
	}
	if b.MaxTokens > 0 {
		max = frac(float64(u.InputTokens+u.OutputTokens)/float64(b.MaxTokens), max)
	}
	if b.MaxCostUSD > 0 {
		max = frac(u.CostUSD/b.MaxCostUSD, max)
	}
	if b.MaxSeconds > 0 {
		max = frac(u.Elapsed.Seconds()/float64(b.MaxSeconds), max)
	}
	return max
}

func frac(v, max float64) float64 {
	if v > max {
		return v
	}
	return max
}

// ToAPI maps the snapshot to the run.status contract (Section 3.5).
func (s *RunStatus) ToAPI() api.RunStatusResult {
	reason := s.Reason
	if s.State == types.RunQueued {
		reason = fmt.Sprintf("queued at position %d", s.QueuePosition)
	}
	step := fmt.Sprintf("step %d", s.Step)
	if s.WaitingModel {
		step += ", waiting on model"
	}
	return api.RunStatusResult{
		RunID:         string(s.RunID),
		State:         s.State,
		Reason:        reason,
		QueuePosition: s.QueuePosition,
		CurrentStep:   step,
		ToolInFlight:  s.Tool,
		LiveOutput:    s.LiveTail,
		LastProgress:  s.LastProgress,
		PlanDone:      s.PlanDone,
		PlanTotal:     s.PlanTotal,
		BudgetUsedPct: s.BudgetUsedPct,
	}
}

// Plain renders the one-line plain-language template the master uses
// ("Lena is running the export tests…", Section 15.6).
func (s *RunStatus) Plain() string {
	var b strings.Builder
	name := s.Name
	if name == "" {
		name = string(s.AgentID)
	}
	fmt.Fprintf(&b, "%s (%s) is %s", name, s.Profile, s.State)
	if s.Reason != "" {
		fmt.Fprintf(&b, " (%s)", s.Reason)
	}
	if s.Tool != "" {
		fmt.Fprintf(&b, "; runs %s", s.Tool)
		if s.ToolElapsed > 0 {
			fmt.Fprintf(&b, " for %s", s.ToolElapsed.Round(time.Second))
		}
		if s.LiveTail != "" {
			fmt.Fprintf(&b, "; last line: %s", lastLine(s.LiveTail))
		}
	}
	if s.PlanTotal > 0 {
		fmt.Fprintf(&b, "; %d of %d plan steps", s.PlanDone, s.PlanTotal)
	}
	if len(s.Blocked) > 0 {
		fmt.Fprintf(&b, "; blocked: %s", strings.Join(s.Blocked, "; "))
	} else {
		b.WriteString("; nothing blocked")
	}
	if s.State == types.RunQueued {
		fmt.Fprintf(&b, "; queue position %d", s.QueuePosition)
	}
	return b.String()
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}
