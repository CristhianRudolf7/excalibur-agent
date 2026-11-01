package attempts

import (
	"testing"
)

// TestSelectBestPrefersCompleted: with equal verified checks and no
// changes, a completed attempt beats one completed with pending items.
// (Before this change completion outranked verified checks; the
// selection order now follows the deliverable, change, verified order
// and completion is a later tie-break. Documented in the status file.)
func TestSelectBestPrefersCompleted(t *testing.T) {
	outs := []Outcome{
		{Index: 0, State: "failed", Verified: 10},
		{Index: 1, State: "completed_with_pending", Verified: 5},
		{Index: 2, State: "completed", Verified: 5},
	}
	if got := SelectBest(outs); got != 2 {
		t.Fatalf("winner = %d, want 2 (completed breaks the tie)", got)
	}
}

func TestSelectBestDeliverableFirst(t *testing.T) {
	outs := []Outcome{
		// Many verified checks but the named output is missing.
		{Index: 0, State: "completed", Verified: 50, ChangedCount: 3, Deliverable: []string{"required artifact missing: /app/out.txt"}},
		// Fewer checks, the deliverable exists.
		{Index: 1, State: "completed", Verified: 2, ChangedCount: 1},
	}
	if got := SelectBest(outs); got != 1 {
		t.Fatalf("winner = %d, want 1 (deliverable checks decide first)", got)
	}
}

func TestSelectBestFailedAttemptNeverPassesWithoutDeliverables(t *testing.T) {
	outs := []Outcome{
		{Index: 0, State: "failed", Verified: 9, ChangedCount: 4},
		{Index: 1, State: "paused", Verified: 1, ChangedCount: 1},
	}
	if got := SelectBest(outs); got != 1 {
		t.Fatalf("winner = %d, want 1 (a failed attempt does not pass)", got)
	}
}

// TestSelectBestNoChangeNeverWinsOverPassingChanges is the rule from
// the task: an attempt that reports no changes never wins over one with
// changes that pass, even when it verified more checks.
func TestSelectBestNoChangeNeverWinsOverPassingChanges(t *testing.T) {
	outs := []Outcome{
		{Index: 0, State: "completed", Verified: 40, ChangedCount: 0},
		{Index: 1, State: "completed_with_pending", Verified: 1, ChangedCount: 2},
	}
	if got := SelectBest(outs); got != 1 {
		t.Fatalf("winner = %d, want 1 (changes that pass beat no changes)", got)
	}
}

func TestSelectBestTieBreakVerifiedThenFewerFiles(t *testing.T) {
	outs := []Outcome{
		{Index: 0, State: "completed", Verified: 5, ChangedCount: 4},
		{Index: 1, State: "completed", Verified: 5, ChangedCount: 1},
		{Index: 2, State: "completed", Verified: 3, ChangedCount: 0},
	}
	if got := SelectBest(outs); got != 1 {
		t.Fatalf("winner = %d, want 1 (same verified, fewer changed files)", got)
	}
}

func TestSelectBestTieBreakVerifiedThenErrors(t *testing.T) {
	outs := []Outcome{
		{Index: 0, State: "completed", Verified: 2, ToolErrors: 0},
		{Index: 1, State: "completed", Verified: 5, ToolErrors: 3},
		{Index: 2, State: "completed", Verified: 5, ToolErrors: 0},
	}
	if got := SelectBest(outs); got != 2 {
		t.Fatalf("winner = %d, want 2 (more verified, fewer errors)", got)
	}
}

func TestSelectBestDeterministicFinalTie(t *testing.T) {
	outs := []Outcome{
		{Index: 0, State: "completed", Verified: 3},
		{Index: 1, State: "completed", Verified: 3},
	}
	if got := SelectBest(outs); got != 0 {
		t.Fatalf("exact tie keeps launch order, got %d", got)
	}
	// A compare hook can still refine the final tie (the cheap model
	// comparison of evidence plugs in here).
	rev := func(a, b Outcome) int {
		if a.Index > b.Index {
			return -1
		}
		return 1
	}
	if got := SelectBestWithCompare(outs, rev); got != 1 {
		t.Fatalf("compare hook winner = %d, want 1", got)
	}
}

func TestTiedListsDeterministicTies(t *testing.T) {
	outs := []Outcome{
		{Index: 0, State: "completed", Verified: 3},
		{Index: 1, State: "completed", Verified: 3},
		{Index: 2, State: "completed", Verified: 1},
	}
	tied := Tied(outs, 0)
	if len(tied) != 2 || tied[0] != 0 || tied[1] != 1 {
		t.Fatalf("tied = %v, want [0 1]", tied)
	}
}
