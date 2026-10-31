// Promotion tests (Section 8.1, verification row 8.11.4): tasks
// that touch a project, exceed 15 steps, or run past a day promote
// N1 to N2 to N3; the user can force any level.
package tasks

import (
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestPromoteN1StaysN1(t *testing.T) {
	if got := Promote(PromotionInput{Level: types.TaskN1, Steps: 3}); got != types.TaskN1 {
		t.Fatalf("short untouched task promoted to %q", got)
	}
}

func TestPromoteN1ToN2OnProjectTouch(t *testing.T) {
	got := Promote(PromotionInput{Level: types.TaskN1, TouchesProject: true, RegisteredProject: true})
	if got != types.TaskN2 {
		t.Fatalf("project-touching task stayed %q", got)
	}
}

func TestPromoteN1ToN2Over15Steps(t *testing.T) {
	if got := Promote(PromotionInput{Level: types.TaskN1, Steps: 16}); got != types.TaskN2 {
		t.Fatalf("16-step task stayed %q", got)
	}
	if got := Promote(PromotionInput{Level: types.TaskN1, Steps: 15}); got != types.TaskN1 {
		t.Fatalf("15-step task promoted to %q", got)
	}
}

func TestPromoteN2ToN3(t *testing.T) {
	cases := []PromotionInput{
		{Level: types.TaskN2, Estimate: 25 * time.Hour},
		{Level: types.TaskN2, Milestones: 2},
		{Level: types.TaskN2, UserAskedN3: true},
	}
	for i, in := range cases {
		if got := Promote(in); got != types.TaskN3 {
			t.Fatalf("case %d: expected N3, got %q", i, got)
		}
	}
	if got := Promote(PromotionInput{Level: types.TaskN2, Estimate: time.Hour, Milestones: 1}); got != types.TaskN2 {
		t.Fatalf("small N2 promoted to %q", got)
	}
}

func TestPromoteForcedWins(t *testing.T) {
	n1 := types.TaskN1
	in := PromotionInput{Level: types.TaskN2, Estimate: 72 * time.Hour, Milestones: 5, Forced: &n1}
	if got := Promote(in); got != types.TaskN1 {
		t.Fatalf("forced level lost: got %q", got)
	}
}

func TestNeedsProject(t *testing.T) {
	if NeedsProject(types.TaskN1) {
		t.Fatal("N1 must not require a project")
	}
	if !NeedsProject(types.TaskN2) || !NeedsProject(types.TaskN3) {
		t.Fatal("N2/N3 require a registered project")
	}
}
