package agenda

import (
	"testing"
	"time"
)

// 21.8, 21.9, 17.6 and the tool surface.

func TestProposalApproval(t *testing.T) {
	policy := DefaultCuriosityPolicy()
	cheap := NewProposal("p1", "grids", "survey storage", SourceInterest, "mem_1", 0.5)
	if err := cheap.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(cheap.Criteria) == 0 {
		t.Fatal("default validation criteria required")
	}
	if got := cheap.Review(policy); got != ProposalPreapproved || !cheap.Runnable() {
		t.Fatalf("under-budget preapproves: %s", got)
	}
	big := NewProposal("p2", "fusion", "survey all", SourceContradiction, "edge_1", 50)
	if got := big.Review(policy); got != ProposalPending || big.Runnable() {
		t.Fatalf("over-budget waits for the user: %s", got)
	}
	big.Approve()
	if !big.Runnable() {
		t.Fatal("explicit approval runs")
	}
	big.Reject()
	if big.Runnable() {
		t.Fatal("rejected never runs")
	}
	off := DefaultCuriosityPolicy()
	off.Enabled = false
	other := NewProposal("p3", "t", "o", SourceOpenQuestion, "q", 0.1)
	if got := other.Review(off); got != ProposalRejected {
		t.Fatalf("disabled curiosity rejects: %s", got)
	}
	for _, bad := range []*Proposal{
		{ID: "b1", Objective: "o", BudgetUSD: 1, Criteria: []string{"c"}, Source: SourceInterest},
		{ID: "b2", Topic: "t", BudgetUSD: 0, Criteria: []string{"c"}, Source: SourceInterest},
		{ID: "b3", Topic: "t", Objective: "o", BudgetUSD: 1, Source: "nope"},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("invalid proposal passes: %+v", bad)
		}
	}
}

func TestInternalJobsContract(t *testing.T) {
	jobs := DefaultInternalJobs()
	if err := ValidateInternalJobs(jobs); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, j := range jobs {
		if j.Lane != "P3" {
			t.Fatalf("%s must run in P3", j.ID)
		}
		// Jobs yield to user activity even inside the window.
		if j.ShouldRun(now, true, true) {
			t.Fatalf("%s must yield to the user", j.ID)
		}
	}
	windowOnly := 0
	for _, j := range jobs {
		if j.WindowOnly {
			windowOnly++
			if j.ShouldRun(now, false, false) {
				t.Fatalf("%s window-only must wait for the window", j.ID)
			}
			if !j.ShouldRun(now, true, false) {
				t.Fatalf("%s must fire in its window", j.ID)
			}
		} else if !j.ShouldRun(now, false, false) {
			t.Fatalf("%s must fire outside windows when idle", j.ID)
		}
	}
	if windowOnly == 0 {
		t.Fatal("some jobs must be window-only")
	}
}

func TestSessionWatchdog(t *testing.T) {
	now := time.Now().UTC()
	running := &Session{ID: "s1", State: SessionRunning, Heartbeat: now.Add(-11 * time.Minute)}
	if !running.Stuck(now) {
		t.Fatal("running without heartbeat for 10+ min is stuck")
	}
	fresh := &Session{ID: "s2", State: SessionRunning, Heartbeat: now}
	fresh.Beat(now)
	if fresh.Stuck(now) {
		t.Fatal("fresh heartbeat is not stuck")
	}
	long := &Session{ID: "s3", State: SessionRunning, Heartbeat: now.Add(-time.Hour), LongTool: "train"}
	if long.Stuck(now) {
		t.Fatal("registered long tool is exempt from the watchdog")
	}
	idle := &Session{ID: "s4", State: SessionScheduled}
	if idle.Stuck(now) {
		t.Fatal("non-running sessions are not watched")
	}
}

func TestSessionChain(t *testing.T) {
	now := time.Now().UTC()
	c := &SessionChain{Initiative: "n3",
		Sessions: []*Session{
			{ID: "m1", State: SessionDone},
			{ID: "m2", State: SessionScheduled},
			{ID: "m3", State: SessionScheduled},
		}}
	if got := c.Next(); got == nil || got.ID != "m2" {
		t.Fatalf("chain order: %+v", got)
	}
	c.Sessions[1].State = SessionRunning
	if err := c.HandOff("m2", "notes", now); err != nil {
		t.Fatal(err)
	}
	if got := c.Next(); got == nil || got.ID != "m3" {
		t.Fatalf("handoff unblocks the next session: %+v", got)
	}
	if err := c.HandOff("nope", "x", now); err == nil {
		t.Fatal("unknown session must fail")
	}
}

func TestToolSurface(t *testing.T) {
	if len(AllTools()) != 5 {
		t.Fatalf("five agenda tools: %v", AllTools())
	}
	for _, name := range AllTools() {
		if d := ToolDescription(name); d == "" {
			t.Fatalf("tool %q needs a short description", name)
		}
	}
	if ToolDescription("nope") != "" {
		t.Fatal("unknown tool has no description")
	}
}
