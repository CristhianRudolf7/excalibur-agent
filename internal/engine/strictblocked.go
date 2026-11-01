package engine

import (
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

// Strict blocked policy (director's W8 design, non-interactive profiles).
//
// `finish status=blocked` is accepted only with deterministic evidence
// of an external impossibility observed in the agent's own tool history:
// a required network resource unreachable or 404 after retries, a
// required file or tool provably absent and not installable, or an
// explicit permission denial outside the agent's control.
//
// "My solution fails validation" is never a valid blocked reason while
// time remains: the harness refuses with a structured recovery prompt
// (failing check and its output, three materially different approaches,
// pick one, continue). Refusals are counted in FinishRefusals plus the
// dedicated StrictBlockedRefusals counter surfaced in the trial
// records.

// strictNetworkMarks are deterministic network-failure signals in tool
// outputs (lowercased, substring match).
var strictNetworkMarks = []string{
	"connection refused", "connection reset", "connection timed out",
	"could not resolve", "name resolution", "network is unreachable",
	"no route to host", "dns", "404", "not found", "failed to fetch",
	"could not connect", "timed out", "timeout",
}

// strictPermissionMarks are deterministic permission-denial signals.
var strictPermissionMarks = []string{
	"permission denied", "operation not permitted", "access denied",
	"eacces", "eperm", "403", "forbidden",
}

// strictMissingMarks name a missing file or tool.
var strictMissingMarks = []string{
	"command not found", "no such file", "not found", "missing",
	"not installed", "cannot find", "can't find",
}

// strictInstallMarks prove an install was attempted.
var strictInstallMarks = []string{
	"apt-get", "apt ", "pip install", "uv pip", "npm install",
	"yarn add", "pnpm add", "gem install", "cargo add", "go get",
	"install.packages", "cpan", "planinstall", "provision:",
}

func containsAnyLower(hay string, marks []string) string {
	for _, m := range marks {
		if strings.Contains(hay, m) {
			return m
		}
	}
	return ""
}

// timeRemains reports whether the run still has wall-clock (or step)
// budget left: closing has not been reached. Uncapped runs count as
// time remaining so validation failures never block by default.
func (e *Engine) timeRemains(run *Run) bool {
	run.mu.Lock()
	limit := run.Tracker.Limit
	used := run.Tracker.Used
	elapsed := run.HeartbeatAt.Sub(run.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	run.mu.Unlock()
	if limit.MaxSeconds > 0 {
		total := int64(limit.MaxSeconds)
		left := total - int64(elapsed/time.Second)
		// Closing starts below 10% remainder: anything above that is
		// time remaining.
		threshold := int64(tasks.ClosingFraction * float64(total))
		if threshold < 1 {
			threshold = 1
		}
		return left > threshold
	}
	if limit.MaxSteps > 0 {
		left := limit.MaxSteps - used.Steps
		th := int(tasks.ClosingFraction * float64(limit.MaxSteps))
		if th < 1 {
			th = 1
		}
		return left > th
	}
	return true
}

// hasExternalEvidence checks the blocked reason plus the agent's own
// tool history for deterministic external-impossibility evidence.
func hasExternalEvidence(reason string, history []tasks.HistoryTool) bool {
	rl := strings.ToLower(reason)
	var outputs strings.Builder
	failCount := 0
	for _, h := range history {
		if h.IsError || h.Output != "" {
			outputs.WriteString("\n")
			outputs.WriteString(strings.ToLower(h.Output))
			outputs.WriteString("\n")
			outputs.WriteString(strings.ToLower(h.Args))
			if h.IsError {
				failCount++
			}
		}
	}
	hl := outputs.String()

	// Permission denial outside the agent's control: reason names it
	// and the history shows it.
	if containsAnyLower(rl, strictPermissionMarks) != "" &&
		containsAnyLower(hl, strictPermissionMarks) != "" {
		return true
	}
	// Network resource unreachable/404: reason names a network
	// resource or failure and the history shows a network failure.
	networkInReason := containsAnyLower(rl, strictNetworkMarks) != "" ||
		strings.Contains(rl, "http://") || strings.Contains(rl, "https://") ||
		strings.Contains(rl, "url") || strings.Contains(rl, "host") ||
		strings.Contains(rl, "network") || strings.Contains(rl, "connection")
	networkInHistory := containsAnyLower(hl, strictNetworkMarks) != ""
	if networkInReason && networkInHistory {
		// Retries: at least two failed tool calls, or one failure
		// plus an explicit retry marker in the reason.
		if failCount >= 2 {
			return true
		}
		if strings.Contains(rl, "retry") || strings.Contains(rl, "retries") ||
			strings.Contains(rl, "after") || strings.Contains(hl, "retry") {
			return true
		}
		// A single crisp 404/connection-refused still counts when
		// both sides name it.
		if strings.Contains(hl, "404") || strings.Contains(hl, "connection refused") ||
			strings.Contains(hl, "could not resolve") || strings.Contains(hl, "network is unreachable") {
			return true
		}
	}
	// Required file/tool provably absent and not installable: the
	// reason names the missing piece, the history shows the miss,
	// and the history shows an install was attempted (and failed) or
	// the network is down so no install is possible.
	if containsAnyLower(rl, strictMissingMarks) != "" &&
		containsAnyLower(hl, strictMissingMarks) != "" {
		if containsAnyLower(hl, strictInstallMarks) != "" {
			return true
		}
		if containsAnyLower(hl, strictNetworkMarks) != "" {
			return true
		}
	}
	return false
}

// validationMarks are deterministic signals that a blocked reason
// describes the agent's own validation failure rather than an
// external impossibility (lowercased, substring match). W10 extends
// the W8 set with the two premature-blocked shapes seen in the
// v9-default records: the qemu-alpine-ssh self-stop ("stopped",
// "timed out", "criterion is unmet") and the extract-elf missing
// reference ("reference", "coverage", "mapping").
var validationMarks = []string{
	"fail", "error", "verif", "test ", "tests ", "implement",
	"bug", "incorrect", "wrong", "not work", "does not work",
	"no valid", "still", "unable to", "cannot", "hull",
	"exception", "traceback", "assertion",
	"reference", "coverage", "mapping", "agreement",
	"timed out", "timeout", "stopped", "unmet", "criterion",
	"could not complete", "not serving", "end-to-end", "end to end",
	"unavailable", "not available", "did not reach", "not reach",
	"mismatch",
}

// looksLikeValidationFailure reports whether the blocked reason reads
// as "my solution fails validation" after the agent already did work:
// the reason names a failure and the history holds tool calls.
func looksLikeValidationFailure(reason string, history []tasks.HistoryTool) bool {
	if len(history) == 0 {
		return false
	}
	rl := strings.ToLower(reason)
	for _, m := range validationMarks {
		if strings.Contains(rl, m) {
			return true
		}
	}
	return false
}

// isReferenceMissingClaim reports whether the blocked reason claims a
// missing reference solution, mapping or coverage target (W10): the
// reference is the hidden verifier, never a task input, so such a
// claim is always a validation failure, never an external
// impossibility.
func isReferenceMissingClaim(reason string) bool {
	rl := strings.ToLower(reason)
	if !strings.Contains(rl, "reference") {
		return false
	}
	for _, m := range []string{"missing", "absent", "not present", "not supplied", "no reference", "solution", "mapping", "coverage", "agreement"} {
		if strings.Contains(rl, m) {
			return true
		}
	}
	return true
}

// isSelfCausedUnavailability reports whether the blocked reason claims
// a service is unavailable after the agent itself stopped or killed it
// (W10, qemu-alpine-ssh shape): the history shows a kill/close/signal
// of the required process and the reason names the resulting
// unavailability. Killing a process the task requires is a failure,
// not an external impossibility.
func isSelfCausedUnavailability(reason string, history []tasks.HistoryTool) bool {
	if len(history) == 0 {
		return false
	}
	rl := strings.ToLower(reason)
	unavail := false
	for _, m := range []string{"stopped", "killed", "was stopped", "not available", "unavailable", "no longer", "not serving", "timed out", "timeout", "did not reach", "could not complete", "unmet"} {
		if strings.Contains(rl, m) {
			unavail = true
			break
		}
	}
	if !unavail {
		return false
	}
	for _, h := range history {
		hl := strings.ToLower(h.Args + "\n" + h.Output)
		if strings.Contains(hl, "kill ") || strings.Contains(hl, "kill\t") ||
			strings.Contains(strings.ToLower(h.Args), "\"signal\"") ||
			strings.Contains(strings.ToLower(h.Args), "\"close\"") ||
			strings.Contains(hl, "pkill") || strings.Contains(hl, "killall") {
			return true
		}
		if strings.Contains(strings.ToLower(h.Args), "kill") && strings.Contains(rl, "qemu") {
			return true
		}
	}
	// Even without an explicit kill in history, a reason that admits
	// the agent stopped the required service is self-caused.
	if strings.Contains(rl, "was stopped") || strings.Contains(rl, "qemu was stopped") ||
		strings.Contains(rl, "i stopped") || strings.Contains(rl, "we stopped") {
		return true
	}
	return false
}

// strictBlockedRefusal builds the structured recovery prompt for a
// blocked claim without deterministic external evidence while time
// remains (W8 strict policy plus W10 run-end fixes). Empty means the
// claim carries external evidence or is the legacy empty-history
// operator-missing-data case, and may proceed. Closing mode (no time
// remains) never accepts blocked: the run must end with `finish`
// status=completed and the best deliverable in place.
func (e *Engine) strictBlockedRefusal(run *Run, reason string) string {
	if !StrictBlockedEnabled() {
		return ""
	}
	// Only non-interactive runs enforce the strict policy; interactive
	// daemon runs keep the legacy path-exists-only check.
	if !e.explicitFinishRequired(run) {
		return ""
	}
	// Closing mode: time is almost exhausted. Never end blocked;
	// leave the best deliverable and complete.
	if !e.timeRemains(run) {
		return "finish refused (strict blocked): closing mode: time is almost exhausted; leave the best deliverable in place, verify what exists quickly, and call `finish` with status completed and evidence now. Do not declare blocked in closing."
	}
	history := historyTools(run.historyCopy())
	// The `finish` control calls themselves never count as work:
	// filter them so the legacy empty-history operator case
	// (TestExplicitFinishBlocked) still sees no prior work even
	// though the current attempt is already in history.
	workHistory := history[:0:0]
	for _, h := range history {
		if h.Name == FinishToolName {
			continue
		}
		workHistory = append(workHistory, h)
	}
	if hasExternalEvidence(reason, workHistory) {
		return ""
	}
	// Reference-solution claims are never external (W10): refuse with
	// the validation recovery shape so the agent keeps working with
	// the best deliverable instead of declaring blocked.
	if isReferenceMissingClaim(reason) {
		return e.validationRecoveryPrompt(run, reason, "the reference solution is the hidden verifier, never a task input: missing-reference is not a valid blocked reason")
	}
	// Self-caused unavailability is never external (W10): the agent
	// killed or stopped the service the task requires.
	if isSelfCausedUnavailability(reason, workHistory) {
		return e.validationRecoveryPrompt(run, reason, "killing or stopping a process the task requires is a failure, not an external impossibility: restart the service, verify it serves, and keep working")
	}
	// Legacy operator-missing-data stays accepted only with no prior
	// work (empty non-finish history) and no validation language, e.g.
	// "need /data/secret.csv from the operator" (TestExplicitFinishBlocked).
	// Any blocked after real work needs deterministic external evidence.
	if len(workHistory) == 0 && !looksLikeValidationFailure(reason, workHistory) {
		return ""
	}
	if len(workHistory) == 0 {
		// Empty work history but validation-shaped (e.g. reference claim
		// without prior calls): still refuse, since no external
		// evidence exists.
		return e.validationRecoveryPrompt(run, reason, "")
	}
	// Non-empty work history without external evidence: refuse. The
	// validation-shaped case gets the three-approaches recovery
	// prompt; the qemu-shaped unevidenced case (no validation
	// keywords yet no external proof) gets the same treatment with
	// the generic external-evidence wording so it is never accepted
	// with 0 refusals.
	if !looksLikeValidationFailure(reason, workHistory) {
		return e.genericBlockedRefusal(run, reason)
	}
	return e.validationRecoveryPrompt(run, reason, "")
}

// validationRecoveryPrompt builds the structured three-approaches
// recovery prompt for a validation-failure blocked claim.
func (e *Engine) validationRecoveryPrompt(run *Run, reason, extra string) string {
	lastOut, isErr, found := lastNonFinishError(run)
	failing := "no failing command recorded"
	if found {
		if isErr {
			failing = truncOneLine(lastOut, 300)
		} else {
			failing = "last command succeeded (" + truncOneLine(lastOut, 200) + "); verification still fails"
		}
	}
	var b strings.Builder
	b.WriteString("finish refused (strict blocked): ")
	b.WriteString(truncOneLine(reason, 200))
	b.WriteString(" names no external impossibility while time remains. ")
	if extra != "" {
		b.WriteString(extra)
		b.WriteString(". ")
	}
	b.WriteString("Failing check/output: ")
	b.WriteString(failing)
	b.WriteString(". List three materially different approaches to fix it, pick one, and execute its first step now. ")
	b.WriteString("Do not declare blocked for validation failures while time remains; leave the best deliverable in place and keep working.")
	return b.String()
}

// genericBlockedRefusal refuses an unevidenced blocked claim that does
// not match the validation shape (W10 qemu shape): no external
// impossibility was demonstrated, so the agent must keep working.
func (e *Engine) genericBlockedRefusal(run *Run, reason string) string {
	lastOut, isErr, found := lastNonFinishError(run)
	failing := "no failing command recorded"
	if found {
		if isErr {
			failing = truncOneLine(lastOut, 300)
		} else {
			failing = "last command succeeded (" + truncOneLine(lastOut, 200) + "); verification still fails"
		}
	}
	var b strings.Builder
	b.WriteString("finish refused (strict blocked): ")
	b.WriteString(truncOneLine(reason, 200))
	b.WriteString(" names no deterministic external impossibility (required network resource unreachable/404 after retries, required file/tool provably absent and not installable, explicit permission denial) while time remains. ")
	b.WriteString("Failing check/output: ")
	b.WriteString(failing)
	b.WriteString(". Demonstrate the external impossibility with retries and evidence, or list three materially different approaches to fix it, pick one, and execute its first step now. ")
	b.WriteString("Do not declare blocked without deterministic external evidence; leave the best deliverable in place and keep working. Killing or stopping a required service is a failure, not a blocked reason.")
	return b.String()
}
