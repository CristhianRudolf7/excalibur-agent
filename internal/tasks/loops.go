// Deterministic loop detector (Section 18.3).
//
// Signatures: h1 = hash(tool + normalized arguments), h2 = hash(h1 +
// result). Per execution:
//
//	R1 Identical repeat: the same h2 3 times in a row.
//	R2 Same error: the same h1 with error 3 times.
//	R3 Cycle: a cycle of 2 to 5 calls repeated 3 times.
//	R4 No progress: 8 steps without state changes (plan items, file
//	fingerprints, evidence, new information by content hash).
//	R5 Repeated text: a 50-character fragment repeated 10 times in
//	the streaming output.
//	R6 Edit mismatches: 3 consecutive fingerprint mismatches in one
//	file (13.6).
//
// Staged response:
//
//  1. First detection: a short corrective note describing the
//     pattern ("you ran cargo test 3 times with the same E0432;
//     change strategy").
//  2. Second: escalate the next step with an expert consult (19.3)
//     or switch strategy (e.g. an explorer investigating the cause).
//  3. Third: the run goes blocked with a report to its parent or
//     the master.
//
// Optional judge: from step 30, every 10 steps, the decider answers
// "is the agent stuck?". It only counts with probability >= 0.9, as
// in the Gemini CLI judge, which also confirms with a stronger
// model. The judge never stops the run alone: it only fires stage 1.
package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Loop thresholds (Appendix B, Section 18.3).
const (
	// LoopRepeatIdentical fires R1/R2 after this many repeats.
	LoopRepeatIdentical = 3
	// LoopCycleMin and LoopCycleMax bound the R3 cycle length.
	LoopCycleMin = 2
	LoopCycleMax = 5
	// LoopCycleRepeats fires R3 after this many cycle repetitions.
	LoopCycleRepeats = 3
	// LoopNoProgressSteps fires R4 after this many steps without a
	// state change.
	LoopNoProgressSteps = 8
	// LoopTextFragment is the R5 fragment length in characters.
	LoopTextFragment = 50
	// LoopTextRepeats fires R5 after this many fragment repeats.
	LoopTextRepeats = 10
	// LoopEditMismatches fires R6 after this many consecutive
	// fingerprint mismatches in one file.
	LoopEditMismatches = 3
	// StuckJudgeFromStep enables the optional judge from this step,
	// every StuckJudgeEvery steps, counting only at or above
	// StuckJudgeThreshold (Section 18.3).
	StuckJudgeFromStep  = 30
	StuckJudgeEvery     = 10
	StuckJudgeThreshold = 0.9
)

// LoopRule names a detector rule.
type LoopRule string

// Detector rules.
const (
	RuleIdenticalRepeat LoopRule = "identical_repeat"
	RuleSameError       LoopRule = "same_error"
	RuleCycle           LoopRule = "cycle"
	RuleNoProgress      LoopRule = "no_progress"
	RuleRepeatedText    LoopRule = "repeated_text"
	RuleEditMismatch    LoopRule = "edit_mismatch"
)

// LoopAction is the staged response: note, escalate, or block.
type LoopAction int

// Staged responses.
const (
	ActionNone LoopAction = iota
	ActionNote
	ActionEscalate
	ActionBlock
)

// Detection is one fired rule with its stage and note.
type Detection struct {
	Rule   LoopRule
	Stage  int
	Action LoopAction
	Note   string
}

// StageAction maps a per-rule detection count to the staged
// response: first note, second escalate, third blocked.
func StageAction(count int) LoopAction {
	switch {
	case count <= 0:
		return ActionNone
	case count == 1:
		return ActionNote
	case count == 2:
		return ActionEscalate
	default:
		return ActionBlock
	}
}

// ToolEvent is one observed tool call with its normalized outcome.
type ToolEvent struct {
	Tool   string
	Args   string
	Result string
	Error  string
}

// ProgressState is the observable run state R4 watches: plan items,
// file fingerprints, evidence and new information, each as a content
// hash supplied by the caller.
type ProgressState struct {
	Plans string
	Files string
	Proof string
	Info  string
}

