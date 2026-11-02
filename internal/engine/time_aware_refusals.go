package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

const timeAwareRefusalCapSwitch = "EXCALIBUR_TIME_AWARE_REFUSAL_CAP"

// The verifier supplies the semantic judgment. Literal citations or failed
// request-derived draft IDs bind that judgment to an explicit user property;
// unavailability, generic disagreement and invented requirements do not qualify.
const unmetPropertyReview = "For a substantive FAIL naming an unmet explicit request property, also return UNMET_PROPERTY: <verbatim request span> | <specific missing or failed evidence>. Cite only properties the user explicitly requested; omit this line for unavailable verification or generic uncertainty.\n"

const unmetPropertyPrefix = "unmet explicit request property "

func substantiveVerifierReasons(reply, request string, drafts []tasks.DraftCriterion) []string {
	verdict := tasks.ParseStrongVerdict(reply, drafts)
	if !verdict.Explicit || verdict.Pass {
		return nil
	}
	var reasons []string
	seen := map[string]bool{}
	add := func(property, detail string) {
		property = strings.Trim(strings.TrimSpace(property), "\"`")
		detail = strings.TrimSpace(detail)
		if property == "" || detail == "" || !strings.Contains(request, property) || seen[property] {
			return
		}
		seen[property] = true
		reasons = append(reasons, fmt.Sprintf("%s%q: %s", unmetPropertyPrefix, property, detail))
	}
	for _, line := range strings.Split(reply, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		if strings.EqualFold(key, "PROPERTY") {
			if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(value)), "FAIL - ") {
				continue
			}
			value = strings.TrimSpace(value)[len("FAIL - "):]
		} else if !strings.EqualFold(key, "UNMET_PROPERTY") {
			continue
		}
		property, detail, ok := strings.Cut(value, "|")
		if ok {
			add(property, detail)
		}
	}
	// Existing per-draft verdicts remain usable without the additional line.
	// A literal key/statement is required so inferred inspection criteria alone
	// cannot extend a refusal beyond the normal cap.
	for _, id := range verdict.FailedDrafts {
		for _, d := range drafts {
			if d.ID != id {
				continue
			}
			property := d.Statement
			if property == "" || !strings.Contains(request, property) {
				property = d.Key
			}
			for _, reason := range verdict.Reasons {
				if strings.HasPrefix(reason, id+": ") {
					add(property, strings.TrimPrefix(reason, id+": "))
				}
			}
		}
	}
	return reasons
}

type refusalProgress struct{ mark, count int }

// Only autonomous exec/eval runs with a known wall limit receive this policy.
// The half-budget boundary is strict; assumed estimates keep the existing cap.
func (e *Engine) earlyExplicitRefusalTime(run *Run) (time.Duration, bool) {
	if !switchOn(timeAwareRefusalCapSwitch) || !e.explicitFinishRequired(run) {
		return 0, false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	total := time.Duration(run.Tracker.Limit.MaxSeconds) * time.Second
	elapsed := e.deps.Clock().Sub(run.StartedAt)
	return total - elapsed, run.Profile.WallSource == "explicit" && total > 0 && !run.StartedAt.IsZero() && elapsed >= 0 && elapsed < total/2
}

// Callers hold run.mu. Match only reasons from the latest actual verifier FAIL.
func (run *Run) substantiveRefusalLocked(reason string) bool {
	report := run.finishVerifierReport
	if report.request != run.Request || !report.explicit || !report.failed {
		return false
	}
	if len(report.properties) == 0 {
		return false
	}
	for _, failedReason := range report.reasons {
		if strings.Contains(reason, failedReason) {
			return true
		}
	}
	return false
}

func (e *Engine) protectVerifierRefusal(run *Run, reasons []string) bool {
	if _, early := e.earlyExplicitRefusalTime(run); !early {
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	for _, reason := range reasons {
		if run.substantiveRefusalLocked(reason) {
			return true
		}
	}
	return false
}

func (e *Engine) timeAwareRefusalNote(run *Run, reasons []string) string {
	remaining, early := e.earlyExplicitRefusalTime(run)
	if !early {
		return ""
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	for _, reason := range reasons {
		if run.substantiveRefusalLocked(reason) {
			return fmt.Sprintf(" Explicit wall budget: %d s remain; %s. Correct and verify this before finishing.", int64(remaining/time.Second), strings.Join(run.finishVerifierReport.properties, "; "))
		}
	}
	return ""
}
