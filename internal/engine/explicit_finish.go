// Explicit finish protocol (agent-quality, Section 7.3 closing).
//
// In non-interactive profiles (exec, delegate, eval) a run ends ONLY
// through an explicit `finish` tool call with `summary` (what was
// done) and `evidence` (commands run and their results proving each
// requirement). An assistant turn without tool calls never ends the
// run: the engine replies with a short continuation message and
// counts consecutive text-only turns; after 3 in a row it enters
// closing mode (budget-closing semantics, tools stay available so
// `finish` remains callable).
//
// `finish` is refused (with the reason) when the most recent
// execution of any check the agent itself ran for a requirement
// failed after the last change, when required artifacts are missing,
// or when the agent's own last command errored. Refusals are not
// ended by a second refusal or a loop detector. Budgets still bind;
// 25 consecutive refusals without tool work also trigger a safety stop.
//
// Confirmation round (Terminus 2 shape): the first accepted `finish`
// triggers one short re-read-and-confirm turn; the second `finish`
// ends the run. `blocked` may only be declared via `finish` with
// `status: blocked` and a concrete reason; a blocked claim about
// missing data/files is checked deterministically against the
// filesystem paths mentioned and refused if they exist.
//
// Records: the model's raw final text and the `finish` arguments are
// stored on the run; the delivery summary uses `finish.summary` and
// never echoes the instruction.
//
// Switch `EXCALIBUR_EXPLICIT_FINISH=on|off` (default on); off
// restores the old behaviour (text-only turns run the gate) for
// ablation. The mechanism never changes budgets, the gate, the
// providers or any tool surface: it only changes step control flow
// and volatile notes.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ExplicitFinishConfig gates the explicit finish protocol. Zero value
// disables; use DefaultExplicitFinishConfig (or nil Deps, which means
// defaults with the environment overlay).
type ExplicitFinishConfig struct {
	Enabled bool
}

// DefaultExplicitFinishConfig returns the production default: on.
func DefaultExplicitFinishConfig() ExplicitFinishConfig {
	return ExplicitFinishConfig{Enabled: true}
}

// WithEnv overlays EXCALIBUR_EXPLICIT_FINISH: on/1/true/yes enables,
// off/0/false/no disables; unset keeps the value, invalid is ignored.
func (c ExplicitFinishConfig) WithEnv() ExplicitFinishConfig {
	raw, ok := os.LookupEnv("EXCALIBUR_EXPLICIT_FINISH")
	if !ok {
		return c
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		c.Enabled = true
	case "0", "false", "no", "off":
		c.Enabled = false
	}
	return c
}

// ExplicitFinishSwitchOn reports the raw switch state (default on)
// for drivers that need it outside the engine (tool prefix,
// instructions, records). The engine's effective decision also
// requires the run profile flag; see explicitFinishRequired.
func ExplicitFinishSwitchOn() bool {
	return DefaultExplicitFinishConfig().WithEnv().Enabled
}

// explicitFinishEnabled reports whether the protocol switch is on:
// the driver override wins, otherwise defaults with the environment
// overlay.
func (e *Engine) explicitFinishEnabled() bool {
	if e.deps.ExplicitFinish != nil {
		return e.deps.ExplicitFinish.Enabled
	}
	return DefaultExplicitFinishConfig().WithEnv().Enabled
}

