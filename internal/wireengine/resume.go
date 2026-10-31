package wireengine

import (
	"context"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ResumeOutcome is the result of resuming one interrupted run from
// its journal: the engine recovery report plus the orchestrator state
// the run was left in.
type ResumeOutcome struct {
	Report    engine.RecoveryReport
	State     types.RunState
	Reran     int
	Marked    int
	Uncertain int
}

// ResumeReplays replays one interrupted run without repeating effects
// (Sections 17.2, 17.3): it lists the journal entries in sequence
// order, rebuilds the run state with engine.Recover, applies the
// recovery decision to the live run, and re-executes only the rerun
// decisions with their recorded idempotency keys (never a model call,
// never a repeated uncertain effect). waitingUser restores a run that
// expected the user (a question or plan approval) instead of
// requeueing it.
func ResumeReplays(ctx context.Context, eng *engine.Engine, run *engine.Run, store journal.Store, waitingUser bool) (ResumeOutcome, error) {
	entries, err := store.List(ctx, run.ID)
	if err != nil {
		return ResumeOutcome{}, fmt.Errorf("wireengine: list journal: %w", err)
	}
	prober := engine.RecoverProber{Tools: nil}
	rep := eng.Recover(ctx, run, entries, prober, waitingUser)
	if err := eng.ApplyRecovery(ctx, run, rep); err != nil {
		return ResumeOutcome{}, err
	}
	if err := eng.ReplayPending(ctx, run, rep); err != nil {
		return ResumeOutcome{}, err
	}
	return ResumeOutcome{
		Report: rep, State: run.State(),
		Reran: len(rep.Rerun), Marked: len(rep.Done),
		Uncertain: len(rep.Uncertain),
	}, nil
}

// AdoptIntoOrchestrator mirrors an engine recovery decision into the
// orchestrator's authoritative row (the engine owns the loop, the
// orchestrator owns the row): resume requeues, long interruptions
// and exhausted budgets park as paused, uncertain effects park as
// blocked, and intact user waits restore waiting. It returns the
// orchestrator state applied.
func AdoptIntoOrchestrator(o *orchestrator.Orchestrator, id types.RunID, rep engine.RecoveryReport) (types.RunState, error) {
	if _, err := o.RunState(id); err != nil {
		return "", err
	}
	switch rep.Decision {
	case engine.RecoverResume:
		// Boot adoption parks the run in recovering first
		// (running -> recovering -> queued); a run that is
		// already queued stays there.
		if err := o.Transition(id, types.RunQueued); err == nil {
			return types.RunQueued, nil
		}
		if err := o.SetRecovering(id); err != nil {
			return types.RunQueued, nil
		}
		if err := o.Transition(id, types.RunQueued); err != nil {
			return types.RunQueued, nil
		}
		return types.RunQueued, nil
	case engine.RecoverPaused:
		if err := o.Transition(id, types.RunPaused); err == nil {
			return types.RunPaused, nil
		}
		if err := o.SetRecovering(id); err != nil {
			return types.RunPaused, nil
		}
		if err := o.Transition(id, types.RunPaused); err != nil {
			return types.RunPaused, nil
		}
		return types.RunPaused, nil
	case engine.RecoverBlocked:
		if err := o.Block(id, types.BlockUncertainEffect, rep.Notice); err != nil {
			// The row may already sit recovering or blocked;
			// the journal decision still stands.
			return types.RunBlocked, nil
		}
		return types.RunBlocked, nil
	case engine.RecoverAskUser:
		if err := o.SetWaiting(id, types.WaitUser); err != nil {
			return types.RunWaiting, nil
		}
		return types.RunWaiting, nil
	default:
		return "", fmt.Errorf("wireengine: unknown recovery decision %q", rep.Decision)
	}
}
