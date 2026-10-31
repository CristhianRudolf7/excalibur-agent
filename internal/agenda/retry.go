package agenda

import (
	"fmt"
	"strings"
	"time"
)

// FailureClass classifies a node or step failure (17.7).
type FailureClass string

// Failure classes (17.7).
const (
	FailureTransient FailureClass = "transient"
	FailureInput     FailureClass = "input"
	FailureLogic     FailureClass = "logic"
	FailurePermanent FailureClass = "permanent"
)

// TransientBackoff is the 17.7 retry schedule: 30 s, 2 min, 10 min,
// 1 h, up to 4 attempts (Appendix B).
var TransientBackoff = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	time.Hour,
}

// MaxTransientAttempts caps transient retries (17.7).
const MaxTransientAttempts = 4

// ClassifyFailure maps an error to its 17.7 class by message markers.
// Providers and capabilities refine this with their own typed errors;
// the markers below are the portable fallback.
func ClassifyFailure(err error) FailureClass {
	if err == nil {
		return FailureInput
	}
	msg := strings.ToLower(err.Error())
	for _, m := range []string{"429", "rate limit", "quota", "overloaded", "overload", "timeout", "timed out", "connection reset", "connection refused", "temporary", "try again", "503", "502", "504", "network", "retry-after"} {
		if strings.Contains(msg, m) {
			return FailureTransient
		}
	}
	for _, m := range []string{"unauthorized", "forbidden", "revoked", "credential", "access denied", "permission denied", "payment required"} {
		if strings.Contains(msg, m) {
			return FailurePermanent
		}
	}
	for _, m := range []string{"invalid argument", "not found", "no such", "bad request", "validation", "schema", "missing required"} {
		if strings.Contains(msg, m) {
			return FailureInput
		}
	}
	// Test failures, unmet criteria and wrong outputs need correction,
	// not blind retry (17.7: logic).
	return FailureLogic
}

// RetryDecision is what the policy says about one failure.
type RetryDecision struct {
	// Retry is true when the node may run again.
	Retry bool
	// Wait is how long to wait first (transient backoff or Retry-After).
	Wait time.Duration
	// Reason is the human-readable justification.
	Reason string
}

// RetryAfter, when positive, overrides the backoff (17.7: Retry-After
// is respected).
func DecideRetry(class FailureClass, attempt int, retryAfter time.Duration) RetryDecision {
	switch class {
	case FailureTransient:
		if attempt > MaxTransientAttempts {
			return RetryDecision{Reason: fmt.Sprintf("transient failure persisted after %d attempts: pause for the user", MaxTransientAttempts)}
		}
		wait := retryAfter
		if wait <= 0 {
			idx := attempt - 1
			if idx < 0 {
				idx = 0
			}
			if idx >= len(TransientBackoff) {
				idx = len(TransientBackoff) - 1
			}
			wait = TransientBackoff[idx]
		}
		return RetryDecision{Retry: true, Wait: wait, Reason: fmt.Sprintf("transient failure, attempt %d/%d in %s", attempt, MaxTransientAttempts, wait)}
	case FailureInput:
		return RetryDecision{Reason: "input failure: no blind retry, return to the agent as a result"}
	case FailureLogic:
		return RetryDecision{Reason: "logic failure: correct, do not blind-retry (replan after 3 attempts)"}
	case FailurePermanent:
		return RetryDecision{Reason: "permanent failure: pause and notify the user"}
	}
	return RetryDecision{Reason: "unknown failure class: pause and notify the user"}
}

// NodeRetryState tracks per-node attempts within one workflow or
// scheduled instance (21.5). Retries reuse the same idempotency key
// so repeating never duplicates effects (17.7).
type NodeRetryState struct {
	NodeID         string
	InstanceID     string
	Attempts       int
	IdempotencyKey string
	LastClass      FailureClass
	Compensations  []string
}

// IdempotencyKeyFor derives the stable key per node and instance (21.5).
func IdempotencyKeyFor(instanceID, nodeID string) string {
	return "wf_" + instanceID + "_" + nodeID
}

// Compensation is one declared undo step for saga abort (21.5): they
// run in reverse order and must themselves be idempotent.
type Compensation struct {
	NodeID string `json:"node_id"`
	// Undo describes the compensating action.
	Undo string `json:"undo"`
	// UndoKey is the idempotency key of the compensation.
	UndoKey string `json:"undo_key"`
}

// CompensationOrder returns the compensations of the completed nodes
// in reverse completion order (17.7/21.5 saga abort).
func CompensationOrder(completed []string, byNode map[string]Compensation) []Compensation {
	var out []Compensation
	for i := len(completed) - 1; i >= 0; i-- {
		if c, ok := byNode[completed[i]]; ok {
			out = append(out, c)
		}
	}
	return out
}
