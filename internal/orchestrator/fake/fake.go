package fake

import (
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Runner is a scripted orchestrator.Runner: tests advance it with
// Step, park tools in flight with SetTool, and watch interrupts and
// directive deliveries. AutoAck makes Step acknowledge every pending
// directive through the orchestrator, like an engine applying them at
// its next safe point.
type Runner struct {
	mu             sync.Mutex
	run            types.RunID
	agent          types.AgentID
	snap           orchestrator.RunnerSnapshot
	interrupts     int
	toolInterrupts int
	delivered      []orchestrator.Directive
	finishLeft     int
	AutoAck        bool
	AckNote        string
	// FinishAfter bounds how many interrupted steps an
	// after_completion tool needs to finish (default 1).
	FinishAfter int
}

// NewRunner returns a runner driving run for agent at step zero.
func NewRunner(run types.RunID, agent types.AgentID) *Runner {
	return &Runner{run: run, agent: agent,
		snap:    orchestrator.RunnerSnapshot{State: types.RunRunning, AckedDirectives: map[string]bool{}},
		AckNote: "applied at safe point",
	}
}

// RunID implements orchestrator.Runner.
func (r *Runner) RunID() types.RunID { return r.run }

// RequestInterrupt implements orchestrator.Runner: it records the
// request; the scripted engine stops at the next Step.
func (r *Runner) RequestInterrupt(toolOnly bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if toolOnly {
		r.toolInterrupts++
		return
	}
	r.interrupts++
}

// ApplyDirective implements orchestrator.Runner.
func (r *Runner) ApplyDirective(d orchestrator.Directive) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delivered = append(r.delivered, d)
}

// Snapshot implements orchestrator.Runner.
func (r *Runner) Snapshot() orchestrator.RunnerSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.snap
	out.AckedDirectives = map[string]bool{}
	for k, v := range r.snap.AckedDirectives {
		out.AckedDirectives[k] = v
	}
	out.ToolResourceKeys = append([]string(nil), r.snap.ToolResourceKeys...)
	return out
}

// Step advances one engine step. A parked run (anything but running)
// consumes nothing: the step is skipped. Pending directives are acked
// through o when AutoAck is set. An interrupt request stops immediate
// and graceful tools at once; after_completion tools finish first
// (FinishAfter steps, default 1); cooperative tools stay pending
// until ClearTool.
func (r *Runner) Step(o *orchestrator.Orchestrator) {
	st, err := o.RunState(r.run)
	if err != nil {
		return
	}
	if st != types.RunRunning {
		// Parked runs consume nothing, but their interrupted
		// tools still wind down in real time.
		r.mu.Lock()
		r.settleInterruptLocked()
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	if r.interrupts > 0 {
		r.settleInterruptLocked()
		if r.interrupts > 0 {
			r.mu.Unlock()
			return
		}
	}
	r.snap.StepsDone++
	r.snap.Usage.Steps++
	step := r.snap.StepsDone
	r.mu.Unlock()
	if r.AutoAck {
		for _, msg := range o.PendingDirectives(r.agent) {
			if msg.Type != orchestrator.MsgDirective {
				continue
			}
			_ = o.AckDirective(r.run, msg.ID, r.AckNote, step)
			r.mu.Lock()
			r.snap.AckedDirectives[string(msg.ID)] = true
			r.snap.InstructionsSeen = step
			r.mu.Unlock()
		}
	}
}

// settleInterruptLocked winds one interrupted tool down: immediate
// and graceful tools clear at once, after_completion tools finish
// first (FinishAfter rounds), cooperative tools stay pending until
// the capability answers. The caller holds r.mu.
func (r *Runner) settleInterruptLocked() {
	if r.interrupts == 0 || !r.snap.HasTool {
		return
	}
	switch r.snap.ToolCancel {
	case types.CancelImmediate, types.CancelGraceful:
		r.snap.HasTool = false
		r.snap.ToolName = ""
	case types.CancelAfterCompletion:
		if r.finishLeft <= 0 {
			r.finishLeft = r.FinishAfter
			if r.finishLeft <= 0 {
				r.finishLeft = 1
			}
		}
		r.finishLeft--
		if r.finishLeft == 0 {
			r.snap.HasTool = false
			r.snap.ToolName = ""
		}
	case types.CancelCooperative:
		// Stays pending: the harness re-queries the final state.
	}
}

// SetTool parks a tool in flight for status and steer scenarios.
func (r *Runner) SetTool(name, args string, cancel types.CancelMode, keys []string, timeout time.Duration, started time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.HasTool = true
	r.snap.ToolName = name
	r.snap.ToolArgs = args
	r.snap.ToolCancel = cancel
	r.snap.ToolResourceKeys = append([]string(nil), keys...)
	r.snap.ToolTimeout = timeout
	r.snap.ToolStarted = started
	r.snap.ToolLiveTail = ""
}

// SetLiveTail sets the queued live-output tail.
func (r *Runner) SetLiveTail(tail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.ToolLiveTail = tail
}

// ClearTool ends the tool in flight.
func (r *Runner) ClearTool() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.HasTool = false
	r.snap.ToolName = ""
	r.snap.ToolArgs = ""
	r.snap.ToolResourceKeys = nil
	r.snap.ToolLiveTail = ""
}

// SetWaitingModel marks a model call in flight.
func (r *Runner) SetWaitingModel(waiting bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.WaitingModel = waiting
}

// SetProgress records narrative and plan progress for status.
func (r *Runner) SetProgress(last string, done, total int, milestone string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.LastProgress = last
	r.snap.PlanDone = done
	r.snap.PlanTotal = total
	r.snap.PlanMilestone = milestone
	r.snap.LastReportStep = r.snap.StepsDone
	r.snap.LastPlanStep = r.snap.StepsDone
	r.snap.LastPlanAt = time.Now().UTC()
}

// MarkReported moves the narrative counters to the current step.
func (r *Runner) MarkReported() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.LastReportStep = r.snap.StepsDone
	r.snap.LastPlanStep = r.snap.StepsDone
	r.snap.LastPlanAt = time.Now().UTC()
}

// HoldNarrative freezes the narrative counters to trigger the
// Section 15.6 report reminder in tests.
func (r *Runner) HoldNarrative() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.LastReportStep = 0
	r.snap.LastPlanStep = 0
	r.snap.LastPlanAt = time.Time{}
}

// Interrupts reports full and tool-only interrupt requests.
func (r *Runner) Interrupts() (full, toolOnly int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.interrupts, r.toolInterrupts
}

// Delivered returns the directives pushed to this runner.
func (r *Runner) Delivered() []orchestrator.Directive {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]orchestrator.Directive(nil), r.delivered...)
}

// ClearInterrupt releases a recorded full interrupt so Step advances
// again.
func (r *Runner) ClearInterrupt() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interrupts = 0
}