// ExplicitFinishEnabledFor reports the effective protocol state for a
// run: the global switch AND the profile flag. Engine.Profile marks
// non-interactive runs (exec/delegate/eval embedded runs set it);
// interactive daemon runs leave it false and keep the old behaviour
// even when the switch is on.
func (e *Engine) explicitFinishRequired(run *Run) bool {
	if !e.explicitFinishEnabled() {
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.Profile.ExplicitFinish
}

// FinishArgs are the `finish` tool arguments.
type FinishArgs struct {
	Summary  string `json:"summary"`
	Evidence string `json:"evidence"`
	Status   string `json:"status,omitempty"`
	Reason   string `json:"reason,omitempty"`
	RawArgs  string `json:"-"`
}

// ParseFinishArgs validates one `finish` call. Status defaults to
// completed; blocked requires a concrete reason.
func ParseFinishArgs(raw json.RawMessage) (FinishArgs, error) {
	var a FinishArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return FinishArgs{}, err
	}
	a.Summary = strings.TrimSpace(a.Summary)
	a.Evidence = strings.TrimSpace(a.Evidence)
	a.Status = strings.ToLower(strings.TrimSpace(a.Status))
	a.Reason = strings.TrimSpace(a.Reason)
	if a.Status == "" {
		a.Status = "completed"
	}
	if a.Status != "completed" && a.Status != "blocked" {
		return FinishArgs{}, errFinish("status must be \"completed\" or \"blocked\", got %q", a.Status)
	}
	if a.Summary == "" {
		return FinishArgs{}, errFinish("summary is required: say what was done")
	}
	if a.Status == "completed" && a.Evidence == "" {
		return FinishArgs{}, errFinish("evidence is required: list the commands run and their results proving each requirement")
	}
	if a.Status == "blocked" && a.Reason == "" {
		return FinishArgs{}, errFinish("reason is required when status is blocked: name the concrete missing data, file or permission")
	}
	a.RawArgs = string(raw)
	return a, nil
}

type finishError string

func errFinish(format string, args ...any) finishError {
	if len(args) == 0 {
		return finishError(format)
	}
	return finishError(fmt.Sprintf(format, args...))
}

func (e finishError) Error() string { return "finish refused: " + string(e) }

// FinishToolName is the control tool name. It is injected into the
// provider request by the driver (CLI exec path) when the protocol is
// active; the engine intercepts calls by name so no registry entry is
// needed and the frozen tool tables never move.
const FinishToolName = "finish"

// FinishToolDefinition is the model-visible schema for `finish`.
func FinishToolDefinition() providers.ToolDefinition {
	return providers.ToolDefinition{
		Name: "finish",
		Description: "End the run explicitly. Call ONLY when everything is verified: summary says what was done, " +
			"evidence lists the commands run and their results proving each requirement. " +
			"Use status blocked with a concrete reason only when genuinely stuck (missing data, file or permission). " +
			"Never stop with a text-only answer: always call finish with evidence. " +
			"Services the instruction requires must be left running (background:true sessions survive the run; declare keep_running in evidence); " +
			"stop temporary helpers with exec_io close. Killing or stopping a process the task requires is a failure, never a blocked reason.",
		Schema: json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string","description":"what was done"},"evidence":{"type":"string","description":"commands run and their results proving each requirement"},"status":{"type":"string","enum":["completed","blocked"],"description":"completed (default) or blocked when genuinely stuck"},"reason":{"type":"string","description":"concrete reason when blocked"}},"required":["summary","evidence"],"additionalProperties":true}`),
	}
}

// Continuation and confirmation prompts (stable prefixes for
// telemetry and tests).
const (
	// TextOnlyContinuation is the volatile note for a text-only turn
	// under the protocol.
	TextOnlyContinuation = "No tool call. Continue working with tools, or call `finish` with evidence if everything is verified."
	// ConfirmAfterFinish is the single confirmation turn after the
	// first accepted finish.
	ConfirmAfterFinish = "Re-read the instruction and confirm each requirement with evidence, or continue working. Call `finish` again to end the run."
	// ClosingAfterTextOnly names the forced closing after 3 text-only
	// turns.
	ClosingAfterTextOnly = "closing mode: three text-only turns without tool calls; verify what exists and call `finish` with evidence soon"
)

// MaxTextOnlyStreak counts text-only turns before forced closing.
const MaxTextOnlyStreak = 3

// MaxTextOnlyTurns ends a non-interactive run whose model has made no
// tool call for this many consecutive turns (safety stop).
const MaxTextOnlyTurns = 40

// FinishAcceptedText is the finish tool result of an accepted finish
// (D1): the run ends with this call.
const FinishAcceptedText = "finish accepted: the run ends now."

