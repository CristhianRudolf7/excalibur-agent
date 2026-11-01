// Boot recovery (Sections 17.2, 17.3 H3 subset, 17.8 crash suite).
//
// Recover rebuilds a run from its journal after an interruption:
// it replays the model leg, classifies every tool intent left
// without a result, and decides, conservatively, how the run
// continues. The full boot sequence (porters, indexes, agenda,
// safe mode) belongs to the daemon milestone; this file owns the
// per-run half: no repeated model call for a recorded response, and
// no repeated non-idempotent effect.
package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// RecoveryDecision tells the orchestrator how to continue the run.
type RecoveryDecision string

// Recovery decisions.
const (
	// RecoverResume re-queues the run with a recovery notice (17.3):
	// everything reconciled without uncertainty and the interruption
	// was short.
	RecoverResume RecoveryDecision = "resume"
	// RecoverPaused parks the run (recovery): long interruption or
	// nearly exhausted budget (17.3).
	RecoverPaused RecoveryDecision = "paused"
	// RecoverBlocked parks the run with an uncertain effect (17.2):
	// never repeat it automatically; ask with information.
	RecoverBlocked RecoveryDecision = "blocked"
	// RecoverAskUser re-enters waiting: the run expected the user
	// (a question or plan approval) and the request is intact.
	RecoverAskUser RecoveryDecision = "waiting"
)

// AutoResumeWithin bounds automatic requeueing after an interruption
// (Appendix B: 6 hours).
const AutoResumeWithin = 6 * time.Hour

// RecoveryReport is the per-run outcome of boot reconciliation.
type RecoveryReport struct {
	RunID         string
	StepsPassed   int
	ModelReplay   journal.ModelReplay
	Rerun         []journal.PendingIntent
	Done          []journal.PendingIntent
	Uncertain     []journal.PendingIntent
	Decision      RecoveryDecision
	Notice        string
	InterruptedBy time.Duration
}

// RecoverProber adapts tools.Prober to the journal seam.
type RecoverProber struct {
	Tools map[string]tools.Tool
}

// Probe implements journal.Prober: only tools declaring the probe
// count; anything else reports not-found without error.
func (p RecoverProber) Probe(ctx context.Context, name string, args json.RawMessage) (bool, error) {
	t, ok := p.Tools[name]
	if !ok {
		return false, nil
	}
	pr, ok := t.(tools.Prober)
	if !ok {
		return false, nil
	}
	return pr.Probe(ctx, args)
}

// Recover rebuilds run state from its journal entries and reconciles
// the interrupted leg. Entries must come from the run's Store in
// sequence order. Reconstruction force-sets the pre-crash state (the
// last recorded state_change, running by default) and the step
// count: this is a rebuild, not a live transition, so the 7.2
// diagram does not apply to it; ApplyRecovery performs the live
// transition afterwards.
func (e *Engine) Recover(ctx context.Context, run *Run, entries []journal.Entry, prober journal.Prober, waitingUser bool) RecoveryReport {
	rep := RecoveryReport{RunID: run.ID}
	rep.ModelReplay = journal.ScanModelLeg(entries)
	pendings := append(journal.PendingIntents(entries), journal.MissingIntents(entries)...)
	e.enrichPendings(pendings)
	decisions := journal.ResolveAll(ctx, pendings, prober)
	for i, d := range decisions {
		switch d.Action {
		case journal.ActionRerun:
			rep.Rerun = append(rep.Rerun, pendings[i])
		case journal.ActionDone:
			rep.Done = append(rep.Done, pendings[i])
		default:
			rep.Uncertain = append(rep.Uncertain, pendings[i])
		}
	}
	rep.StepsPassed = rep.ModelReplay.Responses
	lastState := types.RunRunning
	for _, en := range entries {
		if en.Type == journal.TypeStateChange {
			var p journal.StateChangePayload
			if err := json.Unmarshal(en.Payload, &p); err == nil && p.To != "" {
				lastState = types.RunState(p.To)
			}
		}
	}
	run.mu.Lock()
	if !run.state.IsTerminal() {
		run.state = lastState
	}
	if rep.StepsPassed > run.Steps {
		run.Steps = rep.StepsPassed
	}
	run.HeartbeatAt = e.deps.Clock().UTC()
	run.mu.Unlock()
	if len(entries) > 0 {
		rep.InterruptedBy = e.deps.Clock().UTC().Sub(entries[len(entries)-1].At)
		if rep.InterruptedBy < 0 {
			rep.InterruptedBy = 0
		}
	}
	switch {
	case waitingUser:
		rep.Decision = RecoverAskUser
		rep.Notice = "run was waiting for the user; the question is intact"
	case len(rep.Uncertain) > 0:
		rep.Decision = RecoverBlocked
		rep.Notice = "uncertain effect: " + decisions[0].Reason + "; needs information or a question"
	case rep.InterruptedBy > AutoResumeWithin:
		rep.Decision = RecoverPaused
		rep.Notice = "interruption exceeded 6h; parked for review"
	case e.budgetNearlyGone(run):
		rep.Decision = RecoverPaused
		rep.Notice = "budget nearly exhausted; parked for review"
	default:
		rep.Decision = RecoverResume
		rep.Notice = "reconciled without uncertainty; resuming"
	}
	run.InjectNote("recovery: " + rep.Notice)
	if len(rep.Done) > 0 {
		run.InjectNote("recovery: marked done by postcondition")
	}
	return rep
}

