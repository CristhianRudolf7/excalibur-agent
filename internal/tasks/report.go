// Delivery report (Section 7.6).
//
// Every run that stops emits a structured report: at a terminal
// state and when paused or blocked. It holds the state with its
// reason ("incomplete" names in plain language a run paused by
// budget, time or order, or completed_with_pending), a summary of
// what happened, deliverables (references to files, documents,
// messages), evidence, modified files, assumptions, open questions,
// pendings and metrics (tokens, cost, time, calls). It is what the
// parent or the master receives, and what the user sees in plain
// language.
package tasks

import (
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ReportMetrics carries the per-task numbers measured from the first
// version (Section 22.2): tokens, cost, time and calls.
type ReportMetrics struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	Elapsed      time.Duration
	ToolCalls    int
}

// DeliveryReport is the structured stop report of Section 7.6.
type DeliveryReport struct {
	// State and Reason use the Section 7.2 vocabulary; Reason carries
	// the pause/block motive when any.
	State  types.RunState
	Reason string
	// PlainState renders the state in plain language for the user.
	PlainState   string
	Summary      string
	Deliverables []string
	Evidence     []types.EvidenceID
	Files        []string
	Assumptions  []string
	Questions    []string
	Pendings     []string
	Metrics      ReportMetrics
}

// PlainLanguage maps a run state to what the user reads: a run
// paused by budget, time or order, or completed_with_pending, reads
// "incomplete" (Section 7.6).
func PlainLanguage(state types.RunState, reason string) string {
	switch state {
	case types.RunCompleted:
		return "done"
	case types.RunCompletedPending:
		return "incomplete"
	case types.RunPaused:
		return "incomplete"
	case types.RunBlocked:
		return "blocked"
	case types.RunFailed:
		return "failed"
	case types.RunCanceled:
		return "canceled"
	default:
		if reason != "" {
			return string(state) + " (" + reason + ")"
		}
		return string(state)
	}
}

// ReportInput gathers the report fields from the run and the gate.
type ReportInput struct {
	State        types.RunState
	Reason       string
	Summary      string
	Deliverables []string
	Evidence     []types.EvidenceID
	Files        []string
	Assumptions  []string
	Questions    []string
	Gate         GateReport
	Metrics      ReportMetrics
}

// BuildReport assembles the delivery report: the gate pendings
// become the report pendings, and the state renders in plain
// language.
func BuildReport(in ReportInput) DeliveryReport {
	pendings := append([]string(nil), in.Gate.Pendings...)
	return DeliveryReport{
		State:        in.State,
		Reason:       in.Reason,
		PlainState:   PlainLanguage(in.State, in.Reason),
		Summary:      in.Summary,
		Deliverables: append([]string(nil), in.Deliverables...),
		Evidence:     append([]types.EvidenceID(nil), in.Evidence...),
		Files:        append([]string(nil), in.Files...),
		Assumptions:  append([]string(nil), in.Assumptions...),
		Questions:    append([]string(nil), in.Questions...),
		Pendings:     pendings,
		Metrics:      in.Metrics,
	}
}