// hash returns the hex sha256 of the joined parts.
func hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// LoopDetector is the per-execution deterministic detector. It holds
// no goroutines and no model calls: the engine feeds it every tool
// event, progress hash, text chunk and edit mismatch, and turns the
// detections into notes, escalations and blocked transitions.
// Long-poll tools (wait_for by default) never fire: legitimately
// repetitive polling must not stop the run (Section 18.5).
type LoopDetector struct {
	// LongPoll names the tools exempt from loop detection: their
	// events still count steps but never fire a rule.
	LongPoll   map[string]bool
	h2Window   []string
	h1Errors   map[string]int
	h1Window   []string
	progress   []string
	fragments  map[string]int
	mismatch   map[string]int
	stages     map[LoopRule]int
	judgeNotes int
	steps      int
}

// NewLoopDetector returns a detector with empty windows. wait_for is
// exempt by default (Section 18.5); the engine may add its own
// long-poll tools.
func NewLoopDetector() *LoopDetector {
	return &LoopDetector{
		LongPoll:  map[string]bool{"wait_for": true},
		h1Errors:  map[string]int{},
		fragments: map[string]int{},
		mismatch:  map[string]int{},
		stages:    map[LoopRule]int{},
	}
}

// ObserveTool feeds one tool call outcome and returns the detections
// it fires (R1, R2, R3). Long-poll tools are exempt.
func (d *LoopDetector) ObserveTool(ev ToolEvent) []Detection {
	d.steps++
	if d.LongPoll[ev.Tool] {
		return nil
	}
	h1 := hash(ev.Tool, ev.Args)
	h2 := hash(h1, ev.Result, ev.Error)
	d.h1Window = append(d.h1Window, h1)
	d.h2Window = append(d.h2Window, h2)
	if ev.Error != "" {
		d.h1Errors[h1]++
	} else {
		d.h1Errors[h1] = 0
	}
	var out []Detection
	// R1: same h2 LoopRepeatIdentical times in a row.
	if len(d.h2Window) >= LoopRepeatIdentical && allEqual(d.h2Window[len(d.h2Window)-LoopRepeatIdentical:]) {
		out = append(out, d.fire(RuleIdenticalRepeat,
			fmt.Sprintf("ran %s %s %d times with the identical result; change strategy", ev.Tool, shorten(ev.Args), LoopRepeatIdentical)))
	}
	// R2: same h1 with error LoopRepeatIdentical times.
	if ev.Error != "" && d.h1Errors[h1] >= LoopRepeatIdentical && d.h1Errors[h1]%LoopRepeatIdentical == 0 {
		out = append(out, d.fire(RuleSameError,
			fmt.Sprintf("ran %s %s with error %d times (%s); change strategy", ev.Tool, shorten(ev.Args), d.h1Errors[h1], shorten(ev.Error))))
	}
	// R3: a cycle of LoopCycleMin..LoopCycleMax calls repeated
	// LoopCycleRepeats times.
	if n, reps := detectCycle(d.h1Window); reps >= LoopCycleRepeats {
		out = append(out, d.fire(RuleCycle,
			fmt.Sprintf("repeating a cycle of %d calls %d times; break the loop", n, reps)))
		d.h1Window = nil // avoid refiring on the same window
	}
	return out
}

// ObserveProgress feeds one step state hash and fires R4 after
// LoopNoProgressSteps steps without a change.
func (d *LoopDetector) ObserveProgress(st ProgressState) []Detection {
	d.steps++
	d.progress = append(d.progress, hash(st.Plans, st.Files, st.Proof, st.Info))
	if len(d.progress) > LoopNoProgressSteps {
		d.progress = d.progress[len(d.progress)-LoopNoProgressSteps:]
	}
	if len(d.progress) == LoopNoProgressSteps && allEqual(d.progress) {
		d.progress = nil
		return []Detection{d.fire(RuleNoProgress,
			fmt.Sprintf("no state change in %d steps (plan, files, evidence, info); change strategy", LoopNoProgressSteps))}
	}
	return nil
}

// ObserveText feeds one streaming output chunk and fires R5 when a
// LoopTextFragment-character fragment repeats LoopTextRepeats times.
// The fragment table is bounded: past 65536 entries it resets, so a
// very long stream only loses old counts.
func (d *LoopDetector) ObserveText(chunk string) []Detection {
	if len(d.fragments) > 65536 {
		d.fragments = map[string]int{}
	}
	for _, frag := range fragmentsOf(chunk, LoopTextFragment) {
		d.fragments[frag]++
		if d.fragments[frag] == LoopTextRepeats {
			note := fmt.Sprintf("repeating the same %d-character output %d times; change strategy", LoopTextFragment, LoopTextRepeats)
			return []Detection{d.fire(RuleRepeatedText, note)}
		}
	}
	return nil
}

