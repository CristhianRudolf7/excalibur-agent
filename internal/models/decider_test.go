// Decider tests (Section 19.4 and row 4 of 19.7): calibrated
// bounded questions, abstention in the middle band, the LLM
// fallback, and the never-final-verdict rule.
package models

import (
	"context"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestQuestionValidation(t *testing.T) {
	if err := (Question{Kind: KindNoul, Prompt: "x is true"}).Validate(); err != nil {
		t.Fatalf("valid noul: %v", err)
	}
	if err := (Question{Kind: KindChoice, Prompt: "pick", Options: []string{"a"}}).Validate(); err == nil {
		t.Fatal("choice with one option should fail")
	}
	if err := (Question{Kind: KindNoul, Prompt: "x", Options: []string{"a"}}).Validate(); err == nil {
		t.Fatal("noul with options should fail")
	}
	if err := (Question{Kind: "other", Prompt: "x"}).Validate(); err == nil {
		t.Fatal("unknown kind should fail")
	}
}

func TestDeciderNeverFinalVerdict(t *testing.T) {
	fake := NewFakeDecider(0.99)
	q := Question{Kind: KindNoul, Prompt: "the work is correct", FinalVerdict: true}
	if _, err := fake.Ask(context.Background(), q); err == nil {
		t.Fatal("fake decider must reject final-verdict questions")
	}
	llm := NewLLMDecider(stubCaller{content: `{"probability": 0.99}`}, "")
	if _, err := llm.Ask(context.Background(), q); err == nil {
		t.Fatal("llm fallback must reject final-verdict questions")
	}
	srv := newWireServer(t, wireAnswer{ID: "q0", Probability: 0.99})
	defer srv.Close()
	httpDec := NewJevDecider(srv.URL, "", "", nil)
	defer httpDec.Close()
	if _, err := httpDec.Ask(context.Background(), q); err == nil {
		t.Fatal("http decider must reject final-verdict questions before network use")
	}
}

func TestAbstentionBands(t *testing.T) {
	th := Thresholds{Low: 0.35, High: 0.75}
	if err := th.Validate(); err != nil {
		t.Fatal(err)
	}
	if Decide(0.8, th) != OutcomeAct {
		t.Fatal("p >= high should act")
	}
	if Decide(0.75, th) != OutcomeAct {
		t.Fatal("p == high should act")
	}
	if Decide(0.5, th) != OutcomeDefer {
		t.Fatal("middle band should defer to the LLM in the normal flow")
	}
	if Decide(0.35, th) != OutcomeAlternative {
		t.Fatal("p == low should take the alternative")
	}
	if Decide(0.1, th) != OutcomeAlternative {
		t.Fatal("p <= low should take the alternative")
	}
	if err := (Thresholds{Low: 0.8, High: 0.7}).Validate(); err == nil {
		t.Fatal("low >= high should fail validation")
	}
}

func TestLLMFallbackParsesBoundedJSON(t *testing.T) {
	caller := stubCaller{content: `noise {"choice": "task", "probability": 0.82} tail`}
	d := NewLLMDecider(caller, "")
	ans, err := d.Ask(context.Background(), Question{
		Kind: KindChoice, Prompt: "type?", Options: []string{"task", "conversation"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Choice != "task" || ans.Prob != 0.82 {
		t.Fatalf("answer = %+v", ans)
	}
	if d.Model() != DefaultDecisionsModel {
		t.Fatalf("default decisions model = %q, want gpt-6-luna", d.Model())
	}
}

func TestLLMFallbackClampsAndRejectsGarbage(t *testing.T) {
	d := NewLLMDecider(stubCaller{content: `{"probability": 9}`}, "")
	ans, err := d.Ask(context.Background(), Question{Kind: KindNoul, Prompt: "x?"})
	if err != nil || ans.Prob != 1 {
		t.Fatalf("clamp high: %+v %v", ans, err)
	}
	bad := NewLLMDecider(stubCaller{content: `no json here`}, "custom-model")
	if _, err := bad.Ask(context.Background(), Question{Kind: KindNoul, Prompt: "x?"}); err == nil {
		t.Fatal("garbage output should fail")
	}
	if bad.Model() != "custom-model" {
		t.Fatal("custom decisions model should be kept")
	}
}

func TestLLMFallbackSendsNoReasoningEffortItself(t *testing.T) {
	cap := &effortCapture{}
	d := NewLLMDecider(cap, "")
	_, err := d.Ask(context.Background(), Question{Kind: KindScore, Prompt: "rate", Options: []string{"low", "high"}})
	if err != nil {
		t.Fatal(err)
	}
	if cap.req.ReasoningEffort != "" {
		t.Fatalf("llm fallback must not set reasoning_effort itself: %q", cap.req.ReasoningEffort)
	}
	if cap.role != types.RoleDecider {
		t.Fatalf("fallback uses role %q, want decider", string(cap.role))
	}
	if !strings.Contains(cap.req.Messages[0].Content, "JSON") {
		t.Fatal("fallback prompt must demand restricted JSON")
	}
}

type stubCaller struct {
	content string
	err     error
}

func (s stubCaller) Chat(_ context.Context, _ types.ModelRole, _ providers.ChatRequest) (providers.ChatResponse, CallMeta, error) {
	if s.err != nil {
		return providers.ChatResponse{}, CallMeta{}, s.err
	}
	return providers.ChatResponse{
		Message: providers.Message{Role: providers.RoleAssistant, Content: s.content},
	}, CallMeta{}, nil
}

type effortCapture struct {
	req  providers.ChatRequest
	role types.ModelRole
}

func (c *effortCapture) Chat(_ context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, CallMeta, error) {
	c.req = req
	c.role = role
	return providers.ChatResponse{
		Message: providers.Message{Role: providers.RoleAssistant, Content: `{"score": 1, "probability": 0.6}`},
	}, CallMeta{}, nil
}
