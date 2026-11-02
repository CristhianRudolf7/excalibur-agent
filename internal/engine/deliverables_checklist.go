// Confirmation checklist for the first passing finish (agent quality,
// work package D, D5). The harness extracts up to 8 requirement
// sentences from the literal instruction, deterministically, and asks
// the agent to state the measured value or the command output for each
// in its next finish evidence. Soft warnings (D4, D9) and advisory
// verifier notes (D6) follow the checklist. Nothing here refuses: the
// second finish ends the run as before.
package engine

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// checklistMaxItems caps the extracted requirements.
const checklistMaxItems = 8

// checklistMarkerRe marks requirement sentences: obligation words,
// comparisons and thresholds, numbers with units.
var checklistMarkerRe = regexp.MustCompile(`(?i)\b(must|should|exactly|at most|at least|no more than|no less than|only|without|never|required|requires|ensure)\b|[<>]=?\s*\d|\b\d+(?:\.\d+)?\s?(?:ms|s|sec|seconds|minutes|kb|mb|gb|%|px|bytes|rows|lines|items|points|files|tests)\b`)

// checklistNamedRe marks sentences that name a file, a command or an
// example, which the agent must reproduce exactly.
var checklistNamedRe = regexp.MustCompile(`(?i)(\.(?:py|js|go|rs|json|csv|txt|md|sh|bin|so|png|html|xml|yaml|yml|toml)\b|/[\w.-]+/|\bexample\b|e\.g\.|for instance|such as|\bcommand\b)`)

// These are instruction semantics, independent of any domain or file type.
var broadOccurrenceRe = regexp.MustCompile(`(?i)\b(all|every|any)\b`)
var broadRemovalRe = regexp.MustCompile(`(?i)\b(?:(?:remov|replac|delet|sanitiz)(?:e|es|ed|ing)|redact(?:s|ed|ing)?)\b`)

const broadVerificationItem = "the instruction asks for every occurrence: show a final broad pattern search over the whole tree and its result"

// checklistItems returns up to max requirement sentences of the
// instruction, in order, without duplicates.
func legacyChecklistItems(request string, max int) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range requestSentences(request) {
		if !checklistMarkerRe.MatchString(s) && !checklistNamedRe.MatchString(s) {
			continue
		}
		norm := strings.ToLower(s)
		if seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, truncOneLine(s, 160))
		if len(out) >= max {
			break
		}
	}
	return out
}

var checklistNumberRe = regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?(?:e[+-]?\d+)?\b`)
var checklistLimitRe = regexp.MustCompile(`(?i)[<>]=?|%|\b(at most|at least|under|below|above|exceed\w*|threshold|within|limit\w*|no more than|no less than|must)\b`)
var checklistBulletRe = regexp.MustCompile(`^\s*(?:[-*+]\s+|\d+[.)]\s+)`)
var checklistBoundaryRe = regexp.MustCompile(`[!?]\s+|\.\s+`)

// Quoted paths can contain whitespace, so word count alone is insufficient.
func checklistOnlyPath(s string) bool {
	s = strings.TrimSpace(s)
	path := strings.Trim(s, "`\"'")
	quoted := path != s && len(s) >= 2 && s[0] == s[len(s)-1]
	if !quoted && len(strings.Fields(path)) != 1 {
		return false
	}
	return checklistNamedRe.MatchString(path) && (strings.Contains(path, "/") || strings.Contains(path, "."))
}

// Rank numeric constraints ahead of descriptive obligations. Stable sorting
// preserves instruction order among equal scores; decimals and exponents stay
// intact because only punctuation followed by whitespace ends a sentence.
func checklistItems(request string, max int) []string {
	if !switchOn("EXCALIBUR_CHECKLIST_LIMIT_RANKING") {
		return legacyChecklistItems(request, max)
	}
	if max <= 0 {
		return nil
	}
	if max > 8 {
		max = 8
	}
	type candidate struct {
		text  string
		score int
	}
	var candidates []candidate
	seen := map[string]bool{}
	for _, line := range strings.Split(request, "\n") {
		line = checklistBulletRe.ReplaceAllString(line, "")
		for _, part := range checklistBoundaryRe.Split(line, -1) {
			s := strings.TrimSpace(part)
			if checklistOnlyPath(s) || len(strings.Fields(s)) < 4 {
				continue
			}
			// A path alone is never a requirement, even when its name is numeric.
			if !checklistMarkerRe.MatchString(s) && !checklistNamedRe.MatchString(s) && !checklistLimitRe.MatchString(s) {
				continue
			}
			norm := strings.ToLower(strings.Join(strings.Fields(strings.Trim(s, " .;|")), " "))
			if seen[norm] {
				continue
			}
			seen[norm] = true
			score := 1
			if checklistMarkerRe.MatchString(s) || checklistLimitRe.MatchString(s) {
				score = 2
			}
			if checklistNumberRe.MatchString(s) && checklistLimitRe.MatchString(s) {
				score = 4
			}
			candidates = append(candidates, candidate{s, score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if len(candidates) > max {
		candidates = candidates[:max]
	}
	var out []string
	for _, c := range candidates {
		out = append(out, c.text)
	}
	return out
}

// confirmationText is the verdict of the first passing finish (D1, D5):
// the checks passed, the checklist to confirm with measured values, the
// soft warnings and any advisory verifier note. The run ends only on the
// next finish call.
func (e *Engine) confirmationText(run *Run) string {
	const tail = " Call finish again to end the run, or keep working."
	run.mu.Lock()
	auditRequest := run.verificationRequestLocked()
	run.mu.Unlock()
	var editNotes strings.Builder
	for _, item := range e.editScopeAudit(run, auditRequest) {
		if item.note != "" {
			editNotes.WriteString("\n" + item.note)
		}
	}
	if !FinishChecklistEnabled() {
		return "finish checks passed." + editNotes.String() + tail
	}
	run.mu.Lock()
	request := run.Request
	advisory := append([]string(nil), run.fin.advisory...)
	finishAdvisory := append([]string(nil), run.fin.finishAdvisory...)
	run.mu.Unlock()
	var b strings.Builder
	b.WriteString("finish checks passed.")
	items := checklistItems(request, checklistMaxItems)
	if switchOn("EXCALIBUR_CHECKLIST_EXAMPLES") {
		items = append(items, instructionExamples(request)...)
	}
	if switchOn("EXCALIBUR_BROAD_REVERIFICATION") && broadOccurrenceRe.MatchString(request) && broadRemovalRe.MatchString(request) {
		// Advisory confirmation only: this never refuses a finish by itself.
		items = append(items, broadVerificationItem)
	}
	if len(items) > 0 {
		b.WriteString(" Before the run ends, confirm: ")
		for i, it := range items {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(strconv.Itoa(i+1) + ") " + strings.TrimRight(it, ". ") + ".")
		}
		b.WriteString(" For each item, state the measured value or the command output in the evidence of your next finish call.")
	}
	if warnings := e.softFinishWarnings(run, request, historyTools(run.historyCopy())); len(warnings) > 0 {
		b.WriteString(" Warnings:")
		for i, w := range warnings {
			b.WriteString(" " + string(rune('a'+i)) + ") " + strings.TrimRight(w, ". ") + ".")
		}
	}
	if len(advisory) > 0 {
		b.WriteString(" Verifier note (advisory): " + truncOneLine(advisory[0], 200) + ".")
	}
	for _, note := range finishAdvisory {
		b.WriteString(" Advisory: " + note + ".")
	}
	b.WriteString(editNotes.String())
	b.WriteString(tail)
	return b.String()
}
