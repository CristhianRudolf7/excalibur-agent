package attempts

// Outcome is the self-selection input for one attempt: only the
// attempt's own executable evidence, never the benchmark verifier.
type Outcome struct {
	// Index is the attempt number (0-based) in launch order.
	Index int
	// WorkDir is the isolated working copy the attempt ran in.
	WorkDir string
	// LogDir holds the attempt's own trial records (when present).
	LogDir string
	// State is the terminal run state (completed, failed, ...).
	State string
	// Summary is the attempt's final summary line.
	Summary string
	// Reason is the terminal reason (budget, work, ...).
	Reason string
	// Question carries the waiting question when State is waiting.
	Question string
	// ToolCalls counts all tool executions in the attempt.
	ToolCalls int64
	// ToolErrors counts tool executions that reported failure.
	ToolErrors int64
	// GateAttempts counts completion-gate evaluations.
	GateAttempts int
	// Verified counts successful executable checks (successful tool
	// results that can serve as verification evidence).
	Verified int64
	// DurationSecs is the wall time of the attempt.
	DurationSecs float64
	// ChangedCount counts the files (not directories) the attempt
	// changed, added or deleted relative to the baseline.
	ChangedCount int
	// FilesChanged lists relative paths that differ from the baseline
	// (computed for the record, capped).
	FilesChanged []string
	// DiffStat is the short diff summary for the record.
	DiffStat string
	// Deliverable lists the named outputs missing from the attempt's
	// copy (empty when they all exist or the checks are off).
	Deliverable []string
}

// Passes reports whether the attempt clears the hard checks: every
// named output exists and the attempt did not fail outright.
func (o Outcome) Passes() bool {
	return len(o.Deliverable) == 0 && o.State != "failed"
}

// selectionKey is the ordered comparison used for self-selection
// (Section 18.2). Fields compare in declaration order, larger wins:
//
//  1. passes: the deliverable checks pass and the attempt did not fail.
//  2. changed: the attempt produced changes. An attempt that reports
//     no changes never wins over one with changes that pass.
//  3. verified: more successful executable checks.
//  4. fewer changed files (the negated count).
//  5. completion: completed over completed_with_pending over the rest.
//  6. fewer tool errors (the negated count).
//
// Attempts that tie on every field keep launch order, unless the caller
// refines the tie (the cheap model comparison of evidence).
type selectionKey struct {
	passes     bool
	changed    bool
	verified   int64
	fewerFiles int64
	completion int
	fewerErr   int64
}

func keyOf(o Outcome) selectionKey {
	k := selectionKey{
		passes:     o.Passes(),
		changed:    o.ChangedCount > 0,
		verified:   o.Verified,
		fewerFiles: -int64(o.ChangedCount),
		fewerErr:   -o.ToolErrors,
	}
	switch o.State {
	case "completed":
		k.completion = 2
	case "completed_with_pending":
		k.completion = 1
	}
	return k
}

// compareOutcomes returns >0 when a outranks b on the deterministic
// fields, <0 when b outranks a, and 0 on a tie.
func compareOutcomes(a, b Outcome) int {
	ka, kb := keyOf(a), keyOf(b)
	for _, c := range []int{
		cmpBool(ka.passes, kb.passes),
		cmpBool(ka.changed, kb.changed),
		cmpInt(ka.verified, kb.verified),
		cmpInt(ka.fewerFiles, kb.fewerFiles),
		cmpInt(int64(ka.completion), int64(kb.completion)),
		cmpInt(ka.fewerErr, kb.fewerErr),
	} {
		if c != 0 {
			return c
		}
	}
	return 0
}

func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

func cmpInt(a, b int64) int {
	switch {
	case a > b:
		return 1
	case a < b:
		return -1
	default:
		return 0
	}
}

// SelectBest returns the slice position of the winning attempt under
// the deterministic rules (-1 when empty). Callers refine an exact tie
// with SelectBestWithCompare.
func SelectBest(outcomes []Outcome) int {
	return SelectBestWithCompare(outcomes, nil)
}

// SelectBestWithCompare returns the winning slice position. When
// several attempts tie on every deterministic field, compare breaks
// the tie: it receives two tied outcomes and returns negative when a
// wins, positive when b wins, zero to keep launch order. A nil compare
// keeps launch order.
func SelectBestWithCompare(outcomes []Outcome, compare func(a, b Outcome) int) int {
	if len(outcomes) == 0 {
		return -1
	}
	best := 0
	for i := 1; i < len(outcomes); i++ {
		c := compareOutcomes(outcomes[i], outcomes[best])
		if c > 0 {
			best = i
			continue
		}
		if c != 0 {
			continue
		}
		if compare != nil {
			if compare(outcomes[i], outcomes[best]) < 0 {
				best = i
			}
		} else if outcomes[i].Index < outcomes[best].Index {
			best = i
		}
	}
	return best
}

// Tied returns the slice positions tied with the winner on every
// deterministic field, the winner included (candidates for the cheap
// model comparison).
func Tied(outcomes []Outcome, winner int) []int {
	if winner < 0 || winner >= len(outcomes) {
		return nil
	}
	var out []int
	for i, o := range outcomes {
		if compareOutcomes(o, outcomes[winner]) == 0 {
			out = append(out, i)
		}
	}
	return out
}
