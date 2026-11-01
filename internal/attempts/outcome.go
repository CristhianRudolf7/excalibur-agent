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
	// FilesChanged lists relative paths differing from the original
	// workdir (computed for the record, capped).
	FilesChanged []string
	// DiffStat is the short diff summary for the record.
	DiffStat string
}

// rank orders outcomes for self-selection (Section 18.2): the attempt
// whose own executable checks pass wins; ties break towards more
// verified checks, then fewer errors, then the deterministic launch
// order (the cheap model comparison of evidence refines only this
// final tie in the caller).
func rank(o Outcome) (completed, verified int64, negErrors int64) {
	if o.State == "completed" {
		completed = 2
	} else if o.State == "completed_with_pending" {
		completed = 1
	}
	return completed, o.Verified, -o.ToolErrors
}

// better reports whether a outranks b under the deterministic rules.
func better(a, b Outcome) bool {
	ca, va, ea := rank(a)
	cb, vb, eb := rank(b)
	if ca != cb {
		return ca > cb
	}
	if va != vb {
		return va > vb
	}
	if ea != eb {
		return ea > eb
	}
	return a.Index < b.Index
}

// SelectBest returns the slice position of the winning attempt under
// the deterministic rules (-1 when empty). Callers refine an exact tie
// with SelectBestWithCompare.
func SelectBest(outcomes []Outcome) int {
	return SelectBestWithCompare(outcomes, nil)
}

// SelectBestWithCompare returns the winning slice position. When
// several attempts tie on every deterministic signal (completion,
// verified checks, errors), compare breaks the tie: it receives two
// tied outcomes and returns negative when a wins, positive when b
// wins, zero to keep launch order. A nil compare keeps launch order.
func SelectBestWithCompare(outcomes []Outcome, compare func(a, b Outcome) int) int {
	if len(outcomes) == 0 {
		return -1
	}
	best := 0
	for i := 1; i < len(outcomes); i++ {
		if deterministicTie(outcomes[i], outcomes[best]) {
			if compare != nil {
				if compare(outcomes[i], outcomes[best]) < 0 {
					best = i
				}
			} else if outcomes[i].Index < outcomes[best].Index {
				best = i
			}
			continue
		}
		if better(outcomes[i], outcomes[best]) {
			best = i
		}
	}
	return best
}

// deterministicTie reports whether two outcomes tie on every signal
// except launch order (the cheap model comparison of evidence refines
// exactly this case).
func deterministicTie(a, b Outcome) bool {
	ca, va, ea := rank(a)
	cb, vb, eb := rank(b)
	return ca == cb && va == vb && ea == eb
}

// Tied returns the slice positions tied with the winner on every
// deterministic signal (candidates for the cheap model comparison).
func Tied(outcomes []Outcome, winner int) []int {
	if winner < 0 || winner >= len(outcomes) {
		return nil
	}
	var out []int
	wc, wv, we := rank(outcomes[winner])
	for i, o := range outcomes {
		c, v, e := rank(o)
		if c == wc && v == wv && e == we {
			out = append(out, i)
		}
	}
	return out
}
