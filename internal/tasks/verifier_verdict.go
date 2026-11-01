package tasks

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// VerifierVerdictEnabled controls bounded verdict recovery and evidence checks.
func VerifierVerdictEnabled() bool { return envBool("EXCALIBUR_VERIFIER_VERDICT", true) }

// VerdictRetryEnabled controls the single retry of an empty or
// truncated verifier verdict with a larger output budget (default
// ON). EXCALIBUR_VERDICT_RETRY=off keeps the retry but at the
// original budget, the behaviour before this switch existed.
func VerdictRetryEnabled() bool { return envBool("EXCALIBUR_VERDICT_RETRY", true) }

// verdictRetryCap bounds the enlarged retry budget in output tokens.
const verdictRetryCap = 32768

// VerdictRetryBudget returns the output budget for the retry of an
// empty or truncated verdict: twice the first attempt's budget,
// bounded by verdictRetryCap, when VerdictRetryEnabled is on.
// Otherwise it returns the first budget unchanged.
func VerdictRetryBudget(first int) int {
	if !VerdictRetryEnabled() || first <= 0 {
		return first
	}
	next := first * 2
	if next > verdictRetryCap {
		next = verdictRetryCap
	}
	if next < first {
		return first
	}
	return next
}

// ErrVerifierNoVerdict is returned when the verifier produced no
// usable verdict after its bounded retry (empty, truncated or
// unparsable). It is an absence of judgement: callers must treat it
// as advisory, never as a pass and never as an acceptance by itself.
var ErrVerifierNoVerdict = errors.New("verifier produced no verdict; unverified: retry verification with executable behavioural evidence")

var existenceCommand = regexp.MustCompile(`(?i)^(?:command\s+)?(?:/[a-z0-9_./-]+/)?(?:test\s+-[esfd]|\[\s+-[esfd]|ls(?:\s|$)|stat(?:\s|$)|wc(?:\s|$))`)
var shellWrapper = regexp.MustCompile(`^(?:/[a-z0-9_./-]+/)?(?:bash|sh|dash)\s+-(?:lc|cl|c)\s+`)
var shellBoundary = regexp.MustCompile(`&&|\|\||[;|\n]`)

// ExistenceOnlyEvidence recognizes metadata probes, independently of filenames
// that contain interpreter or verification words. Mixed behaviour checks remain
// available for semantic review by the verifier.
func ExistenceOnlyEvidence(h HistoryTool) bool {
	var args struct {
		Command   string          `json:"command"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal([]byte(h.Args), &args) != nil {
		return false
	}
	command := args.Command
	if command == "" && len(args.Arguments) > 0 {
		if json.Unmarshal(args.Arguments, &args) == nil {
			command = args.Command
		}
	}
	if command == "" {
		return false
	}
	command = strings.TrimSpace(command)
	if prefix := shellWrapper.FindString(command); prefix != "" {
		body := strings.TrimSpace(strings.TrimPrefix(command, prefix))
		if len(body) >= 2 && (body[0] == '\'' || body[0] == '"') && body[len(body)-1] == body[0] {
			command = body[1 : len(body)-1]
		}
	}
	// Substitutions may execute behaviour; leave their assessment to the verifier.
	if strings.Contains(command, "$(") || strings.Contains(command, "`") {
		return false
	}
	probes := false
	for _, part := range shellBoundary.Split(command, -1) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if existenceCommand.MatchString(part) {
			probes = true
			continue
		}
		// Harmless working-directory and display commands do not exercise output.
		if strings.HasPrefix(part, "cd ") || strings.HasPrefix(part, "echo ") || strings.HasPrefix(part, "printf ") {
			continue
		}
		return false
	}
	return probes
}

var behaviouralRequirement = regexp.MustCompile(`(?i)\b(outputs?|prints?|returns?|responds?|answers?|serves?|services?|converts?|conversion|correctness|accuracy|thresholds?|latency|throughput|at least|at most|less than|greater than)\b`)

// BehaviouralEvidencePending catches metadata-only evidence even when no draft
// path was extracted. It never interprets existence or byte counts as behaviour.
func BehaviouralEvidencePending(request string, history []HistoryTool) []string {
	if !VerifierVerdictEnabled() || !behaviouralRequirement.MatchString(request) {
		return nil
	}
	probes := false
	start := lastChangeIndex(history) + 1
	for _, h := range history[start:] {
		if h.IsError || isStillRunning(h.Output) {
			continue
		}
		if h.Name != "exec" && h.Name != "call_tool" {
			continue
		}
		if ExistenceOnlyEvidence(h) {
			probes = true
			continue
		}
		if IsExercisingCall(h) {
			return nil
		}
	}
	if probes {
		return []string{"behaviour is unverified: existence/size checks (test -s, ls, stat, wc) alone cannot verify program output, service responses, conversion correctness or numeric thresholds; exercise the behaviour and assert the expected result"}
	}
	return nil
}
