// Engine Runner: the engine side of the orchestrator contract.
//
// The orchestrator owns the authoritative run row and drives control
// orders (Sections 5.1, 15.8); the engine owns the step loop (Section
// 7). This file is the seam where they meet: EngineRunner adapts one
// *Engine + *Run to the orchestrator.Runner interface so control
// orders interrupt the in-flight model call or tool, directives land
// at the next safe point, and status reads the live facts without
// touching engine internals from the orchestrator package.
package engine

import (
	"context"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// EngineRunner drives one run and reports its live facts. It is the
// production orchestrator.Runner; orchestrator/fake stands in only in
// unit tests that never start an engine.
type EngineRunner struct {
	eng *Engine
	run *Run
}

// NewRunner returns the Runner driving run on eng. Both must be
// non-nil; the orchestrator attaches it with AttachRunner and detaches
// it when the run leaves running.
func NewRunner(eng *Engine, run *Run) *EngineRunner {
	return &EngineRunner{eng: eng, run: run}
}

// RunID implements orchestrator.Runner.
func (r *EngineRunner) RunID() types.RunID {
	return types.RunID(r.run.ID)
}

// RequestInterrupt implements orchestrator.Runner: it stops the
// in-flight model call and the current step's tools per Section 15.8.
// With toolOnly the run keeps going and decides how to continue.
func (r *EngineRunner) RequestInterrupt(toolOnly bool) {
	if r.eng == nil || r.run == nil {
		return
	}
	r.eng.RequestInterrupt(r.run, toolOnly)
}

// ApplyDirective implements orchestrator.Runner: the directive lands
// in the run queue and is applied at the next safe point (top of the
// next step), journaled as applied and folded into a volatile note.
func (r *EngineRunner) ApplyDirective(d orchestrator.Directive) {
	if r.run == nil {
		return
	}
	at := time.Now().UTC()
	ed := Directive{
		ID:       string(d.ID),
		Text:     d.Text,
		Priority: string(d.Action),
		At:       at,
	}
	// DeliverDirective journals and wakes a waiting run; for a
	// running run the directive waits for the next safe point.
	_ = r.eng.DeliverDirective(context.Background(), r.run, ed)
}

// Snapshot implements orchestrator.Runner: the live facts for status,
// watchdogs and directive-ack tracking (Sections 7.2, 15.6, 15.7).
func (r *EngineRunner) Snapshot() orchestrator.RunnerSnapshot {
	run := r.run
	run.mu.Lock()
	defer run.mu.Unlock()
	acked := map[string]bool{}
	for k, v := range run.ackedDirects {
		acked[k] = v
	}
	usage := orchestrator.Usage{
		Steps:        run.Tracker.Used.Steps,
		InputTokens:  run.Tracker.Used.InputTokens,
		OutputTokens: run.Tracker.Used.OutputTokens,
		CostUSD:      run.Tracker.Used.CostUSD,
		Elapsed:      run.Tracker.Used.Elapsed,
		ToolCalls:    run.Tracker.Used.ToolCalls,
	}
	snap := orchestrator.RunnerSnapshot{
		State:            run.state,
		StepsDone:        run.Steps,
		WaitingModel:     run.waitingModel,
		HasTool:          run.toolName != "",
		ToolName:         run.toolName,
		ToolArgs:         run.toolArgs,
		ToolStarted:      run.toolStarted,
		ToolTimeout:      run.toolTimeout,
		ToolLiveTail:     run.toolLiveTail,
		ToolCancel:       run.toolCancel,
		ToolNonBlock:     run.toolNonBlock,
		ToolResourceKeys: append([]string(nil), run.toolKeys...),
		LastProgress:     run.lastProgress,
		LastReportStep:   run.lastReportStep,
		LastPlanStep:     run.lastPlanStep,
		LastPlanAt:       run.lastPlanAt,
		InstructionsSeen: run.instrSeen,
		AckedDirectives:  acked,
		Usage:            usage,
	}
	// Plan progress comes from the tracker-backed progress when the
	// orchestrator hook is absent; the daemon fills PlanDone/Total
	// via its own hook. Report the gate progress here.
	snap.PlanDone = run.GateAttempts
	return snap
}

// RunHandle exposes the underlying run for embedded drivers that own
// both sides (excalibur exec). Orchestrator flows use only the
// Runner interface.
func (r *EngineRunner) RunHandle() *Run { return r.run }

// EngineHandle exposes the underlying engine for embedded drivers.
func (r *EngineRunner) EngineHandle() *Engine { return r.eng }

var _ orchestrator.Runner = (*EngineRunner)(nil)

// TrackerUsage converts the engine task usage to orchestrator usage
// for status reports that merge both sides.
func TrackerUsage(u tasks.Usage) orchestrator.Usage {
	return orchestrator.Usage{
		Steps: u.Steps, InputTokens: u.InputTokens,
		OutputTokens: u.OutputTokens, CostUSD: u.CostUSD,
		Elapsed: u.Elapsed, ToolCalls: u.ToolCalls,
	}
}