// finishState is the finish-protocol state that lives beside the run
// record: the refusal fingerprint for the no-change rule (D2), the
// verifier refusal budget and advisory notes (D6), and the input
// snapshot taken before the first model action (D4). Guarded by the
// run mutex.
type finishState struct {
	blockingCounts   map[string]int
	finishAdvisory   []string
	refused          bool
	refusalMark      int
	refusalReasons   []string
	verifierRefusals int
	advisory         []string
	snapshot         *tasks.InputSnapshot
	inputCopyDir     string
	snapshotTaken    bool
}

func isFinishCall(tc providers.ToolCall) bool {
	return tc.Name == FinishToolName
}

func hasFinishCall(calls []providers.ToolCall) bool {
	for _, tc := range calls {
		if isFinishCall(tc) {
			return true
		}
	}
	return false
}

// resetTextStreak clears the consecutive text-only count after any
// tool call turn. Callers hold no lock.
func (r *Run) resetTextStreak() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.textOnlyStreak = 0
	r.explicitClosing = false
}

// noteTextOnly records one text-only turn: the raw text for the
// records, the incremented streak, and whether the streak tripped
// forced closing. It returns the streak and the closing flag.
// Callers hold no lock.
func (r *Run) noteTextOnly(rawText string) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.FinalText = rawText
	r.textOnlyStreak++
	if r.textOnlyStreak >= MaxTextOnlyStreak {
		r.explicitClosing = true
	}
	return r.textOnlyStreak, r.explicitClosing
}

// TextOnlyStreak reports the consecutive text-only count for records.
// Callers hold no lock; the run is quiescent when drivers read it.
func (r *Run) TextOnlyStreak() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.textOnlyStreak
}

// recordFinishAttempt counts one `finish` call and stores the raw
// arguments for the records. Callers hold no lock.
func (r *Run) recordFinishAttempt(rawArgs, finalText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.FinishCalls++
	r.RawFinish = rawArgs
	if finalText != "" {
		r.FinalText = finalText
	}
}

// recordFinishRefusal counts one refused `finish` with its reason
// for the records. Callers hold no lock.
func (r *Run) recordFinishRefusal(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.FinishRefusals++
	r.LastRefusal = reason
}

// clearRefusal forgets the refusal reason after an accepted `finish`.
// Callers hold no lock.
func (r *Run) clearRefusal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.LastRefusal = ""
}

// confirmState reports whether the confirmation round is armed.
// Callers hold no lock.
func (r *Run) confirmArmed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finishConfirmPending
}

// armConfirm arms the confirmation round and stores the accepted
// arguments. Callers hold no lock.
func (r *Run) armConfirm(args FinishArgs) {
	hash := workspaceCheckHash(r.Workdir)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishConfirmPending = true
	r.confirmProgress = confirmationProgress{hash: hash}
	cp := args
	r.Finish = &cp
}

// storeFinish stores the final accepted arguments with the delivery
// summary. Callers hold no lock.
func (r *Run) storeFinish(args FinishArgs, finalText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := args
	r.Finish = &cp
	r.Summary = args.Summary
	if finalText != "" {
		r.FinalText = finalText
	}
	// A completed run leaves no streak behind.
	r.textOnlyStreak = 0
	r.explicitClosing = false
}

// handleTextOnly applies the protocol to an assistant turn without
// tool calls: it never ends the run. The raw text is stored, the
// streak grows, and a short continuation note returns the model to
// work. After MaxTextOnlyStreak in a row the run enters closing mode
// (tools stay available so `finish` remains callable).
func (e *Engine) handleTextOnly(ctx context.Context, run *Run, rawText string) (bool, error) {
	_ = ctx
	streak, closing := run.noteTextOnly(rawText)
	note := TextOnlyContinuation
	if closing {
		note = note + " " + ClosingAfterTextOnly
	} else if streak > 1 {
		note = note + fmt.Sprintf(" (text-only turn %d of 3 before closing mode)", streak)
	}
	run.InjectNote(note)
	return false, nil
}

// lastNonFinishError returns the last tool result that is not a
// `finish` control call, with its error flag. The `finish` tool
// itself never counts: a refused finish must not poison the next
// attempt.
func lastNonFinishError(run *Run) (output string, isError bool, found bool) {
	h, found := lastNonFinishEntry(run)
	return h.Output, h.IsError, found
}

