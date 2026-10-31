// Routing and escalation for Section 19.3.
//
// By default every role uses its primary endpoint. A deterministic
// escalation signal moves hard steps to the expert role through one
// isolated compact consult; afterwards the run drops back to its
// default model. The router never alternates models inside one run
// except for these isolated expert queries, so the conversation
// prefix cache survives.
package models

import (
	"context"
	"fmt"
)

// validateExpertInput rejects empty briefs before spending an expert call.
func validateExpertInput(objective, exactError string) error {
	if objective == "" {
		return fmt.Errorf("models: expert brief needs an objective")
	}
	if exactError == "" {
		return fmt.Errorf("models: expert brief needs the exact error")
	}
	return nil
}

// EscalationSignal is one deterministic trigger of Section 19.3.2.
type EscalationSignal string

// Escalation signals.
const (
	// SignalLoopSecond fires on the second loop detection (the first
	// only injects a corrective note, per Section 18.3).
	SignalLoopSecond EscalationSignal = "loop_second"
	// SignalTestRepeatFail fires when the same test still fails
	// after two fix attempts.
	SignalTestRepeatFail EscalationSignal = "test_repeat_fail"
	// SignalHighComplexity fires when the estimated plan
	// complexity is high.
	SignalHighComplexity EscalationSignal = "high_complexity"
	// SignalUserQuality fires when the user explicitly asks for
	// more quality.
	SignalUserQuality EscalationSignal = "user_quality"
	// SignalGateRepeatFail fires when the completion gate fails
	// twice on the same criterion.
	SignalGateRepeatFail EscalationSignal = "gate_repeat_fail"
)

// AllEscalationSignals lists every signal in document order.
func AllEscalationSignals() []EscalationSignal {
	return []EscalationSignal{
		SignalLoopSecond,
		SignalTestRepeatFail,
		SignalHighComplexity,
		SignalUserQuality,
		SignalGateRepeatFail,
	}
}

// EscalationPolicy holds the deterministic escalation thresholds.
// Zero values select the Section 19.3 and Appendix B defaults.
type EscalationPolicy struct {
	// TestFixAttempts fires SignalTestRepeatFail after this many
	// failed fix attempts on the same test.
	TestFixAttempts int
	// GateFailures fires SignalGateRepeatFail after this many gate
	// failures on the same criterion.
	GateFailures int
	// LoopDetections fires SignalLoopSecond on this detection
	// count (first detection only notes).
	LoopDetections int
	// ComplexRequirements fires SignalHighComplexity at this many
	// requirements; ComplexSteps at this many plan steps.
	ComplexRequirements int
	ComplexSteps        int
}

func (p EscalationPolicy) withDefaults() EscalationPolicy {
	if p.TestFixAttempts < 1 {
		p.TestFixAttempts = 2
	}
	if p.GateFailures < 1 {
		p.GateFailures = 2
	}
	if p.LoopDetections < 1 {
		p.LoopDetections = 2
	}
	if p.ComplexRequirements < 1 {
		p.ComplexRequirements = 8
	}
	if p.ComplexSteps < 1 {
		p.ComplexSteps = 20
	}
	return p
}

// LoopDetections triggers escalation on the second detection.
func (p EscalationPolicy) loopDetections() int { return p.withDefaults().LoopDetections }

// ShouldEscalateLoop reports whether a loop detection count fires
// SignalLoopSecond: only the second detection escalates.
func (p EscalationPolicy) ShouldEscalateLoop(detections int) bool {
	return detections == p.loopDetections()
}

// ShouldEscalateTest reports whether attempts failed fix attempts on
// the same test fire SignalTestRepeatFail.
func (p EscalationPolicy) ShouldEscalateTest(failedAttempts int) bool {
	return failedAttempts >= p.withDefaults().TestFixAttempts
}

// PlanSize is the deterministic complexity input: requirement and
// step counts plus an explicit user quality request.
type PlanSize struct {
	Requirements int
	Steps        int
	Milestones   int
	UserQuality  bool
}

// ShouldEscalateComplexity reports whether the estimated plan
// complexity is high (Section 19.3.2). The heuristic is
// deterministic: many requirements, many steps, several milestones,
// or an explicit user quality request.
func (p EscalationPolicy) ShouldEscalateComplexity(size PlanSize) bool {
	p = p.withDefaults()
	if size.UserQuality {
		return true
	}
	if size.Requirements >= p.ComplexRequirements {
		return true
	}
	if size.Steps >= p.ComplexSteps {
		return true
	}
	if size.Milestones >= 3 {
		return true
	}
	return false
}

// ShouldEscalateGate reports whether failures gate failures on the
// same criterion fire SignalGateRepeatFail.
func (p EscalationPolicy) ShouldEscalateGate(sameCriterionFailures int) bool {
	return sameCriterionFailures >= p.withDefaults().GateFailures
}

// EscalationState tracks one run's escalation level. After an
// escalated step resolves the problem the run drops back to its
// default model (Section 19.3.4).
type EscalationState struct {
	policy EscalationPolicy
	// Escalated is true while the run works an escalated step.
	Escalated bool
	// Signal is the signal that escalated the current step.
	Signal EscalationSignal
	// ExpertCalls counts compact expert consults for this run.
	ExpertCalls int
}

// NewEscalationState returns a non-escalated state with defaults.
func NewEscalationState(policy EscalationPolicy) *EscalationState {
	return &EscalationState{policy: policy.withDefaults()}
}

// Policy returns the effective policy with defaults applied.
func (s *EscalationState) Policy() EscalationPolicy {
	if s == nil {
		return EscalationPolicy{}.withDefaults()
	}
	return s.policy
}

// Escalate marks the run escalated for signal. It returns false when
// the run is already escalated, so a second signal during an
// escalated step does not stack expert calls.
func (s *EscalationState) Escalate(signal EscalationSignal) bool {
	if s.Escalated {
		return false
	}
	s.Escalated = true
	s.Signal = signal
	return true
}

// Resolve drops the run back to its default model after an escalated
// step resolved the problem (Section 19.3.4).
func (s *EscalationState) Resolve() {
	s.Escalated = false
	s.Signal = ""
}

// ExpertInput is the compact brief of Section 19.3.3: objective,
// state, the exact error, what was tried and the relevant fragments.
// It never carries the conversation history, so the expert call
// stays cheap and never disturbs the run's prefix cache.
type ExpertInput struct {
	Objective  string
	State      string
	ExactError string
	Tried      []string
	Fragments  []string
}

// Validate rejects empty briefs before spending an expert call.
func (in ExpertInput) Validate() error {
	return validateExpertInput(in.Objective, in.ExactError)
}

// RequestExpert sends one compact expert consult through the gateway
// and records it on the state. Callers escalate first (Escalate),
// consult, then Resolve when the step succeeds. The prompt holds
// only the brief: objective, state, exact error, tried list and
// fragments, with no history.
func (s *EscalationState) RequestExpert(ctx context.Context, gw *Gateway, in ExpertInput) (string, CallMeta, error) {
	if err := in.Validate(); err != nil {
		return "", CallMeta{}, err
	}
	diag, meta, err := gw.ConsultExpert(ctx, in.Objective, in.State, in.ExactError, in.Tried, in.Fragments)
	if err != nil {
		return "", meta, err
	}
	s.ExpertCalls++
	return diag, meta, nil
}
