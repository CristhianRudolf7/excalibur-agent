package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type verdictEndpoint struct {
	responses []providers.ChatResponse
	errors    []error
	requests  []providers.ChatRequest
	sites     []string
}

func (m *verdictEndpoint) Chat(ctx context.Context, _ types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	i := len(m.requests)
	m.requests = append(m.requests, req)
	m.sites = append(m.sites, ModelCallSite(ctx))
	var err error
	if i < len(m.errors) {
		err = m.errors[i]
	}
	return m.responses[i], models.CallMeta{}, err
}

func TestRecordedTruncationsCannotPass(t *testing.T) {
	raw, err := os.ReadFile("testdata/verifier_truncations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Role   string              `json:"role"`
		Input  int64               `json:"input_tokens"`
		Output int64               `json:"output_tokens"`
		Kind   providers.ErrorKind `json:"provider_error_class"`
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		if fixture.Role != "verifier" || fixture.Kind != providers.KindTruncated || fixture.Output != 0 {
			t.Fatal(fixture)
		}
		for _, recover := range []bool{false, true} {
			second := finalResp("")
			if recover {
				second = finalResp("D1: PASS - asserted output\nVERDICT: PASS")
			}
			m := &verdictEndpoint{responses: []providers.ChatResponse{{Usage: providers.Usage{Input: fixture.Input, Output: fixture.Output}}, second}, errors: []error{&providers.ProviderError{Kind: fixture.Kind}, nil}}
			e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
			e.deps.Models = m
			run := testRun(Profile{})
			run.Request = "Write /tmp/example.py to print 17."
			cfg := tasks.DefaultSelfVerifyConfig()
			cfg.EnableGeneralizeChecks = false
			ok, reasons := e.runStrongVerifier(context.Background(), run, cfg, tasks.ExtractDraftCriteria(run.Request, 6), nil, strings.Repeat("final prose", 200))
			if ok != recover || (!ok && len(reasons) == 0) {
				t.Fatalf("ok=%v reasons=%v", ok, reasons)
			}
			// The first attempt keeps the verdict budget; the retry
			// doubles it (EXCALIBUR_VERDICT_RETRY).
			if len(m.requests) != 2 || m.requests[0].MaxTokens != 8192 || m.requests[1].MaxTokens != 16384 {
				t.Fatal(m.requests)
			}
			if len(m.requests[1].Messages[0].Content) >= len(m.requests[0].Messages[0].Content) {
				t.Fatal("retry prompt did not shrink")
			}
			if m.sites[0] != "verifier.strong" || m.sites[1] != "verifier.strong.retry" {
				t.Fatal(m.sites)
			}
		}
	}
}

func TestNoVerdictNeverPasses(t *testing.T) {
	for _, reply := range []string{"", "looks good", "D1: PASS - checked", "CHECKS:\n- python3 example.py"} {
		m := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(reply), finalResp(reply)}}
		e, _ := testEngine(m, scriptedRunner(), passingGate())
		r := testRun(Profile{})
		r.Request = "Create /tmp/example.py."
		cfg := tasks.DefaultSelfVerifyConfig()
		cfg.EnableGeneralizeChecks = false
		ok, reasons := e.runStrongVerifier(context.Background(), r, cfg, tasks.ExtractDraftCriteria(r.Request, 6), nil, "")
		if ok || len(reasons) == 0 || m.calls() != 2 {
			t.Fatalf("reply=%q ok=%v reasons=%v calls=%d", reply, ok, reasons, m.calls())
		}
	}
}

func TestVerifierCancellationDoesNotRetry(t *testing.T) {
	m := &fakeEndpoint{err: context.Canceled}
	e, _ := testEngine(m, scriptedRunner(), passingGate())
	_, _, err := e.verifierChat(context.Background(), testRun(Profile{}), "test", "prompt", "small", 384, func(string) bool { return false })
	if err != context.Canceled || m.calls() != 1 {
		t.Fatalf("err=%v calls=%d", err, m.calls())
	}
}

