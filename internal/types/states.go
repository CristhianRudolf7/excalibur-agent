package types

// Run lifecycle states from Section 7.2.
//
// NOTE on naming: the architecture document labels these states in
// Spanish (lista, ejecutando, …). The repository is English-only, so the
// canonical stored values are the English strings below; see
// docs/decisions/0002-english-state-enum-values.md for the mapping.
type RunState string

// Run states.
const (
	RunCreated          RunState = "created"
	RunQueued           RunState = "queued"
	RunRunning          RunState = "running"
	RunWaiting          RunState = "waiting"
	RunVerifying        RunState = "verifying"
	RunPaused           RunState = "paused"
	RunBlocked          RunState = "blocked"
	RunRecovering       RunState = "recovering"
	RunCompleted        RunState = "completed"
	RunCompletedPending RunState = "completed_with_pending"
	RunFailed           RunState = "failed"
	RunCanceled         RunState = "canceled"
)

// IsTerminal reports whether the state is terminal. Paused and blocked
// are NOT terminal: they keep journal, context, plan and worktree and
// resume where they stopped.
func (s RunState) IsTerminal() bool {
	switch s {
	case RunCompleted, RunCompletedPending, RunFailed, RunCanceled:
		return true
	default:
		return false
	}
}

// WaitReason identifies why a run is waiting (Section 7.2).
type WaitReason string

// Wait reasons.
const (
	WaitTool       WaitReason = "tool"
	WaitUser       WaitReason = "user"
	WaitSubagents  WaitReason = "subagents"
	WaitAgenda     WaitReason = "agenda"
	WaitCapability WaitReason = "capability"
	WaitLease      WaitReason = "lease"
)

// PauseReason identifies why a run is paused (Section 7.2).
type PauseReason string

// Pause reasons.
const (
	PauseUser     PauseReason = "user"
	PauseParent   PauseReason = "parent"
	PauseResource PauseReason = "resource"
	PauseBudget   PauseReason = "budget"
	PauseRecovery PauseReason = "recovery"
	PauseSafeMode PauseReason = "safe_mode"
)

// BlockReason identifies why a run is blocked (Section 7.2).
type BlockReason string

// Block reasons.
const (
	BlockLoop              BlockReason = "loop"
	BlockMissingData       BlockReason = "missing_data"
	BlockUncertainEffect   BlockReason = "uncertain_effect"
	BlockVerifierDisagrees BlockReason = "verifier_disagreement"
	BlockAwaitingUser      BlockReason = "awaiting_user"
)

// transitions encodes the state diagram of Section 7.2: every allowed
// edge, and nothing else.
var transitions = map[RunState]map[RunState]bool{
	RunCreated: {
		RunQueued: true, RunCanceled: true,
	},
	RunQueued: {
		RunRunning: true, RunPaused: true, RunCanceled: true,
	},
	RunRunning: {
		RunWaiting: true, RunVerifying: true, RunPaused: true,
		RunBlocked: true, RunFailed: true, RunCanceled: true,
		RunRecovering: true,
	},
	RunWaiting: {
		RunRunning: true, RunPaused: true, RunBlocked: true,
		RunCanceled: true, RunRecovering: true,
	},
	RunVerifying: {
		RunRunning: true, RunCompleted: true,
		RunCompletedPending: true, RunBlocked: true,
	},
	RunPaused: {
		RunQueued: true, RunCanceled: true,
	},
	RunBlocked: {
		RunQueued: true, RunCanceled: true,
	},
	RunRecovering: {
		RunQueued: true, RunPaused: true, RunBlocked: true,
	},
}

// CanTransition reports whether moving a run from one state to another
// is allowed by the Section 7.2 diagram.
func CanTransition(from, to RunState) bool {
	return transitions[from][to]
}

// TaskState is derived from the root run state; the model never writes
// it (Section 8.2).
type TaskState string

// Task states.
const (
	TaskOpen             TaskState = "open"
	TaskInProgress       TaskState = "in_progress"
	TaskPaused           TaskState = "paused"
	TaskBlocked          TaskState = "blocked"
	TaskCompleted        TaskState = "completed"
	TaskCompletedPending TaskState = "completed_with_pending"
	TaskFailed           TaskState = "failed"
	TaskCanceled         TaskState = "canceled"
)

// DeriveTaskState maps a root run state to its task state.
func DeriveTaskState(run RunState) TaskState {
	switch run {
	case RunCreated:
		return TaskOpen
	case RunQueued, RunRunning, RunWaiting, RunVerifying, RunRecovering:
		return TaskInProgress
	case RunPaused:
		return TaskPaused
	case RunBlocked:
		return TaskBlocked
	case RunCompleted:
		return TaskCompleted
	case RunCompletedPending:
		return TaskCompletedPending
	case RunFailed:
		return TaskFailed
	case RunCanceled:
		return TaskCanceled
	default:
		return TaskOpen
	}
}

// CriterionState is computed, never written by the model (Section 8.3).
type CriterionState string

// Criterion states.
const (
	CriterionSatisfied     CriterionState = "satisfied"
	CriterionFailed        CriterionState = "failed"
	CriterionUnverified    CriterionState = "unverified"
	CriterionNotApplicable CriterionState = "not_applicable"
)

// CriterionEvidence describes the latest evidence known for a criterion.
type CriterionEvidence struct {
	// Present is false when no evidence exists yet.
	Present bool
	// Fresh is false when the evidence predates a change in the impact
	// area or in the applicable instruction revision.
	Fresh bool
	// Passed is the recorded outcome (pass/fail) of fresh evidence.
	Passed bool
	// NotApplicable marks criteria waived with a recorded justification.
	NotApplicable bool
	Justified     bool
}

// ComputeCriterionState implements the Section 8.3 algorithm.
func ComputeCriterionState(e CriterionEvidence) CriterionState {
	if e.NotApplicable && e.Justified {
		return CriterionNotApplicable
	}
	if !e.Present || !e.Fresh {
		return CriterionUnverified
	}
	if e.Passed {
		return CriterionSatisfied
	}
	return CriterionFailed
}