// blockedPathsExist checks a blocked claim deterministically: every
// filesystem path mentioned in the reason (via the artifact
// extractor) must be missing. It returns the first path that exists.
func (e *Engine) blockedPathsExist(run *Run, reason string) string {
	reqs := tasks.ExtractArtifactRequirements(reason, 8)
	if len(reqs) == 0 {
		return ""
	}
	stat := e.deps.ArtifactStat
	if stat == nil {
		stat = tasks.DefaultStat
	}
	run.mu.Lock()
	workdir := run.Workdir
	run.mu.Unlock()
	for _, r := range reqs {
		if r.Path == "" {
			continue
		}
		resolved := resolveFinishPath(workdir, r.Path)
		if fs, err := stat(resolved); err == nil && fs.Exists {
			return r.Path
		}
	}
	return ""
}

// resolveFinishPath maps a requirement path onto the run filesystem:
// absolute paths stay absolute, anything else resolves under workdir.
func resolveFinishPath(workdir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	trimmed := strings.TrimPrefix(path, "./")
	if workdir != "" {
		return filepath.Join(workdir, trimmed)
	}
	return trimmed
}

// historyEventForTool builds the tool-result history entry, mirroring
// execBatch so the trajectory stays complete.
func historyEventForTool(seq int64, tc providers.ToolCall, res tools.Result) ctxcompiler.ActivityEvent {
	return ctxcompiler.ActivityEvent{
		Seq: seq, Role: providers.RoleTool,
		Content:    res.Output,
		ToolResult: &providers.ToolResult{CallID: tc.ID, Name: tc.Name, Output: res.Output, IsError: res.Status != tools.StatusOK},
	}
}

// finishDecision is the outcome of the deterministic and verifier
// checks for one completed or blocked `finish` call. Reasons refuse
// the call; soft reasons (verifier advice and inspection items) are
// accepted as completed_with_pending once less than 10% of the wall
// budget remains (D2).
type finishDecision struct {
	reasons []string
	soft    bool
}

// validateFinish runs the refusal checks for one parsed `finish`
// call and returns the refusal reason ("" when accepted). It is the
// string view of decideFinish, kept for callers that only need the
// verdict text.
func (e *Engine) validateFinish(ctx context.Context, run *Run, args FinishArgs, evidenceText string) string {
	dec := e.decideFinish(ctx, run, args, evidenceText)
	if len(dec.reasons) == 0 || dec.soft {
		return ""
	}
	return strings.Join(dec.reasons, "; ")
}

// decideFinish is the single entry for the finish checks: the agent's
// own last command must not have errored, the full verification (gate,
// self-verify, artifacts, strong verifier) must pass, and a blocked
// claim must not name paths that exist. Blocked claims go through the
// strict blocked rules (strictblocked.go).
func (e *Engine) decideFinish(ctx context.Context, run *Run, args FinishArgs, evidenceText string) finishDecision {
	e.restoreRefusals(ctx, run)
	var reasons []string
	if reason := scopeRefusal(run); reason != "" {
		reasons = append(reasons, reason)
	}
	// Probe failures (grep with no match, test -f, command -v) are normal
	// answers; the exec result marks them with the probe note.
	h, found := lastNonFinishEntry(run)
	if args.Status != "blocked" && found && h.IsError && !tools.IsProbeFailureOutput(h.Output) {
		reason := lastErrorReason(h.Output)
		if finishErrorEntryEnabled() {
			reason = lastErrorReason(h.Output, historyCommand(h))
		}
		reasons = append(reasons, reason)
		if !ActionableRefusalsEnabled() {
			return finishDecision{reasons: e.hardFinishReasons(run, reasons)}
		}
	}
	if args.Status == "blocked" {
		reasons = append(reasons, e.blockedReasons(run, args)...)
		return finishDecision{reasons: e.hardFinishReasons(run, reasons)}
	}
	gate := e.gateCheck(run)
	merged := e.selfVerify(ctx, run, gate, evidenceText)
	if !merged.OK {
		reasons = append(reasons, merged.Pendings...)
	}
	reasons = e.hardFinishReasons(run, reasons)
	dec := finishDecision{reasons: reasons}
	dec.soft = len(reasons) > 0 && allSoftReasons(reasons) && !e.timeRemains(run)
	return dec
}