// ObserveEditMismatch feeds one file fingerprint mismatch outcome
// for path and fires R6 after LoopEditMismatches consecutive
// mismatches in that file. A match resets the file counter.
func (d *LoopDetector) ObserveEditMismatch(path string, mismatch bool) []Detection {
	if !mismatch {
		d.mismatch[path] = 0
		return nil
	}
	d.mismatch[path]++
	if d.mismatch[path] >= LoopEditMismatches && (d.mismatch[path]-LoopEditMismatches)%LoopEditMismatches == 0 {
		return []Detection{d.fire(RuleEditMismatch,
			fmt.Sprintf("fingerprint mismatch %d times in a row in %s; re-read the file before editing", d.mismatch[path], path))}
	}
	return nil
}

// JudgeFunc answers "is the agent stuck?" with a probability. Only
// answers at or above StuckJudgeThreshold count, and only from step
// StuckJudgeFromStep every StuckJudgeEvery steps.
type JudgeFunc func(step int) (stuck bool, probability float64)

// MaybeJudge runs the optional stuck judge: it never blocks the run
// alone and only ever fires the stage-1 note (Section 18.3).
func (d *LoopDetector) MaybeJudge(judge JudgeFunc) []Detection {
	if d.steps < StuckJudgeFromStep || d.steps%StuckJudgeEvery != 0 {
		return nil
	}
	stuck, p := judge(d.steps)
	if !stuck || p < StuckJudgeThreshold {
		return nil
	}
	// The judge only triggers stage 1: it never touches the
	// per-rule escalation counters, so it cannot block the run.
	d.judgeNotes++
	return []Detection{{
		Rule:   RuleNoProgress,
		Stage:  1,
		Action: ActionNote,
		Note:   fmt.Sprintf("stuck judge suspects no progress at step %d (p=%.2f); change strategy", d.steps, p),
	}}
}

// Counts returns the per-rule detection counts (for tests and
// telemetry).
func (d *LoopDetector) Counts() map[LoopRule]int {
	out := map[LoopRule]int{}
	for r, n := range d.stages {
		out[r] = n
	}
	return out
}

func (d *LoopDetector) fire(rule LoopRule, note string) Detection {
	d.stages[rule]++
	n := d.stages[rule]
	return Detection{Rule: rule, Stage: n, Action: StageAction(n), Note: note}
}

func allEqual(w []string) bool {
	for _, s := range w[1:] {
		if s != w[0] {
			return false
		}
	}
	return true
}

// detectCycle finds a trailing repetition: a block of n calls
// (LoopCycleMin..LoopCycleMax) repeated reps times ending at the
// window end. The block must hold at least two distinct calls: an
// all-identical repetition is R1's domain, which stays sensitive to
// the result. It returns the block length and repetition count.
func detectCycle(w []string) (int, int) {
	for n := LoopCycleMin; n <= LoopCycleMax; n++ {
		if len(w) < n*LoopCycleRepeats {
			continue
		}
		base := w[len(w)-n:]
		if !distinct(base) {
			continue
		}
		reps := 1
		for len(w) >= (reps+1)*n {
			prev := w[len(w)-(reps+1)*n : len(w)-reps*n]
			if !equalSlice(prev, base) {
				break
			}
			reps++
		}
		if reps >= LoopCycleRepeats {
			return n, reps
		}
	}
	return 0, 0
}

func distinct(in []string) bool {
	seen := map[string]bool{}
	for _, s := range in {
		seen[s] = true
	}
	return len(seen) >= 2
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fragmentsOf slides a LoopTextFragment window over the chunk. To
// bound memory the detector keeps counts per fragment; callers with
// very long streams should construct one detector per execution,
// which the engine already does.
func fragmentsOf(chunk string, n int) []string {
	runes := []rune(chunk)
	if len(runes) < n {
		return nil
	}
	var out []string
	for i := 0; i+n <= len(runes); i++ {
		out = append(out, string(runes[i:i+n]))
	}
	// Deduplicate within the chunk: one chunk votes once per frame.
	sort.Strings(out)
	return dedup(out)
}

func dedup(in []string) []string {
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}

func shorten(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:77] + "..."
	}
	if s == "" {
		return "(no args)"
	}
	return s
}
