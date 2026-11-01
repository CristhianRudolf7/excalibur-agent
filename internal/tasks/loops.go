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
//  3. Third and later: a further escalation. No detection ends the
//     run: a loop is a reason to change approach, never a terminal
//     state while budget remains (agent quality, backlog 99).
//
// Exact-call guard (agent quality loop guard v2, see ExactCallGuard):
// one exact call (tool, normalized arguments, fingerprint of its
// result) counted with no workspace change in between. The third
// occurrence adds a note; the sixth refuses that exact call for the
// next ExactCallBanTurns turns. It supersedes R1 when the engine
// enables it (LoopDetector.ExactRepeatsByGuard).
//
// Optional judge: from step 30, every 10 steps, the decider answers
// "is the agent stuck?". It only counts with probability >= 0.9, as
// in the Gemini CLI judge, which also confirms with a stronger
// model. The judge never stops the run alone: it only fires stage 1.
package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
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
	// LoopNearRepeat fires the near-repeat rule after this many
	// consecutive same-tool calls with similar (not necessarily
	// identical) arguments. It closes the gap R1/R2 leave open:
	// the same command polled with varying output (exit codes,
	// process listings, timestamps) never repeats an h1/h2 hash,
	// so exact-match rules stay silent while the agent stalls.
	LoopNearRepeat = 3
	// LoopNoInfoSteps fires the no-information rule after this many
	// consecutive tool steps that produced no new information (all
	// errors, all empty, or all identical to the previous output of
	// the same tool). Shorter than R4's window so the recovery
	// prompt lands while the run still has budget to act.
	LoopNoInfoSteps = 6
	// LoopArgCompareCap bounds the argument bytes compared by the
	// near-repeat similarity: longer payloads compare on the head.
	LoopArgCompareCap = 500
	// LoopNearSimilarity is the minimum normalized similarity
	// (1 - lev/maxlen over whitespace-collapsed arguments) for two
	// same-tool argument strings to count as near-identical.
	LoopNearSimilarity = 0.8
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
	// RuleNearRepeat fires on consecutive same-tool calls with
	// near-identical arguments whose outputs keep varying, so the
	// exact-match R1/R2 rules never trigger (agent-quality loop
	// breaker; staged like every other rule).
	RuleNearRepeat LoopRule = "near_repeat"
	// RuleNoInfo fires after LoopNoInfoSteps consecutive tool steps
	// with no new information (agent-quality loop breaker; staged
	// like every other rule).
	RuleNoInfo LoopRule = "no_info"
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
	// nearTool and nearArgs track the trailing run of consecutive
	// same-tool calls for the near-repeat rule: the tool name and
	// the whitespace-collapsed argument of each call in the run.
	nearTool string
	nearArgs []string
	// dull counts consecutive tool steps with no new information
	// (fed by ObserveNoInfo): all errors, all empty, or all outputs
	// identical to the previous output of the same tool.
	dull int
	// ExactRepeatsByGuard hands the identical-repeat rule (R1) to
	// the exact-call guard: R1 keeps shaping the near-repeat
	// suppression but no longer fires, so one repeat produces one
	// note. Off by default, which keeps R1 for ablations.
	ExactRepeatsByGuard bool
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
	// R1: same h2 LoopRepeatIdentical times in a row. Under the
	// exact-call guard the condition still suppresses near-repeat
	// below, but the detection itself belongs to the guard.
	r1 := false
	if len(d.h2Window) >= LoopRepeatIdentical && allEqual(d.h2Window[len(d.h2Window)-LoopRepeatIdentical:]) {
		if !d.ExactRepeatsByGuard {
			out = append(out, d.fire(RuleIdenticalRepeat,
				fmt.Sprintf("ran %s %s %d times with the identical result; change strategy", ev.Tool, shorten(ev.Args), LoopRepeatIdentical)))
		}
		r1 = true
	}
	// R2: same h1 with error LoopRepeatIdentical times.
	r2 := false
	if ev.Error != "" && d.h1Errors[h1] >= LoopRepeatIdentical && d.h1Errors[h1]%LoopRepeatIdentical == 0 {
		out = append(out, d.fire(RuleSameError,
			fmt.Sprintf("ran %s %s with error %d times (%s); change strategy", ev.Tool, shorten(ev.Args), d.h1Errors[h1], shorten(ev.Error))))
		r2 = true
	}
	// R3: a cycle of LoopCycleMin..LoopCycleMax calls repeated
	// LoopCycleRepeats times.
	if n, reps := detectCycle(d.h1Window); reps >= LoopCycleRepeats {
		out = append(out, d.fire(RuleCycle,
			fmt.Sprintf("repeating a cycle of %d calls %d times; break the loop", n, reps)))
		d.h1Window = nil // avoid refiring on the same window
	}
	// Near-repeat: LoopNearRepeat consecutive same-tool calls with
	// near-identical arguments. Exact-match R1/R2 own the identical
	// outcome, so this rule only fires when neither did: the same
	// command polled with varying output, or the same call with
	// cosmetic argument tweaks that change nothing.
	if !r1 && !r2 {
		if det, ok := d.observeNearRepeat(ev.Tool, ev.Args); ok {
			out = append(out, det)
		}
	} else {
		d.nearTool, d.nearArgs = "", nil
	}
	return out
}

