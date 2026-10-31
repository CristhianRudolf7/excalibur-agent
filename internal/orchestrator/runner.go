package orchestrator

import (
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// RunnerSnapshot is the live engine-side state the orchestrator reads
// for status, watchdogs and directive-ack tracking (Sections 7.2,
// 15.6, 15.7). The engine owns the loop; the orchestrator owns the
// authoritative run row and only observes here.
type RunnerSnapshot struct {
	State        types.RunState
	StepsDone    int
	WaitingModel bool

	HasTool      bool
	ToolName     string
	ToolArgs     string
	ToolStarted  time.Time
	ToolTimeout  time.Duration
	ToolLiveTail string
	ToolCancel   types.CancelMode
	ToolNonBlock bool
	// ToolResourceKeys are the in-flight tool resource keys
	// (canonical paths, sessions, accounts) for the Section 15.7
	// impact-area intersection.
	ToolResourceKeys []string

	LastProgress       string
	LastReportStep     int
	LastPlanStep       int
	LastPlanAt         time.Time
	PlanDone           int
	PlanTotal          int
	PlanMilestone      string
	InstructionsSeen   int
	AckedDirectives    map[string]bool
	Usage              Usage
	OpenAssistQuestion bool
}

// Runner is the engine-facing interface the orchestrator drives.
// Section 7 assigns the step loop to internal/engine (H3/H5
// territory, not merged yet); this interface is the seam the real
// engine will implement and the fake in orchestrator/fake stands in
// for in tests. Control requests are advisory for cooperative tools
// and immediate otherwise, per each tool's cancel class.
type Runner interface {
	// RunID is the execution this runner drives.
	RunID() types.RunID
	// RequestInterrupt asks the engine to stop: the in-flight model
	// call is canceled and the tool stops per its cancel class. With
	// toolOnly the run itself keeps going and decides how to
	// continue (Section 15.8).
	RequestInterrupt(toolOnly bool)
	// ApplyDirective hands a directive to the engine for the next
	// safe point (Section 15.5).
	ApplyDirective(d Directive)
	// Snapshot reports the live facts for status and watchdogs.
	Snapshot() RunnerSnapshot
}

// RunnerRegistry tracks the live engines attached to runs in embedded
// mode. The daemon will replace process-local attach with its
// supervision tree; the lookup contract stays the same.
type RunnerRegistry struct {
	runners map[types.RunID]Runner
}

// Attach records the runner driving run.
func (r *RunnerRegistry) Attach(runner Runner) {
	if r.runners == nil {
		r.runners = map[types.RunID]Runner{}
	}
	r.runners[runner.RunID()] = runner
}

// Detach forgets the runner driving run.
func (r *RunnerRegistry) Detach(run types.RunID) {
	delete(r.runners, run)
}

// Lookup returns the runner driving run, if any.
func (r *RunnerRegistry) Lookup(run types.RunID) (Runner, bool) {
	out, ok := r.runners[run]
	return out, ok
}
