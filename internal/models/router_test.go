// Routing and escalation tests (Section 19.3 and row 3 of 19.7:
// every escalation signal fires a compact expert consult without
// history, and the run drops back to its default model after).
package models

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type expertCapture struct {
	messages []providers.Message
	resp     providers.ChatResponse
}

func (c *expertCapture) Name() string { return "expert-p" }
func (c *expertCapture) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	c.messages = append([]providers.Message(nil), req.Messages...)
	return c.resp, nil
}
func (c *expertCapture) Stream(context.Context, providers.ChatRequest, func(providers.StreamEvent) error) error {
	return nil
}
func (c *expertCapture) Close() error { return nil }

func expertGateway(t *testing.T) (*Gateway, *expertCapture) {
	t.Helper()
	cap := &expertCapture{resp: providers.ChatResponse{
		Message: providers.Message{Role: providers.RoleAssistant, Content: "diagnosis and plan"},
	}}
	g := NewGateway(GatewayConfig{
		Roles: map[types.ModelRole]RoleEntry{
			types.RoleExecutor: {Primary: ModelRef{Provider: "expert-p", Model: "cheap-1"}},
			types.RoleExpert:   {Primary: ModelRef{Provider: "expert-p", Model: "strong-9"}},
		},
		MaxAttempts: 1,
	})
	g.Register(cap)
	return g, cap
}

func TestEveryEscalationSignalConsultsExpert(t *testing.T) {
	policy := EscalationPolicy{}.withDefaults()
	cases := []struct {
		signal EscalationSignal
		fires  func(EscalationPolicy) bool
	}{
		{SignalLoopSecond, func(p EscalationPolicy) bool { return p.ShouldEscalateLoop(2) }},
		{SignalTestRepeatFail, func(p EscalationPolicy) bool { return p.ShouldEscalateTest(2) }},
		{SignalHighComplexity, func(p EscalationPolicy) bool {
			return p.ShouldEscalateComplexity(PlanSize{Requirements: 9})
		}},
		{SignalUserQuality, func(p EscalationPolicy) bool {
			return p.ShouldEscalateComplexity(PlanSize{UserQuality: true})
		}},
		{SignalGateRepeatFail, func(p EscalationPolicy) bool { return p.ShouldEscalateGate(2) }},
	}
	for _, tc := range cases {
		if !tc.fires(policy) {
			t.Fatalf("signal %q did not fire", string(tc.signal))
		}
		gw, cap := expertGateway(t)
		st := NewEscalationState(policy)
		if !st.Escalate(tc.signal) {
			t.Fatalf("signal %q did not escalate", string(tc.signal))
		}
		if !st.Escalated || st.Signal != tc.signal {
			t.Fatalf("signal %q state = %+v", string(tc.signal), st)
		}
		in := ExpertInput{
			Objective: "finish the task", State: "failing step",
			ExactError: "E0432", Tried: []string{"cargo test"},
			Fragments: []string{"src/lib.rs:12"},
		}
		diag, meta, err := st.RequestExpert(context.Background(), gw, in)
		if err != nil || diag == "" {
			t.Fatalf("signal %q expert: %v %q", string(tc.signal), err, diag)
		}
		if meta.Role != types.RoleExpert {
			t.Fatalf("signal %q served by role %q, want expert", string(tc.signal), string(meta.Role))
		}
		if len(cap.messages) != 1 {
			t.Fatalf("signal %q: expert gets one compact prompt, got %d messages", string(tc.signal), len(cap.messages))
		}
		body := cap.messages[0].Content
		for _, want := range []string{"finish the task", "E0432", "cargo test"} {
			if !strings.Contains(body, want) {
				t.Fatalf("signal %q: compact brief misses %q in %q", string(tc.signal), want, body)
			}
		}
		if st.ExpertCalls != 1 {
			t.Fatalf("signal %q: expert calls = %d, want 1", string(tc.signal), st.ExpertCalls)
		}
		// Second signal during the escalated step does not stack.
		if st.Escalate(tc.signal) {
			t.Fatalf("signal %q: double escalate should not stack", string(tc.signal))
		}
		// After the step resolves the run drops back to default.
		st.Resolve()
		if st.Escalated {
			t.Fatalf("signal %q: Resolve should drop back to default", string(tc.signal))
		}
	}
}

func TestFirstLoopDetectionOnlyNotes(t *testing.T) {
	policy := EscalationPolicy{}.withDefaults()
	if policy.ShouldEscalateLoop(1) {
		t.Fatal("first detection must only note, not escalate")
	}
	if !policy.ShouldEscalateLoop(2) {
		t.Fatal("second detection must escalate")
	}
}

func TestLowComplexityDoesNotEscalate(t *testing.T) {
	policy := EscalationPolicy{}.withDefaults()
	if policy.ShouldEscalateComplexity(PlanSize{Requirements: 2, Steps: 3}) {
		t.Fatal("small plan must stay on the default model")
	}
	if policy.ShouldEscalateTest(1) {
		t.Fatal("one failed fix must not escalate")
	}
	if policy.ShouldEscalateGate(1) {
		t.Fatal("one gate failure must not escalate")
	}
}

func TestExpertBriefValidation(t *testing.T) {
	gw, _ := expertGateway(t)
	st := NewEscalationState(EscalationPolicy{})
	if _, _, err := st.RequestExpert(context.Background(), gw, ExpertInput{}); err == nil {
		t.Fatal("empty brief should fail before spending an expert call")
	}
	if st.ExpertCalls != 0 {
		t.Fatal("rejected brief must not count an expert call")
	}
}
