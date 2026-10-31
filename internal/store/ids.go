package store

import (
	"context"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Short human- and model-facing codes of Section 4.4: T45 (task),
// R12 (requirement), C12.3 (criterion 3 of requirement 12), M2
// (milestone). Models and humans always see the codes or full ULIDs,
// never list positions. Codes are allocated from per-scope counters
// that never move backwards, so deleted codes are never reused.

// NextTaskCode allocates the next T-code for scope (usually a
// project or the general scope).
func NextTaskCode(ctx context.Context, w Writer, scopeID string) (types.TaskRef, error) {
	n, err := nextShort(ctx, w, scopeID, "T")
	if err != nil {
		return "", err
	}
	return types.FormatTaskRef(n), nil
}

// NextRequirementCode allocates the next R-code for scope.
func NextRequirementCode(ctx context.Context, w Writer, scopeID string) (types.RequirementRef, error) {
	n, err := nextShort(ctx, w, scopeID, "R")
	if err != nil {
		return "", err
	}
	return types.FormatRequirementRef(n), nil
}

// NextMilestoneCode allocates the next M-code for scope.
func NextMilestoneCode(ctx context.Context, w Writer, scopeID string) (types.MilestoneRef, error) {
	n, err := nextShort(ctx, w, scopeID, "M")
	if err != nil {
		return "", err
	}
	return types.FormatMilestoneRef(n), nil
}

// NextCriterionCode allocates the next C-code for one requirement:
// Cn.k where n is the requirement number and k counts up per
// requirement, never reused after deletion.
func NextCriterionCode(ctx context.Context, w Writer, req types.RequirementRef, requirementID string) (types.CriterionRef, error) {
	n, err := nextShort(ctx, w, requirementID, "C")
	if err != nil {
		return types.CriterionRef{}, err
	}
	return types.CriterionRef{Requirement: req.Number(), Criterion: n}, nil
}

// nextShort atomically allocates one counter value. The increment
// happens inside a porter transaction, so concurrent allocators
// never collide and gaps (from discarded tasks) are never filled:
// codes are never reused (Section 4.4).
func nextShort(ctx context.Context, w Writer, scopeID, series string) (int, error) {
	var n int
	err := w.Write(ctx, PriorityNormal, func(ctx context.Context, ex Execer) error {
		row := ex.QueryRow(ctx, `INSERT INTO short_counters(scope_id,series,next_n) VALUES(?,?,2)
			ON CONFLICT(scope_id,series) DO UPDATE SET next_n=next_n+1
			RETURNING next_n-1`, scopeID, series)
		if err := row.Scan(&n); err != nil {
			return fmt.Errorf("store: allocate %s code: %w", series, err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}
