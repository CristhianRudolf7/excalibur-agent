package orchestrator

import (
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// DeliveryReport is the Section 7.6 handover every execution emits:
// on a terminal state and when paused or blocked. The parent or the
// master receives it; the user sees it in plain language.
type DeliveryReport struct {
	RunID        types.RunID
	AgentID      types.AgentID
	State        types.RunState
	Reason       string
	Summary      string
	Deliverables []string
	Evidence     []string
	FilesChanged []string
	Assumptions  []string
	OpenQueries  []string
	Pending      []string
	Usage        Usage
	CreatedAt    time.Time
}

// PlainSummary renders the report as the one-line plain-language
// sentence the master template uses (Section 15.6): what ran, how it
// ended, and what is still open.
func (r DeliveryReport) PlainSummary() string {
	s := string(r.State)
	if r.Reason != "" {
		s += "(" + r.Reason + ")"
	}
	out := "run " + string(r.RunID) + " ended " + s + ": " + r.Summary
	if len(r.Pending) > 0 {
		out += " Pending: "
		for i, p := range r.Pending {
			if i > 0 {
				out += "; "
			}
			out += p
		}
	}
	return out
}

// EffectKind classifies a recorded tool effect for undo planning
// (Section 15.8).
type EffectKind string

// Effect kinds.
const (
	EffectEdit     EffectKind = "edit"
	EffectCommand  EffectKind = "command"
	EffectCommit   EffectKind = "commit"
	EffectExternal EffectKind = "external"
)

// EffectRecord is one recorded effect a run may need to undo.
type EffectRecord struct {
	Kind       EffectKind
	Tool       string
	Detail     string
	HasInverse bool
}

// UndoStep is one ordered compensation: undoable effects carry their
// inverse tool, the rest are reported as not reversible.
type UndoStep struct {
	Effect     EffectRecord
	Reversible bool
	// Inverse names the compensation when Reversible is true; empty
	// means the harness-native undo (edit history, shadow snapshot,
	// branch revert) applies.
	Inverse string
	Note    string
}

// PlanUndo orders compensations reverse (saga order) and marks what
// cannot be undone (Section 15.8): agent edits, in-project command
// effects and local commits are reversible through the harness;
// external effects only when the tool declares an inverse, and even
// then the compensation does not erase that the effect existed.
func PlanUndo(effects []EffectRecord) []UndoStep {
	out := make([]UndoStep, 0, len(effects))
	for i := len(effects) - 1; i >= 0; i-- {
		e := effects[i]
		step := UndoStep{Effect: e}
		switch e.Kind {
		case EffectEdit, EffectCommit:
			step.Reversible = true
			step.Note = "harness revert: edit history / branch revert"
		case EffectCommand:
			step.Reversible = true
			step.Note = "harness revert: project shadow snapshot"
		case EffectExternal:
			if e.HasInverse {
				step.Reversible = true
				step.Inverse = e.Tool
				step.Note = "compensation via declared inverse; the effect existed and may have been seen"
			} else {
				step.Note = "not reversible: external effect without a declared inverse"
			}
		default:
			step.Note = "not reversible: unknown effect kind"
		}
		out = append(out, step)
	}
	return out
}
