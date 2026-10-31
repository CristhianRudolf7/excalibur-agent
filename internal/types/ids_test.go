package types

import (
	"strings"
	"testing"
	"time"
)

func TestNewULIDShape(t *testing.T) {
	u := NewULID()
	if len(u) != ulidLen {
		t.Fatalf("bad ULID length %d", len(u))
	}
	for _, c := range u {
		if !strings.ContainsRune(crockford, c) {
			t.Fatalf("ULID %q contains non-Crockford char %q", u, c)
		}
	}
}

func TestULIDTimeRoundtrip(t *testing.T) {
	before := time.Now().UTC()
	u := NewULID()
	ts, err := ULIDTime(u)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Before(before.Add(-2*time.Second)) || ts.After(time.Now().UTC().Add(2*time.Second)) {
		t.Fatalf("ULID time %v out of range around %v", ts, before)
	}
}

func TestULIDOrdering(t *testing.T) {
	a := NewULID()
	time.Sleep(2 * time.Millisecond)
	b := NewULID()
	if !(a < b) {
		t.Fatalf("ULIDs not time-ordered: %q >= %q", a, b)
	}
}

func TestSplitAndValidate(t *testing.T) {
	id := NewID(PrefixTask)
	p, rest, err := SplitID(id)
	if err != nil {
		t.Fatal(err)
	}
	if p != PrefixTask || len(rest) != ulidLen {
		t.Fatalf("bad split: %q %q", p, rest)
	}
	if err := ValidateID(id, PrefixTask); err != nil {
		t.Fatal(err)
	}
	if err := ValidateID(id, PrefixRun); err == nil {
		t.Fatal("expected prefix mismatch error")
	}
	for _, bad := range []string{"", "tsk_", "xxx_01JABC", "tsk_short", "tsk_01JABC!@#________________"} {
		if _, _, err := SplitID(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestTypedIDs(t *testing.T) {
	if err := NewTaskID().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := NewRunID().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := TaskID(NewID(PrefixRun)).Validate(); err == nil {
		t.Fatal("expected cross-kind validation error")
	}
	ids := []string{
		string(NewProjectID()), string(NewConversationID()), string(NewMessageID()),
		string(NewTaskID()), string(NewRunID()), string(NewAgentID()),
		string(NewRequirementID()), string(NewCriterionID()), string(NewCheckID()),
		string(NewEvidenceID()), string(NewMemoryID()), string(NewSkillID()),
		string(NewJobID()),
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestShortCodes(t *testing.T) {
	r, err := ParseTaskRef("T45")
	if err != nil || r.Number() != 45 {
		t.Fatalf("ParseTaskRef: %v %q", err, r)
	}
	if FormatTaskRef(45) != "T45" {
		t.Fatal("FormatTaskRef")
	}
	rr, err := ParseRequirementRef("R12")
	if err != nil || rr.Number() != 12 {
		t.Fatalf("ParseRequirementRef: %v", err)
	}
	mr, err := ParseMilestoneRef("M2")
	if err != nil || mr.Number() != 2 {
		t.Fatalf("ParseMilestoneRef: %v", err)
	}
	cr, err := ParseCriterionRef("C12.3")
	if err != nil || cr.Requirement != 12 || cr.Criterion != 3 || cr.String() != "C12.3" {
		t.Fatalf("ParseCriterionRef: %v %+v", err, cr)
	}
	for _, bad := range []string{"T0", "T", "45", "t45", "T01", "R0", "C12", "C0.3", "C12.0", "M01"} {
		if _, err := ParseTaskRef(bad); bad[0] == 'T' && err == nil {
			t.Fatalf("expected error for task code %q", bad)
		}
		if _, err := ParseRequirementRef(bad); len(bad) > 0 && bad[0] == 'R' && err == nil {
			t.Fatalf("expected error for requirement code %q", bad)
		}
		if _, err := ParseCriterionRef(bad); len(bad) > 0 && bad[0] == 'C' && err == nil {
			t.Fatalf("expected error for criterion code %q", bad)
		}
		if _, err := ParseMilestoneRef(bad); len(bad) > 0 && bad[0] == 'M' && err == nil {
			t.Fatalf("expected error for milestone code %q", bad)
		}
	}
}
