package engine

import (
	"context"
	"encoding/json"
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
			if len(m.requests) != 2 || m.requests[0].MaxTokens != 8192 || m.requests[1].MaxTokens != 8192 {
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
