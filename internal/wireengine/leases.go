package wireengine

import (
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// WorkspaceLeases adapts the workspace LeaseManager (Section 16.2) to
// the tools.LeaseManager interface the edit tools use (Section 13.6).
// The workspace manager owns expiry, heartbeats, OS advisory locks and
// run-state releases; this adapter only translates the older two-method
// shape onto it with an empty activity string.
type WorkspaceLeases struct {
	M *workspace.LeaseManager
}

// Acquire implements tools.LeaseManager.
func (w *WorkspaceLeases) Acquire(path, holder string) (func(), error) {
	if w == nil || w.M == nil {
		return func() {}, nil
	}
	return w.M.Acquire(filepath.Clean(path), holder, "")
}

// Holding implements tools.LeaseManager.
func (w *WorkspaceLeases) Holding(path string) (string, bool) {
	if w == nil || w.M == nil {
		return "", false
	}
	snap, ok := w.M.Holding(filepath.Clean(path))
	if !ok {
		return "", false
	}
	return snap.Holder, true
}

var _ tools.LeaseManager = (*WorkspaceLeases)(nil)

// ReleaseLeasesHook returns the orchestrator ReleaseLeases hook over
// the workspace manager: a paused or blocked run frees its leases so
// files are never immobilized for hours (Section 16.2). Nil-safe.
func ReleaseLeasesHook(m *workspace.LeaseManager) func(run types.RunID) {
	return func(run types.RunID) {
		if m == nil {
			return
		}
		m.ReleaseAll(string(run))
	}
}

// LeaseHeartbeatHook returns the engine heartbeat hook: while the run
// is running or waiting on a tool or subagents the orchestrator
// renews every lease of the holder, including during long tools.
// Human-paced waits release instead (workspace.NotifyRunState).
func LeaseHeartbeatHook(m *workspace.LeaseManager) func(runID string) {
	return func(runID string) {
		if m == nil {
			return
		}
		m.Heartbeat(runID)
	}
}

// LeaseReleaseHook returns the engine release hook for parked and
// terminal states.
func LeaseReleaseHook(m *workspace.LeaseManager) func(runID string) {
	return func(runID string) {
		if m == nil {
			return
		}
		m.ReleaseAll(runID)
	}
}

// NotifyRunState forwards an orchestrator run-state transition to the
// lease manager so paused, blocked, user-waiting and terminal states
// release while running and tool/subagent waits renew (Section 16.2).
func NotifyRunState(m *workspace.LeaseManager, holder string, state types.RunState, waitReason types.WaitReason) {
	if m == nil {
		return
	}
	m.NotifyRunState(holder, state, waitReason)
}

// EnsureRunnerAttached attaches the engine runner to the orchestrator
// when a run is admitted to running in embedded mode, and detaches it
// when the run leaves running. Callers invoke it around engine
// execution; the daemon will replace process-local attach with its
// supervision tree without changing the lookup contract.
func EnsureRunnerAttached(o *orchestrator.Orchestrator, r orchestrator.Runner) {
	o.AttachRunner(r)
}

// EnsureRunnerDetached forgets the runner after the run stops.
func EnsureRunnerDetached(o *orchestrator.Orchestrator, run types.RunID) {
	o.DetachRunner(run)
}
