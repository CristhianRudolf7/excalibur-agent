package engine

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Agent-quality loop behaviour (Sections 7.3, 18.3): actionable
// loop nudges with recovery strategies, a finish guard against
// premature completion, and a pinned working-notes block. Every
// mechanism has a config switch (QualityConfig, default ON) so it can
// be measured on and off; the EXCALIBUR_Q_* environment variables
// force a switch off (0/false) or on (1/true) for ablations.
//
// The switches never change budgets, the gate, the providers or any
// tool surface: they only change prompt bytes and volatile notes.
// Sibling workstreams plug precise behaviour through the hooks in
// Deps (FinishGuard, ErrorHint); the defaults here are generic
// engineering guidance.

// QualityConfig gates each agent-quality mechanism. Zero value
// disables everything; use DefaultQualityConfig (or nil Deps.Quality
// / builder Quality, which both mean defaults).
type QualityConfig struct {
	// WorkingMethod renders the L0 working-method block.
	WorkingMethod bool
	// FamilyAdapt selects the per-family prompt variant.
	FamilyAdapt bool
	// WorkingNotes maintains and renders the pinned notes block.
	WorkingNotes bool
	// LoopNudges replaces terse loop notes with actionable nudges
	// and enables ErrorHint recovery hints.
	LoopNudges bool
	// StuckRecovery injects the structured recovery prompt on the
	// near-repeat and no-information rules, tracks per-step novelty
	// for the no-information rule, and skips the R4 progress rule
	// while no progress sampler is installed (it false-fires on the
	// zero state in exec/eval single-run scope, where nothing ever
	// installs one).
	StuckRecovery bool
	// RepeatBlock rejects an exact repeat of the previous tool call
	// once per run and signature, with guidance to change approach.
	// Retryable outcomes (transient, timeout, canceled) are never
	// blocked: an immediate retry may be the correct repair. Loop
	// guard v2 (LoopGuardV2) replaces this blocker when both are on.
	RepeatBlock bool
	// LoopGuardV2 counts identical calls (tool, normalized arguments,
	// result fingerprint) with no workspace change in between: a note
	// at the third occurrence, a five-turn refusal of the exact call
	// at the sixth. It never ends the run. It also takes over the
	// identical-repeat detection of the loop detector.
	LoopGuardV2 bool
	// FinishGuard nudges back runs that try to finish with no
	// verified work while no gate is wired.
	FinishGuard bool
	// MaxGuardTrips caps finish-guard nudges per run.
	MaxGuardTrips int
	// MaxNotes caps pinned working notes kept per run.
	MaxNotes int
}

// DefaultQualityConfig returns the production defaults: every
// mechanism on.
func DefaultQualityConfig() QualityConfig {
	return QualityConfig{
		WorkingMethod: true,
		FamilyAdapt:   true,
		WorkingNotes:  true,
		LoopNudges:    true,
		StuckRecovery: true,
		RepeatBlock:   true,
		LoopGuardV2:   true,
		FinishGuard:   true,
		MaxGuardTrips: 2,
		MaxNotes:      24,
	}
}

// WithEnv overlays the EXCALIBUR_Q_* ablation switches on q: a set
// variable forces the mechanism off (0, false, no) or on (1, true,
// yes); unset variables keep q.
func (q QualityConfig) WithEnv() QualityConfig {
	if v, ok := envSwitch("EXCALIBUR_Q_METHOD"); ok {
		q.WorkingMethod = v
	}
	if v, ok := envSwitch("EXCALIBUR_Q_FAMILY"); ok {
		q.FamilyAdapt = v
	}
	if v, ok := envSwitch("EXCALIBUR_Q_NOTES"); ok {
		q.WorkingNotes = v
	}
	if v, ok := envSwitch("EXCALIBUR_Q_NUDGES"); ok {
		q.LoopNudges = v
	}
	if v, ok := envSwitch("EXCALIBUR_Q_RECOVERY"); ok {
		q.StuckRecovery = v
	}
	if v, ok := envSwitch("EXCALIBUR_Q_REPEATBLOCK"); ok {
		q.RepeatBlock = v
	}
	if v, ok := envSwitch("EXCALIBUR_Q_GUARD"); ok {
		q.FinishGuard = v
	}
	if v, ok := envSwitch(LoopGuardV2Switch); ok {
		q.LoopGuardV2 = v
	}
	return q
}