func TestMissingEmbeddedVerifierRespectsOwnershipAndApplicability(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.Models = nil
	r := testRun(Profile{})
	r.Request = "Create /tmp/example.py."
	cfg := tasks.DefaultSelfVerifyConfig()
	drafts := tasks.ExtractDraftCriteria(r.Request, 6)
	if ok, _ := e.runStrongVerifier(context.Background(), r, cfg, drafts, nil, ""); ok {
		t.Fatal("applicable verification passed without model")
	}
	r.GateInput.VerifierRequired = true
	if ok, _ := e.runStrongVerifier(context.Background(), r, cfg, drafts, nil, ""); !ok {
		t.Fatal("embedded hook overrode daemon-owned verification")
	}
	r.GateInput.VerifierRequired = false
	r.Request = "Say hello"
	if ok, _ := e.runStrongVerifier(context.Background(), r, cfg, nil, nil, ""); !ok {
		t.Fatal("non-applicable embedded verifier blocked")
	}
}

// An empty verdict (zero output tokens, no error) gets one retry at a
// larger budget, and a verdict on the retry is accepted.
func TestEmptyVerdictRetriesWithLargerBudget(t *testing.T) {
	t.Setenv("EXCALIBUR_VERDICT_RETRY", "")
	m := &verdictEndpoint{responses: []providers.ChatResponse{
		{Usage: providers.Usage{Output: 0}},
		finalResp("D1: PASS - asserted output\nVERDICT: PASS"),
	}}
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.Models = m
	valid := func(reply string) bool { return tasks.ParseStrongVerdict(reply, nil).Explicit }
	resp, _, err := e.verifierChat(context.Background(), testRun(Profile{}), "verifier.strong", "full prompt", "small prompt", 512, valid)
	if err != nil || !valid(resp.Message.Content) {
		t.Fatalf("retry verdict must be accepted: err=%v content=%q", err, resp.Message.Content)
	}
	if len(m.requests) != 2 || m.requests[0].MaxTokens != 8192 || m.requests[1].MaxTokens != 16384 {
		t.Fatalf("retry must double the budget, got %+v", m.requests)
	}
	if m.requests[1].Messages[0].Content != "small prompt" {
		t.Fatal("retry keeps the smaller prompt")
	}
}

// A verdict still missing after the retry is the sentinel "no
// verdict": it is never a pass, and it is not a provider error.
func TestEmptyVerdictTwiceIsNoVerdict(t *testing.T) {
	t.Setenv("EXCALIBUR_VERDICT_RETRY", "")
	m := &verdictEndpoint{responses: []providers.ChatResponse{{}, {Usage: providers.Usage{Output: 0}}}}
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.Models = m
	valid := func(reply string) bool { return tasks.ParseStrongVerdict(reply, nil).Explicit }
	_, _, err := e.verifierChat(context.Background(), testRun(Profile{}), "verifier.strong", "full", "small", 512, valid)
	if !errors.Is(err, tasks.ErrVerifierNoVerdict) {
		t.Fatalf("err = %v, want ErrVerifierNoVerdict", err)
	}
	if len(m.requests) != 2 || m.requests[1].MaxTokens != 16384 {
		t.Fatalf("exactly one retry at the larger budget expected, got %+v", m.requests)
	}
}

// With EXCALIBUR_VERDICT_RETRY=off the retry keeps the original
// budget (the behaviour before the switch existed).
func TestVerdictRetryOffKeepsOriginalBudget(t *testing.T) {
	t.Setenv("EXCALIBUR_VERDICT_RETRY", "off")
	m := &verdictEndpoint{responses: []providers.ChatResponse{{}, {}}}
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	e.deps.Models = m
	valid := func(reply string) bool { return tasks.ParseStrongVerdict(reply, nil).Explicit }
	if _, _, err := e.verifierChat(context.Background(), testRun(Profile{}), "verifier.strong", "full", "small", 512, valid); !errors.Is(err, tasks.ErrVerifierNoVerdict) {
		t.Fatalf("err = %v", err)
	}
	if len(m.requests) != 2 || m.requests[0].MaxTokens != 8192 || m.requests[1].MaxTokens != 8192 {
		t.Fatalf("switch off must keep 8192 on the retry, got %+v", m.requests)
	}
}
