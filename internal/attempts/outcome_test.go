package attempts

import (
	"testing"
)

func TestSelectBestPrefersCompleted(t *testing.T) {
	outs := []Outcome{
		{Index: 0, State: "failed", Verified: 10},
		{Index: 1, State: "completed", Verified: 1},
		{Index: 2, State: "completed_with_pending", Verified: 20},
	}
	if got := SelectBest(outs); got != 1 {
		t.Fatalf("winner = %d, want 1 (completed first)", got)
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
