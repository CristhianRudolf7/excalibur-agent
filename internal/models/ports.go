// Decider ports of Section 19.4 and the deterministic / decider /
// LLM boundary of Section 2.1.
//
// Each helper builds the bounded questions one port asks and maps
// the calibrated answer back to the harness action. Deterministic
// rules run first everywhere: the decider only fires when the rules
// leave genuine ambiguity, and triage never adds an LLM call
// (Section 6.1 cost rule: without a decider, or below confidence,
// the preanalysis result goes to the master inside its own turn).
package models

// Triage request types of Section 6.1.
var triageTypes = []string{
	"conversation", "status_query", "quick_action", "task",
	"correction_or_redirect", "schedule_or_reminder",
	"memory_instruction", "configuration",
}

// TriageQuestions builds the bounded triage questions for a message:
// the request type (choice), the scope (choice), durable
// instructions (noul) and whether clarification is needed (noul).
func TriageQuestions(message, context string, scopeOptions []string) []Question {
	scopes := append([]string(nil), scopeOptions...)
	if len(scopes) == 0 {
		scopes = []string{"general"}
	}
	return []Question{
		{Kind: KindChoice, Prompt: "request type of the message", Options: triageTypes, Context: "message: " + message + "\n" + context},
		{Kind: KindChoice, Prompt: "scope of the message", Options: scopes, Context: "message: " + message + "\n" + context},
		{Kind: KindNoul, Prompt: "the message carries durable instructions", Context: "message: " + message},
		{Kind: KindNoul, Prompt: "the message needs clarification before acting", Context: "message: " + message + "\n" + context},
	}
}

// FastLaneClass classifies a triage answer for the Section 6.1 fast
// lane: commands and resolved state/control queries are served
// without a model.
func FastLaneClass(requestType string, scopeResolved bool, p float64, t Thresholds) bool {
	switch requestType {
	case "status_query", "quick_action", "correction_or_redirect":
		return scopeResolved && Decide(p, t) == OutcomeAct
	default:
		return false
	}
}

// ShouldAsk applies Section 6.4: mandatory data without a default
// or an irreversible outward action always asks; otherwise the
// decider probability of the main interpretation decides, asking
// below AskThreshold (0.7) when rework is costly and assuming with
// a recorded assumption otherwise.
func ShouldAsk(mainProb float64, mandatoryMissing, irreversible bool, reworkCostly bool) bool {
	if mandatoryMissing || irreversible {
		return true
	}
	if reworkCostly && mainProb < AskThreshold {
		return true
	}
	return false
}

// Admission classes of Section 10.5.
type AdmissionClass string

// Admission classes.
const (
	AdmissionDuplicate     AdmissionClass = "duplicate"
	AdmissionRefinement    AdmissionClass = "refinement"
	AdmissionContradiction AdmissionClass = "contradiction"
	AdmissionNew           AdmissionClass = "new"
)

// Admission bands of Section 10.5 and Appendix B.
const (
	// AdmissionDuplicateAt is the cosine at or above which a same
	// entity candidate merges provenance deterministically.
	AdmissionDuplicateAt = 0.95
	// AdmissionAmbiguousBelow is the floor of the ambiguous band.
	AdmissionAmbiguousBelow = 0.80
)

// ClassifyAdmission applies the deterministic bands: at or above
// 0.95 with the same entity merges; below 0.80 is new; the band in
// between needs the decider. needsDecider is false for the two
// deterministic outcomes, when there is no decider configured the
// caller falls back to a cheap LLM call (Section 10.5).
func ClassifyAdmission(cosine float64, sameEntity bool) (AdmissionClass, bool) {
	if sameEntity && cosine >= AdmissionDuplicateAt {
		return AdmissionDuplicate, false
	}
	if cosine < AdmissionAmbiguousBelow {
		return AdmissionNew, false
	}
	return "", true
}

// AdmissionQuestion builds the ambiguous-band question: the decider
// (or a cheap LLM call without one) classifies into duplicate,
// refinement, contradiction or new.
func AdmissionQuestion(candidate, existing string) Question {
	return Question{
		Kind:    KindChoice,
		Prompt:  "classify the candidate memory against the existing one",
		Options: []string{"duplicate", "refinement", "contradiction", "new"},
		Context: "candidate: " + candidate + "\nexisting: " + existing,
	}
}

// StuckQuestion builds the Section 18.3 stuck judge question. The
// judge runs from step 30 every 10 steps and only counts at or above
// StuckJudgeThreshold (0.9); it only ever fires the stage-1 note and
// never stops the run alone.
func StuckQuestion(step int, state string) (Question, bool) {
	if step < 30 || step%10 != 0 {
		return Question{}, false
	}
	return Question{
		Kind:    KindNoul,
		Prompt:  "the agent is stuck",
		Context: state,
	}, true
}

// CountsStuck reports whether a stuck probability counts.
func CountsStuck(p float64) bool { return p >= StuckJudgeThreshold }

// NotifyQuestion builds the Section 5.4 question: does this event
// deserve an immediate notice.
func NotifyQuestion(event string) Question {
	return Question{
		Kind:    KindNoul,
		Prompt:  "the event deserves an immediate notice",
		Context: event,
	}
}

// SkillScoreQuestion builds the Section 12.3/12.4 pertinence
// question for one candidate skill or group.
func SkillScoreQuestion(skill, task string) Question {
	return Question{
		Kind:    KindScore,
		Prompt:  "pertinence of the skill to the task",
		Options: []string{"irrelevant", "related", "pertinent", "best"},
		Context: "skill: " + skill + "\ntask: " + task,
	}
}

// ToolRerankQuestion builds the Section 13.3 question used to
// reorder search_tools candidates.
func ToolRerankQuestion(tool, query string) Question {
	return Question{
		Kind:    KindScore,
		Prompt:  "pertinence of the tool to the query",
		Options: []string{"irrelevant", "related", "pertinent", "best"},
		Context: "tool: " + tool + "\nquery: " + query,
	}
}