// observeNearRepeat tracks the trailing run of consecutive
// same-tool calls and fires RuleNearRepeat after LoopNearRepeat of
// them carry near-identical arguments. After firing it restarts the
// run, so a persisting loop refires (and escalates) every
// LoopNearRepeat further similar calls. A different tool or
// dissimilar arguments restarts the run.
func (d *LoopDetector) observeNearRepeat(tool, args string) (Detection, bool) {
	norm := collapseSpaces(args)
	if tool != d.nearTool || len(d.nearArgs) == 0 {
		d.nearTool, d.nearArgs = tool, []string{norm}
		return Detection{}, false
	}
	prev := d.nearArgs[len(d.nearArgs)-1]
	if !nearArgs(prev, norm) {
		d.nearTool, d.nearArgs = tool, []string{norm}
		return Detection{}, false
	}
	d.nearArgs = append(d.nearArgs, norm)
	if len(d.nearArgs) < LoopNearRepeat {
		return Detection{}, false
	}
	det := d.fire(RuleNearRepeat,
		fmt.Sprintf("ran %s %d times with near-identical arguments (%s); the outputs change but the approach does not; change strategy", tool, len(d.nearArgs), shorten(norm)))
	d.nearArgs = nil
	return det, true
}

// ObserveNoInfo feeds one tool step's novelty outcome and fires
// RuleNoInfo every LoopNoInfoSteps consecutive steps without new
// information, staged note, escalate, block like every other rule.
// An informative step resets the count.
func (d *LoopDetector) ObserveNoInfo(informative bool) []Detection {
	if informative {
		d.dull = 0
		return nil
	}
	d.dull++
	if d.dull < LoopNoInfoSteps || d.dull%LoopNoInfoSteps != 0 {
		return nil
	}
	return []Detection{d.fire(RuleNoInfo,
		fmt.Sprintf("no new information in the last %d tool steps (errors, empty or repeated outputs only); change strategy", d.dull))}
}

// collapseSpaces trims s and collapses every whitespace run to one
// space, so cosmetic spacing tweaks compare identical.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// nearArgs reports whether two whitespace-collapsed argument strings
// count as near-identical: byte-equal, or a normalized similarity at
// or above LoopNearSimilarity over the capped head. Comparison is
// case-sensitive (paths and flags are), and length guards keep the
// edit computation bounded.
func nearArgs(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) > LoopArgCompareCap {
		a = a[:LoopArgCompareCap]
	}
	if len(b) > LoopArgCompareCap {
		b = b[:LoopArgCompareCap]
	}
	la, lb := len(a), len(b)
	if la == 0 || lb == 0 {
		return false
	}
	mx := la
	if lb > mx {
		mx = lb
	}
	mn := la
	if lb < mn {
		mn = lb
	}
	if float64(mx-mn)/float64(mx) > 1-LoopNearSimilarity {
		return false
	}
	return 1-float64(levenshtein(a, b))/float64(mx) >= LoopNearSimilarity
}