func (e *Engine) budgetNearlyGone(run *Run) bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	dup := run.Tracker
	return dup.Evaluate() != tasks.StatusOK
}

// ApplyRecovery moves the run to the state its recovery decision
// names: resume re-queues, paused parks with the recovery motive,
// blocked parks with the uncertain-effect motive, waiting restores
// the user wait (7.2, 17.3). The source state is the reconstructed
// pre-crash state, so the move is a direct set journaled as a
// state_change, not a live 7.2 transition.
func (e *Engine) ApplyRecovery(ctx context.Context, run *Run, rep RecoveryReport) error {
	run.mu.Lock()
	defer run.mu.Unlock()
	from := run.state
	if from.IsTerminal() {
		return nil
	}
	switch rep.Decision {
	case RecoverResume:
		run.state = types.RunQueued
		run.pauseReason = ""
		run.blockReason = ""
		run.waitReason = ""
	case RecoverPaused:
		run.state = types.RunPaused
		run.pauseReason = types.PauseRecovery
	case RecoverBlocked:
		run.state = types.RunBlocked
		run.blockReason = types.BlockUncertainEffect
	case RecoverAskUser:
		run.state = types.RunWaiting
		run.waitReason = types.WaitUser
	}
	e.recordState(ctx, run, from, run.state, "recovery: "+rep.Notice)
	return nil
}

// enrichPendings fills missing effect metadata from the tool
// catalog so synthesized intents (recorded response, no intent)
// classify exactly like recorded ones (17.2).
func (e *Engine) enrichPendings(pendings []journal.PendingIntent) {
	if e.deps.Tools == nil {
		return
	}
	for i := range pendings {
		in := &pendings[i].Intent
		if in.Effect != "" && in.Idempotency != "" {
			continue
		}
		def, err := e.deps.Tools.Definition(in.Tool)
		if err != nil {
			continue
		}
		if in.Effect == "" {
			in.Effect = def.Effect
		}
		if in.Idempotency == "" {
			in.Idempotency = def.Idempotency
		}
	}
}

func toolCallOf(p journal.PendingIntent) providers.ToolCall {
	return providers.ToolCall{ID: p.Intent.CallID, Name: p.Intent.Tool, Arguments: p.Intent.Args}
}

func historyEventOf(seq int64, tc providers.ToolCall, res tools.Result) ctxcompiler.ActivityEvent {
	return ctxcompiler.ActivityEvent{
		Seq: seq, Role: providers.RoleTool, Content: res.Output,
		ToolResult: &providers.ToolResult{CallID: tc.ID, Name: tc.Name, Output: res.Output, IsError: res.Status != tools.StatusOK},
	}
}

func toolEventOf(tc providers.ToolCall, res tools.Result) tasks.ToolEvent {
	return tasks.ToolEvent{Tool: tc.Name, Args: string(tc.Arguments), Result: res.Output, Error: toolErrorOf(res)}
}

// ReplayPending executes the rerun decisions of a recovery without
// calling the model: the recorded response is reused for its step
// (7.3). Each intent reuses its recorded idempotency key, so keyed
// effects land once (17.2). Done decisions only leave notes;
// uncertain ones stay parked by ApplyRecovery.
func (e *Engine) ReplayPending(ctx context.Context, run *Run, rep RecoveryReport) error {
	for _, p := range rep.Rerun {
		if err := ctx.Err(); err != nil {
			return err
		}
		tc := toolCallOf(p)
		res := e.runOneWithKey(ctx, run, tc, p.Intent.IdempotencyKey)
		run.mu.Lock()
		seq := run.nextHistorySeqLocked()
		run.History = append(run.History, historyEventOf(seq, tc, res))
		run.mu.Unlock()
		for _, d := range run.Loop.ObserveTool(toolEventOf(tc, res)) {
			if stop, err := e.onDetection(ctx, run, d); err != nil || stop {
				return err
			}
		}
	}
	return nil
}
