package engine

import (
	"fmt"
	"strings"
	"time"
)

func FinishAuditEnabled() bool { return switchOn("EXCALIBUR_FINISH_AUDIT") }

// FinishAuditVerdict is an observation of a finish decision, never a gate input.
// Sequence orders durable rows; AttemptID joins the originating tool call when
// present and gives automatic decisions a distinct identity otherwise.
type FinishAuditVerdict struct {
	At        time.Time
	Sequence  int
	AttemptID string
	Mechanism string
	Outcome   string
	Note      string
}

// SetFinishAudit installs the per-run synchronous durable sink before Start.
func (r *Run) SetFinishAudit(sink func(FinishAuditVerdict) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishAuditSink = sink
}

func (r *Run) auditFinish(at time.Time, callID, mechanism, outcome, note string) {
	if !FinishAuditEnabled() {
		return
	}
	r.mu.Lock()
	sink := r.finishAuditSink
	if sink == nil {
		r.mu.Unlock()
		return
	}
	r.finishAuditSequence++
	seq := r.finishAuditSequence
	r.mu.Unlock()
	if callID == "" {
		callID = fmt.Sprintf("automatic-%d", seq)
	}
	// Audit failures do not alter finish policy, just as other export failures
	// do not become task correctness failures. The sink syncs each accepted row.
	_ = sink(FinishAuditVerdict{At: at.UTC(), Sequence: seq, AttemptID: callID, Mechanism: mechanism, Outcome: outcome, Note: note})
}

func verdictOutcome(text string, isError bool) string {
	if isError {
		return "refusal"
	}
	if strings.HasPrefix(text, FinishAcceptedText) {
		return "acceptance"
	}
	return "confirmation_request"
}