// levenshtein returns the rune-wise edit distance of a and b,
// capped in practice by the caller trimming inputs first.
func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur := make([]int, len(br)+1)
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 0
			if ar[i-1] != br[j-1] {
				cost = 1
			}
			del, ins, sub := prev[j]+1, cur[j-1]+1, prev[j-1]+cost
			cur[j] = del
			if ins < cur[j] {
				cur[j] = ins
			}
			if sub < cur[j] {
				cur[j] = sub
			}
		}
		prev = cur
	}
	return prev[len(br)]
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

// Exact-call guard thresholds (agent quality loop guard v2).
const (
	// ExactCallNoteAt adds the note after this many identical
	// occurrences with no workspace change in between.
	ExactCallNoteAt = 3
	// ExactCallBanAt refuses the exact call from the turn after this
	// occurrence, for ExactCallBanTurns turns.
	ExactCallBanAt = 6
	// ExactCallBanTurns is the refusal window after ExactCallBanAt.
	ExactCallBanTurns = 5
)

// ExactCallNote is the note text for one identical occurrence count.
func ExactCallNote(count int) string {
	return fmt.Sprintf("you ran this exact call %d times with the same result; change approach", count)
}

// ExactCallBanNote is the note for the occurrence that opens a
// refusal window. That call has already run; the window applies to
// the calls after it.
func ExactCallBanNote(count, turns int) string {
	return fmt.Sprintf("%s. This exact call is refused for the next %d turns; use a different command or a different check.", ExactCallNote(count), turns)
}

// ExactCallRefusal is the text returned in place of a refused exact
// call, with the turns left in its window.
func ExactCallRefusal(count, turnsLeft int) string {
	return fmt.Sprintf("refused: %s. This exact call is refused for %d more turns; use a different command or a different check.", ExactCallNote(count), turnsLeft)
}

// ExactCallGuard counts identical calls and refuses persistent
// repeats. A call's count key combines its tool, the arguments it is
// compared by and the fingerprint of its result (ResultFingerprint);
// its ban key is the exact normalized call. Counts reset at every
// workspace change (MarkStateChange), so a repeat that follows a
// write or an install is a new observation. The guard never ends a
// run: it adds notes and refuses calls for a bounded window.
type ExactCallGuard struct {
	counts map[string]int
	bans   map[string]exactBan
}

type exactBan struct {
	until int
	count int
}

// NewExactCallGuard returns an empty guard.
func NewExactCallGuard() *ExactCallGuard {
	return &ExactCallGuard{counts: map[string]int{}, bans: map[string]exactBan{}}
}

// MarkStateChange records a workspace change. Every count restarts,
// because an identical call after a change may legitimately return
// the same result. Bans are not lifted.
func (g *ExactCallGuard) MarkStateChange() {
	g.counts = map[string]int{}
}

// Refusal reports whether the exact call identified by callKey is
// refused at turn, with the refusal text. Expired bans are dropped.
func (g *ExactCallGuard) Refusal(turn int, callKey string) (string, bool) {
	b, ok := g.bans[callKey]
	if !ok {
		return "", false
	}
	if turn > b.until {
		delete(g.bans, callKey)
		return "", false
	}
	return ExactCallRefusal(b.count, b.until-turn+1), true
}

// Observe counts one executed call at turn. It returns the note to
// inject ("" for none) and whether this occurrence starts a refusal
// window on callKey. Counting restarts after a window opens.
func (g *ExactCallGuard) Observe(turn int, countKey, callKey string) (note string, banned bool) {
	n := g.counts[countKey] + 1
	if n >= ExactCallBanAt {
		g.bans[callKey] = exactBan{until: turn + ExactCallBanTurns, count: n}
		delete(g.counts, countKey)
		return ExactCallBanNote(n, ExactCallBanTurns), true
	}
	g.counts[countKey] = n
	if n == ExactCallNoteAt {
		return ExactCallNote(n), false
	}
	return "", false
}

