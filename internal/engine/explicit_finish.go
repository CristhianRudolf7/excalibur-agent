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
// bounded by a fixed small count: only the time/step budget bounds
// them (closing mode near the end).
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishConfirmPending = true
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
	run.mu.Lock()
	defer run.mu.Unlock()
	for i := len(run.History) - 1; i >= 0; i-- {
		tr := run.History[i].ToolResult
		if tr == nil || tr.Name == FinishToolName {
			continue
		}
		return tr.Output, tr.IsError, true
	}
	return "", false, false
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

// validateFinish runs the refusal checks for one parsed `finish`
// call: the agent's own last command must not have errored, the full
// verification (gate + self-verify + artifacts + strong verifier)
// must pass, and a blocked claim must not name paths that exist. It
// returns the refusal reason ("" when accepted).
func (e *Engine) validateFinish(ctx context.Context, run *Run, args FinishArgs, evidenceText string) string {
	e.restoreRefusals(ctx, run)
	var reasons []string
	if out, isErr, found := lastNonFinishError(run); found && isErr {
		reason := "your last command errored (" + truncOneLine(out, 200) + "). Fix it and verify by executing before calling `finish` again."
		if !ActionableRefusalsEnabled() {
			return reason
		}
		reasons = append(reasons, reason)
	}
	if args.Status == "blocked" {
		if len(reasons) > 0 {
			return e.refuseFinish(ctx, run, reasons)
		}
		if existing := e.blockedPathsExist(run, args.Reason); existing != "" {
			return "blocked claim refused: path " + existing + " exists. Re-read the filesystem result and continue working instead of declaring blocked."
		}
		if refusal := e.strictBlockedRefusal(run, args.Reason); refusal != "" {
			// Count strict refusals separately for the trial
			// records; the generic FinishRefusals counter is
			// bumped by the caller.
			run.mu.Lock()
			run.StrictBlockedRefusals++
			run.mu.Unlock()
			return refusal
		}
		return ""
	}
	gate := e.gateCheck(run)
	merged := e.selfVerify(ctx, run, gate, evidenceText)
	if !merged.OK {
		reasons = append(reasons, merged.Pendings...)
	}
	if len(reasons) > 0 {
		return e.refuseFinish(ctx, run, reasons)
	}
	return ""
}

// execFinishBatch executes one single-`finish` batch and applies the
// protocol. The tool itself runs through the normal pipeline (so the
// journal, history and trial records stay complete); loop detection,
// novelty tracking and working-notes harvesting are skipped for the
// control call. It reports whether the run left running.
func (e *Engine) execFinishBatch(ctx context.Context, run *Run, tc providers.ToolCall, assistantText string) (bool, error) {
	results := e.runParallel(ctx, run, []providers.ToolCall{tc})
	res := results[0]
	run.mu.Lock()
	seq := run.nextHistorySeqLocked()
	run.History = append(run.History, historyEventForTool(seq, tc, res))
	run.mu.Unlock()
	run.recordFinishAttempt(string(tc.Arguments), assistantText)
	args, err := ParseFinishArgs(tc.Arguments)
	if err != nil {
		reason := e.refuseFinish(ctx, run, []string{err.Error()})
		run.recordFinishRefusal(reason)
		run.InjectNote(reason)
		return false, nil
	}
	if reason := e.validateFinish(ctx, run, args, finishEvidenceText(args, assistantText)); reason != "" {
		if !strings.HasPrefix(reason, "finish refused: ") {
			reason = e.refuseFinish(ctx, run, []string{reason})
		}
		run.recordFinishRefusal(reason)
		run.InjectNote(reason)
		return false, nil
	}
	run.clearRefusal()
	if args.Status == "blocked" {
		run.storeFinish(args, assistantText)
		return true, e.completeBlocked(ctx, run, args)
	}
	if !run.confirmArmed() {
		run.armConfirm(args)
		run.InjectNote(ConfirmAfterFinish)
		return false, nil
	}
	run.storeFinish(args, assistantText)
	return true, e.completeFinish(ctx, run, args)
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
// validateFinish; the gate report is refreshed for the records.
func (e *Engine) completeFinish(ctx context.Context, run *Run, args FinishArgs) error {
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
	rep := e.gateCheck(run)
	rep = e.selfVerify(ctx, run, rep, finishEvidenceText(args, ""))
	run.mu.Lock()
	run.LastGate = rep
	run.mu.Unlock()
	if !rep.OK {
		// The world moved between validation and completion (a
		// background session settled, a file changed): return to
		// work with the fresh pendings instead of completing.
		run.mu.Lock()
		run.finishConfirmPending = true
		back := run.state
		_ = run.transition(types.RunRunning)
		run.mu.Unlock()
		e.recordState(ctx, run, back, types.RunRunning, "finish race: re-verify")
		reason := e.refuseFinish(ctx, run, rep.Pendings)
		run.InjectNote(reason)
		run.recordFinishRefusal(reason)
		return nil
	}
	return e.complete(ctx, run, types.RunCompleted, "explicit finish")
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