// LoopGuardV2Switch gates the exact-call guard (loop guard v2). Default
// ON; EXCALIBUR_LOOP_GUARD_V2=0 restores the legacy single-call repeat
// blocker and the identical-repeat rule of the loop detector.
const LoopGuardV2Switch = "EXCALIBUR_LOOP_GUARD_V2"

// envSwitch reads one boolean environment variable.
func envSwitch(name string) (bool, bool) {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

// effectiveQuality resolves a nil Quality to the defaults with the
// environment overlay.
func effectiveQuality(q *QualityConfig) QualityConfig {
	if q == nil {
		d := DefaultQualityConfig()
		return d.WithEnv()
	}
	return *q
}

// FinishGuardFunc decides whether a run that answered without tools
// must keep working. It returns continue=true with a volatile note
// for the model. Nil Deps.FinishGuard installs the default
// thin-work guard; the verification workstream overrides it with
// real criteria without touching the loop.
type FinishGuardFunc func(run *Run) (cont bool, note string)

// ErrorHintFunc suggests a recovery for one failed tool call. The
// note is injected volatilely for the next model call (at most one
// per step). Nil means no per-error hint; the tool-surface
// workstream plugs precise per-tool hints here.
type ErrorHintFunc func(tool, args, errText string) string

// quality returns the engine's effective quality config.
func (e *Engine) quality() QualityConfig {
	return effectiveQuality(e.deps.Quality)
}

// nudgeFor renders one loop detection as an actionable nudge: the
// pattern plus a concrete recovery strategy (Section 18.3 staged
// response, stage 1). With LoopNudges off it renders the legacy
// terse note. The "loop:"/"expert:"/"loop escalated:" prefixes are
// stable so telemetry and tests can match on them.
func nudgeFor(q QualityConfig, d tasks.Detection, expertDiag string) string {
	if !q.LoopNudges {
		switch d.Action {
		case tasks.ActionEscalate:
			if expertDiag != "" {
				return "expert: " + expertDiag
			}
			return "loop escalated: " + d.Note
		default:
			return "loop: " + d.Note
		}
	}
	switch d.Rule {
	case tasks.RuleSameError:
		return "loop: " + d.Note + " Recovery: do not retry the identical call. Read the full error, fix exactly the failing parameter or path, then retry once with corrected arguments. If it fails again, change approach (read the relevant code or search for the right tool) instead of retrying."
	case tasks.RuleIdenticalRepeat:
		return "loop: " + d.Note + " Recovery: stop repeating this call. Re-read the last result: if the goal is already met, finish with the evidence; otherwise use a different tool or different arguments."
	case tasks.RuleCycle:
		return "loop: " + d.Note + " Recovery: break the sequence now. State in one line what you learned from the last cycle, do one different read-only check, then take a single new action."
	case tasks.RuleNoProgress:
		return "loop: " + d.Note + " Recovery: re-read the original request and the pinned working notes, restate the smallest unverified step, verify the current state with one check, then act on it."
	case tasks.RuleRepeatedText:
		return "loop: " + d.Note + " Recovery: stop generating text and make one tool call."
	case tasks.RuleEditMismatch:
		return "loop: " + d.Note + " Recovery: re-read the file and base the next edit on the fresh content, with a smaller uniquely-matching hunk."
	default:
		return "loop: " + d.Note + " Recovery: change strategy instead of repeating the last action."
	}
}

// escalateNote renders a stage-2 escalation: the expert diagnosis
// when available, otherwise a strategy-switch note.
func escalateNote(q QualityConfig, d tasks.Detection, expertDiag string) string {
	if expertDiag != "" {
		if !q.LoopNudges {
			return "expert: " + expertDiag
		}
		return "expert: " + expertDiag + " Apply the diagnosis with one small change and verify it. If it does not help, switch strategy: investigate the cause with read-only tools before acting again."
	}
	if !q.LoopNudges {
		return "loop escalated: " + d.Note
	}
	return "loop escalated: " + d.Note + " Strategy switch: stop acting and investigate the cause with read-only tools (read the files, reproduce the error), then try one different approach."
}

// recoveryPrompt renders the structured stuck-recovery prompt for
// the near-repeat and no-information rules: what was just tried,
// then a forced reconsider (summarise, list three different
// strategies, pick one and execute it) instead of another retry.
// tried carries the most recent tool signatures (tool + truncated
// args), most-recent-last, at most three. The "loop:" prefix stays
// stable so telemetry and tests match on it.
func recoveryPrompt(q QualityConfig, d tasks.Detection, tried []string) string {
	if !q.StuckRecovery {
		return nudgeFor(q, d, "")
	}
	var b strings.Builder
	b.WriteString("loop: " + d.Note + "\n")
	b.WriteString("Stuck recovery — do NOT repeat the previous call. Answer in this shape:\n")
	b.WriteString("1. Tried (2 lines max): what the last calls did and what each result showed")
	if len(tried) > 0 {
		b.WriteString(" — e.g. " + strings.Join(tried, "; "))
	}
	b.WriteString(".\n")
	b.WriteString("2. Three DIFFERENT strategies: each must use a different tool or a different approach, not another parameter tweak of the same command.\n")
	b.WriteString("3. Pick the most promising one and execute its first step NOW as your next tool call.")
	return b.String()
}

// recentTried summarizes the last n tool calls as short signatures
// (tool + truncated args, most-recent-last) for the recovery prompt.
// Callers hold no lock.
func recentTried(run *Run, n int) []string {
	run.mu.Lock()
	defer run.mu.Unlock()
	if len(run.recentCalls) <= n {
		return append([]string(nil), run.recentCalls...)
	}
	return append([]string(nil), run.recentCalls[len(run.recentCalls)-n:]...)
}

// recordCall remembers one executed tool signature for the recovery
// prompt and the exact-repeat blocker. Callers hold no lock.
func recordCall(run *Run, sig string) {
	if sig == "" {
		return
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	run.recentCalls = append(run.recentCalls, sig)
	for len(run.recentCalls) > 6 {
		run.recentCalls = run.recentCalls[1:]
	}
}

// guardFinish decides whether a run that answered without tools
// must keep working. The default thin-work guard only fires when no
// gate is wired at all (no Deps.Gate and an empty GateInput) and the
// run attempted tools without a single success: evidence of struggle
// without verification. A run that never attempted a tool (a trivial
// answer) and any run with a success, a wired gate input, or a hook
// verdict pass straight through, so wired verification always wins.
// It trips at most MaxGuardTrips times per run and never fails it.
func (e *Engine) guardFinish(run *Run) (bool, string) {
	q := e.quality()
	if !q.FinishGuard {
		return false, ""
	}
	if e.deps.FinishGuard != nil {
		return e.deps.FinishGuard(run)
	}
	if e.deps.Gate != nil {
		return false, ""
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if !reflect.DeepEqual(run.GateInput, tasks.GateInput{}) {
		return false, ""
	}
	attempts, successes := countAttemptsLocked(run), countSuccessesLocked(run)
	if attempts == 0 || successes > 0 {
		return false, ""
	}
	maxTrips := q.MaxGuardTrips
	if maxTrips < 0 {
		maxTrips = 0
	}
	if run.GuardTrips >= maxTrips {
		return false, ""
	}
	run.GuardTrips++
	return true, "no verified work yet: the tools tried so far produced no usable result. Read the last error, fix one thing, and verify by executing before finishing. Do not finish without evidence."
}

// countAttemptsLocked counts tool results in the history, successful
// or not. Callers hold run.mu.
func countAttemptsLocked(run *Run) int {
	n := 0
	for _, ev := range run.History {
		if ev.ToolResult != nil {
			n++
		}
	}
	return n
}

// countSuccessesLocked counts non-error tool results in the history.
// Callers hold run.mu.
func countSuccessesLocked(run *Run) int {
	n := 0
	for _, ev := range run.History {
		if ev.ToolResult != nil && !ev.ToolResult.IsError {
			n++
		}
	}
	return n
}

// factForToolCall derives one pinned working-note line from a tool
// outcome: failures always (with the first error line), successes
// that name a file path (deduplicated later). Truncation keeps the
// block small and stable.
func factForToolCall(name, args string, res tools.Result) string {
	if res.Status != tools.StatusOK {
		first := firstLine(res.Output)
		if first == "" {
			first = string(res.Status)
		}
		return "FAIL " + name + " " + truncOneLine(args, 80) + ": " + truncOneLine(first, 160)
	}
	if path := argsPath(args); path != "" {
		return "did " + name + " " + truncOneLine(path, 120)
	}
	return ""
}

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}

// truncOneLine flattens s to one line capped at n characters.
func truncOneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

// argsPath extracts a top-level "path" string from a JSON args
// object. Most file tools name their target path.
func argsPath(args string) string {
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &body); err != nil {
		return ""
	}
	raw, ok := body["path"]
	if !ok {
		return ""
	}
	var path string
	if err := json.Unmarshal(raw, &path); err != nil || path == "" {
		return ""
	}
	return path
}

// guardKeys is one executed call reduced to the exact-call guard's
// keys: the count key (occurrences with the same result), the call key
// (the exact call, used for refusals) and whether the call changed
// the workspace.
type guardKeys struct {
	count  string
	call   string
	writes bool
}

// unwrapCallTool returns the tool a call_tool wrapper dispatches to and
// its arguments; any other call is returned unchanged.
func unwrapCallTool(name string, args json.RawMessage) (string, json.RawMessage) {
	if name != "call_tool" {
		return name, args
	}
	var wrapped struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(args, &wrapped) == nil && wrapped.Name != "" {
		return wrapped.Name, wrapped.Arguments
	}
	return name, args
}

// callKeyOf is the exact-call key of a call before it runs.
func callKeyOf(tc providers.ToolCall) string {
	name, raw := unwrapCallTool(tc.Name, tc.Arguments)
	return tasks.ExactCallKey(name, tasks.NormalizeCallArgs(string(raw)))
}

// guardKeysOf derives the exact-call guard keys of one executed call.
// Pure echo, printf, true and sleep commands compare by result alone,
// so a repeat with the same output counts whatever its exact text.
// Writes are file-changing tools that succeeded, shell commands that
// redirect to a file or call a file-writing utility, a provisioner
// install, and input sent to a live session.
func guardKeysOf(tc providers.ToolCall, res tools.Result) guardKeys {
	name, raw := unwrapCallTool(tc.Name, tc.Arguments)
	args := tasks.NormalizeCallArgs(string(raw))
	compare := args
	ok := res.Status == tools.StatusOK
	var fields struct {
		Command string `json:"command"`
		Action  string `json:"action"`
	}
	_ = json.Unmarshal(raw, &fields)
	var writes bool
	switch name {
	case "exec":
		if tasks.IsPureCommand(fields.Command) {
			compare = "pure"
		}
		writes = ok && (tasks.MayWriteCommand(fields.Command) || strings.Contains(res.Output, "provision: installed "))
	case "exec_io":
		writes = ok && (fields.Action == "write" || fields.Action == "keys")
	case "write_file", "edit_file", "apply_patch":
		writes = ok
	}
	return guardKeys{
		count:  tasks.ExactCountKey(name, compare, tasks.ResultFingerprint(res.Output)),
		call:   tasks.ExactCallKey(name, args),
		writes: writes,
	}
}

// guardRefusal returns the refusal text when the exact call is inside
// its refusal window at the current step. Callers hold no lock.
func (r *Run) guardRefusal(tc providers.ToolCall) (string, bool) {
	key := callKeyOf(tc)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exact == nil {
		return "", false
	}
	return r.exact.Refusal(r.Steps, key)
}

// observeExact counts one executed call and returns the note to inject
// ("" for none). A session that is still running is progress, not a
// repeat, so its result is not counted; a workspace change still resets
// the counts. The guard never refuses here: a window opens only for the
// next occurrence (see guardRefusal). Callers hold no lock.
func (r *Run) observeExact(tc providers.ToolCall, res tools.Result) string {
	k := guardKeysOf(tc, res)
	live := r.sessionRunning(tc, res)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exact == nil {
		r.exact = tasks.NewExactCallGuard()
	}
	note := ""
	if !live {
		note, _ = r.exact.Observe(r.Steps, k.count, k.call)
	}
	if k.writes {
		r.exact.MarkStateChange()
	}
	return note
}

// finishAttempt matches the attempt counter of a structured finish refusal.
var finishAttempt = regexp.MustCompile(`"attempt":[0-9]+,?`)

// observeFinish counts one finish attempt with the finish protocol's
// outcome and normalized arguments. The same bounded refusal window
// applies as for ordinary tools. Callers hold no lock.
func (r *Run) observeFinish(tc providers.ToolCall, refusal string, enabled bool) string {
	if !enabled {
		return ""
	}
	result := "accepted"
	if refusal != "" {
		// The attempt number differs on every refusal; the rest of the
		// refusal (checklist, next action) is what repeats.
		result = finishAttempt.ReplaceAllString(refusal, "")
	}
	countKey := tasks.ExactCountKey("finish", tasks.NormalizeCallArgs(string(tc.Arguments)), tasks.ResultFingerprint(result))
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.exact == nil {
		r.exact = tasks.NewExactCallGuard()
	}
	note, _ := r.exact.Observe(r.Steps, countKey, callKeyOf(tc))
	return note
}

// lastRefusal returns the latest finish refusal reason. Callers hold no
// lock.
func (r *Run) lastRefusal() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.LastRefusal
}