// Tally counts one occurrence of a call that the finish protocol
// handles, so no refusal window opens: the note comes at the third
// and the sixth occurrence, with the same text as Observe.
func (g *ExactCallGuard) Tally(countKey string) (note string) {
	n := g.counts[countKey] + 1
	g.counts[countKey] = n
	if n == ExactCallNoteAt || n == ExactCallBanAt {
		return ExactCallNote(n)
	}
	return ""
}

// NormalizeCallArgs canonicalizes a call's JSON arguments (sorted
// keys, numbers kept as written). Arguments that are not JSON fall
// back to whitespace-collapsed text.
func NormalizeCallArgs(args string) string {
	dec := json.NewDecoder(strings.NewReader(args))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err == nil {
		if buf, err := json.Marshal(v); err == nil {
			return string(buf)
		}
	}
	return collapseSpaces(args)
}

// volatileOutput matches the parts of a tool result that change on
// every run without changing what the call observed: the exec header
// duration, session id, process id and clock times.
var volatileOutput = []*regexp.Regexp{
	regexp.MustCompile(`duration: [0-9.]+s`),
	regexp.MustCompile(`session: [A-Za-z0-9_-]+`),
	regexp.MustCompile(`pid [0-9]+`),
	regexp.MustCompile(`\b[0-9]{2}:[0-9]{2}:[0-9]{2}\b`),
}

// ResultFingerprint hashes a tool result with its volatile tokens
// blanked, so two runs of the same observation compare equal.
func ResultFingerprint(out string) string {
	for _, re := range volatileOutput {
		out = re.ReplaceAllString(out, "_")
	}
	return hash("result", out)
}

// ExactCountKey is the count key of one call: tool, the arguments it
// is compared by (see the engine) and the result fingerprint.
func ExactCountKey(tool, compareArgs, resultFingerprint string) string {
	return hash("count", tool, compareArgs, resultFingerprint)
}

// ExactCallKey is the ban key of one call: tool and normalized
// arguments, with no result.
func ExactCallKey(tool, normalizedArgs string) string {
	return tool + "\x00" + normalizedArgs
}

// pureVerbs are shell commands whose output carries no state: a
// repeat with the same output is an identical call whatever the
// duration or the exact text.
var pureVerbs = map[string]bool{"echo": true, "printf": true, "true": true, "sleep": true}

var (
	devNullRedirect = regexp.MustCompile(`[0-9&]*>>?\s*/dev/null`)
	fileRedirect    = regexp.MustCompile(`(^|[^0-9&>])>>?\s*[^&\s>]`)
	fileWriteWord   = regexp.MustCompile(`(^|[;&|(\s])(tee|mv|cp|rm|mkdir|touch|chmod|chown|ln|dd|truncate|unzip|tar|sed\s+-i)\b`)
)

// MayWriteCommand reports whether a shell command can change files:
// a redirection to a file or a file-writing utility. Package
// installs are not counted here; the provisioner reports its own
// installs.
func MayWriteCommand(cmd string) bool {
	cmd = devNullRedirect.ReplaceAllString(cmd, "")
	return fileRedirect.MatchString(cmd) || fileWriteWord.MatchString(cmd)
}

// IsPureCommand reports whether a shell command is an echo, printf,
// true or sleep that cannot write files.
func IsPureCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) == 0 || !pureVerbs[fields[0]] {
		return false
	}
	return !MayWriteCommand(cmd)
}

// LiveSessionOutput reports whether a tool result shows a session
// that is still running. Such results are progress, not repeats, so
// the exact-call guard never counts them.
func LiveSessionOutput(out string) bool {
	return strings.Contains(out, "alive: true") || strings.Contains(out, "exited: false")
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
