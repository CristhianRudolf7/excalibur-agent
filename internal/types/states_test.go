package types

import "testing"

func TestCanTransition(t *testing.T) {
	allowed := [][2]RunState{
		{RunCreated, RunQueued}, {RunCreated, RunCanceled},
		{RunQueued, RunRunning}, {RunQueued, RunPaused}, {RunQueued, RunCanceled},
		{RunRunning, RunWaiting}, {RunRunning, RunVerifying}, {RunRunning, RunPaused},
		{RunRunning, RunBlocked}, {RunRunning, RunFailed}, {RunRunning, RunCanceled},
		{RunRunning, RunRecovering},
		{RunWaiting, RunRunning}, {RunWaiting, RunPaused}, {RunWaiting, RunBlocked},
		{RunWaiting, RunCanceled}, {RunWaiting, RunRecovering},
		{RunVerifying, RunRunning}, {RunVerifying, RunCompleted},
		{RunVerifying, RunCompletedPending}, {RunVerifying, RunBlocked},
		{RunPaused, RunQueued}, {RunPaused, RunCanceled},
		{RunBlocked, RunQueued}, {RunBlocked, RunCanceled},
		{RunRecovering, RunQueued}, {RunRecovering, RunPaused}, {RunRecovering, RunBlocked},
	}
	for _, a := range allowed {
		if !CanTransition(a[0], a[1]) {
			t.Errorf("expected transition %s -> %s to be allowed", a[0], a[1])
		}
	}
	denied := [][2]RunState{
		{RunCreated, RunRunning},
		{RunQueued, RunCompleted},
		{RunRunning, RunCreated}, {RunRunning, RunCompleted},
		{RunPaused, RunRunning},
		{RunBlocked, RunRunning},
		{RunCompleted, RunQueued}, {RunFailed, RunQueued}, {RunCanceled, RunQueued},
		{RunRecovering, RunRunning},
		{RunVerifying, RunCanceled}, {RunVerifying, RunPaused},
	}
	for _, d := range denied {
		if CanTransition(d[0], d[1]) {
			t.Errorf("expected transition %s -> %s to be denied", d[0], d[1])
		}
	}
}

func TestTerminalStates(t *testing.T) {
	for _, s := range []RunState{RunCompleted, RunCompletedPending, RunFailed, RunCanceled} {
		if !s.IsTerminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	for _, s := range []RunState{RunCreated, RunQueued, RunRunning, RunWaiting, RunVerifying, RunPaused, RunBlocked, RunRecovering} {
		if s.IsTerminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

func TestDeriveTaskState(t *testing.T) {
	cases := map[RunState]TaskState{
		RunCreated: TaskOpen,
		RunQueued:  TaskInProgress, RunRunning: TaskInProgress,
		RunWaiting: TaskInProgress, RunVerifying: TaskInProgress,
		RunRecovering: TaskInProgress,
		RunPaused:     TaskPaused, RunBlocked: TaskBlocked,
		RunCompleted: TaskCompleted, RunCompletedPending: TaskCompletedPending,
		RunFailed: TaskFailed, RunCanceled: TaskCanceled,
	}
	for run, want := range cases {
		if got := DeriveTaskState(run); got != want {
			t.Errorf("DeriveTaskState(%s) = %s, want %s", run, got, want)
		}
	}
}

func TestComputeCriterionState(t *testing.T) {
	cases := []struct {
		name string
		in   CriterionEvidence
		want CriterionState
	}{
		{"no evidence", CriterionEvidence{}, CriterionUnverified},
		{"stale evidence", CriterionEvidence{Present: true}, CriterionUnverified},
		{"fresh pass", CriterionEvidence{Present: true, Fresh: true, Passed: true}, CriterionSatisfied},
		{"fresh fail", CriterionEvidence{Present: true, Fresh: true}, CriterionFailed},
		{"waived with justification", CriterionEvidence{NotApplicable: true, Justified: true}, CriterionNotApplicable},
		{"waived without justification", CriterionEvidence{NotApplicable: true}, CriterionUnverified},
	}
	for _, c := range cases {
		if got := ComputeCriterionState(c.in); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestScopeParseRoundtrip(t *testing.T) {
	for _, s := range []string{"general", "project:prj_x", "conversation:cnv_y", "task:tsk_z", "app:sheet1"} {
		sc, err := ParseScope(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := sc.Validate(); err != nil {
			t.Fatal(err)
		}
		if sc.String() != s {
			t.Fatalf("roundtrip: got %q want %q", sc.String(), s)
		}
	}
	for _, bad := range []string{"", "project:", "unknown:x", "project", "general:x"} {
		if _, err := ParseScope(bad); err == nil {
			t.Fatalf("expected error for scope %q", bad)
		}
	}
	ss := ScopeSet{{Kind: ScopeGeneral}, {Kind: ScopeProject, ID: "p"}}
	if !ss.Contains(GeneralScope()) || ss.Contains(Scope{Kind: ScopeTask, ID: "t"}) {
		t.Fatal("ScopeSet.Contains")
	}
	if err := (ScopeSet{{Kind: ScopeKind("bogus")}}).Validate(); err == nil {
		t.Fatal("expected scope validation error")
	}
}

func TestCursorRoundtrip(t *testing.T) {
	for _, seq := range []uint64{0, 1, 123456789} {
		s := Cursor{Seq: seq}.String()
		got, err := ParseCursor(s)
		if err != nil || got.Seq != seq {
			t.Fatalf("cursor roundtrip %d: %v %+v", seq, err, got)
		}
	}
	if _, err := ParseCursor("!!!not-base64!!!"); err == nil {
		t.Fatal("expected cursor parse error")
	}
	if c, err := ParseCursor(""); err != nil || c.Seq != 0 {
		t.Fatal("empty cursor should be zero")
	}
}
