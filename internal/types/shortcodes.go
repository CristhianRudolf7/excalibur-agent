package types

import (
	"fmt"
	"strconv"
	"strings"
)

// Short human- and model-facing codes, stable per project: T45 (task),
// R12 (requirement), C12.3 (criterion 3 of requirement 12), M2
// (milestone). Allocated with a per-project counter and never reused,
// not even after deletion (Section 4.4).

// TaskRef is a short task code such as "T45".
type TaskRef string

// ParseTaskRef parses a short task code.
func ParseTaskRef(s string) (TaskRef, error) {
	n, ok := cutCodePrefix(s, "T")
	if !ok || n < 1 {
		return "", fmt.Errorf("types: bad task code %q: want T<number>", s)
	}
	return TaskRef(s), nil
}

// Number returns the numeric part of the task code.
func (r TaskRef) Number() int {
	n, _ := strconv.Atoi(string(r)[1:])
	return n
}

// RequirementRef is a short requirement code such as "R12".
type RequirementRef string

// ParseRequirementRef parses a short requirement code.
func ParseRequirementRef(s string) (RequirementRef, error) {
	n, ok := cutCodePrefix(s, "R")
	if !ok || n < 1 {
		return "", fmt.Errorf("types: bad requirement code %q: want R<number>", s)
	}
	return RequirementRef(s), nil
}

// Number returns the numeric part of the requirement code.
func (r RequirementRef) Number() int {
	n, _ := strconv.Atoi(string(r)[1:])
	return n
}

// MilestoneRef is a short milestone code such as "M2".
type MilestoneRef string

// ParseMilestoneRef parses a short milestone code.
func ParseMilestoneRef(s string) (MilestoneRef, error) {
	n, ok := cutCodePrefix(s, "M")
	if !ok || n < 1 {
		return "", fmt.Errorf("types: bad milestone code %q: want M<number>", s)
	}
	return MilestoneRef(s), nil
}

// Number returns the numeric part of the milestone code.
func (r MilestoneRef) Number() int {
	n, _ := strconv.Atoi(string(r)[1:])
	return n
}

// CriterionRef is a short criterion code such as "C12.3".
type CriterionRef struct {
	Requirement int
	Criterion   int
}

// ParseCriterionRef parses a short criterion code.
func ParseCriterionRef(s string) (CriterionRef, error) {
	rest, ok := strings.CutPrefix(s, "C")
	if !ok {
		return CriterionRef{}, fmt.Errorf("types: bad criterion code %q: want C<req>.<n>", s)
	}
	req, num, ok := strings.Cut(rest, ".")
	r, err1 := strconv.Atoi(req)
	c, err2 := strconv.Atoi(num)
	if !ok || err1 != nil || err2 != nil || r < 1 || c < 1 {
		return CriterionRef{}, fmt.Errorf("types: bad criterion code %q: want C<req>.<n>", s)
	}
	return CriterionRef{Requirement: r, Criterion: c}, nil
}

// String renders the canonical "C<req>.<n>" form.
func (r CriterionRef) String() string {
	return "C" + strconv.Itoa(r.Requirement) + "." + strconv.Itoa(r.Criterion)
}

// FormatTaskRef renders a task code.
func FormatTaskRef(n int) TaskRef { return TaskRef("T" + strconv.Itoa(n)) }

// FormatRequirementRef renders a requirement code.
func FormatRequirementRef(n int) RequirementRef {
	return RequirementRef("R" + strconv.Itoa(n))
}

// FormatMilestoneRef renders a milestone code.
func FormatMilestoneRef(n int) MilestoneRef { return MilestoneRef("M" + strconv.Itoa(n)) }

func cutCodePrefix(s, prefix string) (int, bool) {
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok || rest == "" {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 {
		return 0, false
	}
	// Reject zero-padded or non-canonical spellings so codes compare
	// by string equality.
	if rest != strconv.Itoa(n) {
		return 0, false
	}
	return n, true
}