// lastErrorReason states the agent's own failing command with the
// evidence the model needs: the exit code and the last output lines.
func lastErrorReason(out string, command ...string) string {
	detail := "tool error"
	if code, ok := exitCodeOfOutput(out); ok {
		detail = fmt.Sprintf("exit %d", code)
	}
	if len(command) > 0 && command[0] != "" {
		detail += "; command: " + truncOneLine(command[0], 300)
	}
	return "your last command errored (" + detail + "; last output: " + lastOutputLines(out, 3, 300) +
		"). Fix it and verify by executing before calling `finish` again."
}

// exitCodeOfOutput reads the exec header `exit: N` line.
func exitCodeOfOutput(out string) (int, bool) {
	m := execExitRe.FindStringSubmatch(out)
	if len(m) != 2 {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

var execExitRe = regexp.MustCompile(`(?m)^exit: (-?\d+)\b`)

// lastOutputLines returns the last n non-empty lines of out, joined
// with " | " and cut to max bytes.
func lastOutputLines(out string, n, max int) string {
	if finishErrorEntryEnabled() {
		// Executor path suggestions and the engine clock footer are harness
		// context, not the failing process's output.
		if i := strings.Index(out, "\nhint: no such file or directory."); i >= 0 {
			out = out[:i]
		}
		if i := strings.LastIndex(out, "\n[time:"); i >= 0 {
			out = out[:i]
		}
	}
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	s := strings.Join(lines, " | ")
	if len(s) > max {
		s = s[:max-1] + "…"
	}
	return s
}

// allSoftReasons reports whether every reason is soft.
func allSoftReasons(reasons []string) bool {
	for _, r := range reasons {
		if !isSoftReason(r) {
			return false
		}
	}
	return true
}

// softReasonMarks identify advisory verifier outcomes and inspection
// items: they never block a run that has no wall time left.
var softReasonMarks = []string{
	"independent verifier", "generalisation verifier", "verifier note",
	"constraint holds", "is produced in", "produces ",
}

func isSoftReason(reason string) bool {
	lower := strings.ToLower(reason)
	for _, m := range softReasonMarks {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// setFinishVerdict makes the finish tool result carry the verdict
// text (D1). The model sees the result in its next request; the
// volatile note stays as a duplicate for models that ignore results.
func (e *Engine) setFinishVerdict(run *Run, callID, text string, isError bool) {
	if !FinishVerdictResultEnabled() {
		return
	}
	run.setFinishResult(callID, withFooter(text, run.confirmationFooter()), isError)
}

// setFinishResult rewrites the output of the finish tool result for
// one call id (the latest finish result when callID is empty).
func (r *Run) setFinishResult(callID, output string, isError bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.History) - 1; i >= 0; i-- {
		tr := r.History[i].ToolResult
		if tr == nil || tr.Name != FinishToolName {
			continue
		}
		if callID != "" && tr.CallID != callID {
			continue
		}
		cp := *tr
		cp.Output = output
		cp.IsError = isError
		r.History[i].ToolResult = &cp
		r.History[i].Content = output
		return
	}
}

// recordCallResults answers every call of a turn that is not executed
// (a `finish` batched with other calls): each call gets a tool result
// so the trajectory stays paired.
func (e *Engine) recordCallResults(run *Run, calls []providers.ToolCall, finishText string) {
	footer := run.confirmationFooter()
	run.mu.Lock()
	defer run.mu.Unlock()
	for _, tc := range calls {
		text := "not executed: `finish` must be called alone in its own turn."
		if isFinishCall(tc) {
			text = finishText
		}
		text = withFooter(text, footer)
		seq := run.nextHistorySeqLocked()
		run.History = append(run.History, ctxcompiler.ActivityEvent{
			Seq: seq, Role: providers.RoleTool, Content: text,
			// Not executed is not a failure of the agent's own command.
			ToolResult: &providers.ToolResult{CallID: tc.ID, Name: tc.Name, Output: text, IsError: false},
		})
	}
}

// workMarkLocked counts the non-finish tool results in the history:
// the measure of tool work used by the no-change refusal (D2). Callers
// hold run.mu.
func (r *Run) workMarkLocked() int {
	n := 0
	for _, ev := range r.History {
		if ev.ToolResult != nil && ev.ToolResult.Name != FinishToolName {
			n++
		}
	}
	return n
}

// unchangedRefusal returns the reasons of the previous refusal when no
// tool work happened since it (D2). The verifier is not consulted.
func (r *Run) unchangedRefusal() ([]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.fin.refused || r.fin.refusalMark != r.workMarkLocked() {
		return nil, false
	}
	return append([]string(nil), r.fin.refusalReasons...), true
}

// rememberRefusal stores the refusal reasons with the tool-work mark.
func (r *Run) rememberRefusal(reasons []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fin.refused = true
	r.fin.refusalMark = r.workMarkLocked()
	r.fin.refusalReasons = append([]string(nil), reasons...)
}

// refuseCall refuses one finish call: the numbered refusal is the
// finish tool result (D1) and a duplicate volatile note.
func (e *Engine) refuseCall(ctx context.Context, run *Run, tc providers.ToolCall, reasons []string) {
	e.countBlockingReasons(run, reasons)
	text := e.refuseFinish(ctx, run, reasons)
	run.recordFinishRefusal(text)
	run.rememberRefusal(reasons)
	e.setFinishVerdict(run, tc.ID, text, true)
	run.InjectNote(text)
}

// noChangeRefusal answers a finish that follows a refusal with no tool
// work in between (D2): the same reasons, without the verifier.
func (e *Engine) noChangeRefusal(ctx context.Context, run *Run, tc providers.ToolCall, reasons []string) {
	e.countBlockingReasons(run, reasons)
	if e.boundedRefusals(run) && ActionableRefusalsEnabled() {
		_ = e.refuseFinish(ctx, run, reasons) // Persist repeated blocks for recovery.
	}
	text := "finish refused: nothing changed since the previous refusal (same reasons): " + strings.Join(reasons, "; ")
	run.recordFinishRefusal(text)
	e.setFinishVerdict(run, tc.ID, text, true)
	run.InjectNote(text)
	_ = ctx
}

// execFinishBatch executes one single-`finish` batch and applies the
// protocol. The tool itself runs through the normal pipeline (so the
// journal, history and trial records stay complete); loop detection,
// novelty tracking and working-notes harvesting are skipped for the
// control call. The finish tool result carries the verdict (D1).
// It reports whether the run left running. A refusal never ends the
// run (D2): only an accepted finish, a blocked acceptance or the
// budget does.
func (e *Engine) execFinishBatch(ctx context.Context, run *Run, tc providers.ToolCall, assistantText string) (bool, error) {
	results := e.runParallel(ctx, run, []providers.ToolCall{tc})
	res := results[0]
	// The result reaching the model carries the time footer (deadline.go).
	res.Output = withFooter(res.Output, e.timeFooterFor(run))
	run.mu.Lock()
	seq := run.nextHistorySeqLocked()
	run.History = append(run.History, historyEventForTool(seq, tc, res))
	run.mu.Unlock()
	run.recordFinishAttempt(string(tc.Arguments), assistantText)
	args, err := ParseFinishArgs(tc.Arguments)
	if err != nil {
		e.refuseCall(ctx, run, tc, []string{strings.TrimPrefix(err.Error(), "finish refused: ")})
		// Argument corrections are new control evidence; they need no
		// filesystem work before the corrected finish can be checked.
		run.mu.Lock()
		run.fin.refused = false
		run.mu.Unlock()
		return false, nil
	}
	if reasons, ok := run.unchangedRefusal(); ok && (e.timeRemains(run) || !allSoftReasons(reasons)) {
		if hard := e.hardFinishReasons(run, reasons); len(hard) > 0 {
			e.noChangeRefusal(ctx, run, tc, hard)
			return false, nil
		}
	}
	dec := e.decideFinish(ctx, run, args, finishEvidenceText(args, assistantText))
	if len(dec.reasons) > 0 && !dec.soft {
		e.refuseCall(ctx, run, tc, dec.reasons)
		return false, nil
	}
	run.clearRefusal()
	if args.Status == "blocked" {
		run.storeFinish(args, assistantText)
		e.setFinishVerdict(run, tc.ID, FinishAcceptedText, false)
		return true, e.completeBlocked(ctx, run, args)
	}
	if len(dec.reasons) > 0 {
		// Soft items with the wall budget nearly spent: accept as
		// completed_with_pending (D2) instead of refusing again.
		run.storeFinish(args, assistantText)
		e.setFinishVerdict(run, tc.ID, FinishAcceptedText+" Completed with pending items: "+strings.Join(dec.reasons, "; "), false)
		return true, e.completeWithPending(ctx, run, dec.reasons)
	}
	if !run.confirmArmed() {
		run.armConfirm(args)
		text := e.confirmationText(run)
		e.setFinishVerdict(run, tc.ID, text, false)
		run.InjectNote(text)
		return false, nil
	}
	run.storeFinish(args, assistantText)
	reasons, err := e.completeFinish(ctx, run, args)
	if len(reasons) > 0 {
		e.refuseCall(ctx, run, tc, reasons)
		return false, nil
	}
	if err != nil {
		return true, err
	}
	e.setFinishVerdict(run, tc.ID, FinishAcceptedText, false)
	return true, nil
}

// finishEvidenceText is the verification input for one `finish`
// call: the evidence plus the summary, falling back to the assistant
// text when evidence is thin.
func finishEvidenceText(args FinishArgs, assistantText string) string {
	if strings.TrimSpace(args.Evidence) != "" {
		if strings.TrimSpace(args.Summary) != "" {
			return args.Summary + "\n" + args.Evidence
		}
		return args.Evidence
	}
	return assistantText
}

// completeFinish moves running -> verifying -> completed for an
// accepted second `finish`. The verification already passed in
// validateFinish; the gate report is refreshed for the records. It
// returns the refusal reasons when the world moved between validation
// and completion (the run goes back to work).
func (e *Engine) completeFinish(ctx context.Context, run *Run, args FinishArgs) ([]string, error) {
	run.mu.Lock()
	from := run.state
	if from == types.RunRunning {
		if err := run.transition(types.RunVerifying); err != nil {
			run.mu.Unlock()
			return nil, err
		}
	}
	run.mu.Unlock()
	e.recordState(ctx, run, from, types.RunVerifying, "explicit finish")
	rep := e.gateCheck(run)
	rep = e.selfVerify(ctx, run, rep, finishEvidenceText(args, ""))
	run.mu.Lock()
	run.LastGate = rep
	run.mu.Unlock()
	rep.Pendings = e.hardFinishReasons(run, rep.Pendings)
	if !rep.OK && len(rep.Pendings) > 0 {
		// The world moved between validation and completion (a
		// background session settled, a file changed): return to
		// work with the fresh pendings instead of completing.
		run.mu.Lock()
		run.finishConfirmPending = true
		back := run.state
		_ = run.transition(types.RunRunning)
		run.mu.Unlock()
		e.recordState(ctx, run, back, types.RunRunning, "finish race: re-verify")
		return rep.Pendings, nil
	}
	return nil, e.complete(ctx, run, types.RunCompleted, "explicit finish")
}

// completeWithPending moves running -> verifying -> completed_with_pending
// for a finish accepted with soft items only (D2).
func (e *Engine) completeWithPending(ctx context.Context, run *Run, pending []string) error {
	run.mu.Lock()
	from := run.state
	if from == types.RunRunning {
		if err := run.transition(types.RunVerifying); err != nil {
			run.mu.Unlock()
			return err
		}
	}
	run.mu.Unlock()
	e.recordState(ctx, run, from, types.RunVerifying, "explicit finish")
	return e.complete(ctx, run, types.RunCompletedPending, "pending: "+truncOneLine(strings.Join(pending, "; "), 200))
}

// completeBlocked moves running -> verifying -> blocked for an
// accepted `finish` with status blocked.
func (e *Engine) completeBlocked(ctx context.Context, run *Run, args FinishArgs) error {
	reason := args.Reason
	if reason == "" {
		reason = args.Summary
	}
	run.mu.Lock()
	from := run.state
	if from == types.RunRunning {
		if err := run.transition(types.RunVerifying); err != nil {
			run.mu.Unlock()
			return err
		}
	}
	run.mu.Unlock()
	e.recordState(ctx, run, from, types.RunVerifying, "explicit finish blocked")
	run.mu.Lock()
	back := run.state
	if back == types.RunVerifying {
		_ = run.transition(types.RunBlocked)
		run.blockReason = types.BlockMissingData
	}
	run.mu.Unlock()
	e.recordState(ctx, run, back, types.RunBlocked, "blocked: "+truncOneLine(reason, 200))
	if e.deps.LeaseRelease != nil {
		e.deps.LeaseRelease(run.ID)
	}
	return nil
}

// finishSafetyReason bounds non-interactive refusal loops even when a
// caller omits budgets. Other tool work resets the refusal streak.
func (e *Engine) finishSafetyReason(run *Run) string {
	if !e.explicitFinishRequired(run) {
		return ""
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	streak := 0
	for i := len(run.History) - 1; i >= 0; i-- {
		tr := run.History[i].ToolResult
		if tr == nil {
			continue
		}
		if tr.Name != FinishToolName {
			break
		}
		if strings.HasPrefix(tr.Output, "finish refused") {
			streak++
		}
	}
	if streak >= 25 {
		return "safety stop: 25 consecutive refused finishes without other tool work"
	}
	// A model that keeps answering without any tool call (an empty or
	// broken provider stream included) cannot make progress; stop after
	// MaxTextOnlyTurns in a row instead of spinning until the wall clock.
	if r := run.textOnlyStreak; r >= MaxTextOnlyTurns {
		return fmt.Sprintf("safety stop: %d consecutive turns without a tool call", r)
	}
	if run.Tracker.Limit == (tasks.Limit{}) && run.Steps > 200 {
		return "safety stop: 200 turns without a configured budget"
	}
	return ""
}

// The arguments, output and status always come from one joined history entry.
func lastNonFinishEntry(run *Run) (tasks.HistoryTool, bool) {
	run.mu.Lock()
	defer run.mu.Unlock()
	history := historyTools(run.finishHistoryLocked())
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Name != FinishToolName {
			return history[i], true
		}
	}
	return tasks.HistoryTool{}, false
}

func historyCommand(h tasks.HistoryTool) string {
	var a struct {
		Command string `json:"command"`
	}
	if h.Name == "exec" {
		_ = json.Unmarshal([]byte(h.Args), &a)
	}
	return a.Command
}

func finishErrorEntryEnabled() bool {
	v, set := envSwitch("EXCALIBUR_FINISH_ERROR_ENTRY")
	return !set || v
}

// markLoopRefusedLocked preserves engine provenance instead of trusting process
// output text. The finish gate must assess the last executed command, even when
// a repeat refusal followed it. Callers hold r.mu.
func (r *Run) markLoopRefusedLocked(seq int64) {
	if r.loopRefusedResults == nil {
		r.loopRefusedResults = make(map[int64]bool)
	}
	r.loopRefusedResults[seq] = true
}

// finishHistoryLocked returns an owned evidence view. The conversation keeps
// every refusal, but unexecuted calls cannot pass or fail verification.
// Callers hold r.mu.
func (r *Run) finishHistoryLocked() []ctxcompiler.ActivityEvent {
	out := make([]ctxcompiler.ActivityEvent, 0, len(r.History))
	filter := switchOn("EXCALIBUR_FINISH_EXECUTED_ERRORS")
	for _, ev := range r.History {
		if !filter || !r.loopRefusedResults[ev.Seq] {
			out = append(out, ev)
		}
	}
	return out
}
