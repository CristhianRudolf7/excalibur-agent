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
// external impossibility (lowercased, substring match).
var validationMarks = []string{
	"fail", "error", "verif", "test ", "tests ", "implement",
	"bug", "incorrect", "wrong", "not work", "does not work",
	"no valid", "still", "unable to", "cannot", "hull",
	"exception", "traceback", "assertion",
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

// strictBlockedRefusal builds the structured recovery prompt for a
// validation-failure blocked claim while time remains: it states the
// failing check and its output, demands three materially different
// approaches, picks one, and continues. Empty means the claim carries
// external evidence, names operator-missing data without validation
// language, or no time remains, and may proceed.
func (e *Engine) strictBlockedRefusal(run *Run, reason string) string {
	if !StrictBlockedEnabled() {
		return ""
	}
	// Only non-interactive runs enforce the strict policy; interactive
	// daemon runs keep the legacy path-exists-only check.
	if !e.explicitFinishRequired(run) {
		return ""
	}
	if !e.timeRemains(run) {
		return ""
	}
	history := historyTools(run.historyCopy())
	if hasExternalEvidence(reason, history) {
		return ""
	}
	// Operator-missing-data claims without validation language stay
	// accepted (e.g. "need /data/secret.csv from the operator"): the
	// strict policy only refuses validation failures while time
	// remains.
	if !looksLikeValidationFailure(reason, history) {
		return ""
	}
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
	b.WriteString("Failing check/output: ")
	b.WriteString(failing)
	b.WriteString(". List three materially different approaches to fix it, pick one, and execute its first step now. ")
	b.WriteString("Do not declare blocked for validation failures while time remains; leave the best deliverable in place and keep working.")
	return b.String()
}
