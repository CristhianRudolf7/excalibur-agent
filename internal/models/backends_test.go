// HTTP backend and decisions-model tests (Section 19.4): the Jev
// and pplx shapes answer bounded questions with probabilities, the
// wire never carries reasoning_effort, and embedding operations omit
// it while chat decisions keep the configured effort.
package models

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openaiwire"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// omitWire is a ReasonOmit openaiwire client: it stands in for
// contracts without a reasoning field (DeepSeek, Ollama).
type omitWire struct {
	name  string
	inner *openaiwire.Client
}

func newOmitWireForTest(name, baseURL string) (*omitWire, error) {
	inner, err := openaiwire.New(openaiwire.Options{
		Provider:  name,
		BaseURL:   baseURL,
		APIKey:    "test",
		Reasoning: openaiwire.ReasonOmit,
	})
	if err != nil {
		return nil, err
	}
	return &omitWire{name: name, inner: inner}, nil
}

func (o *omitWire) Name() string { return o.name }
func (o *omitWire) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	return o.inner.Chat(ctx, req)
}
func (o *omitWire) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	return o.inner.Stream(ctx, req, emit)
}
func (o *omitWire) Close() error { return nil }

func newWireServer(t *testing.T, ans wireAnswer) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req wireRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode wire request: %v", err)
		}
		if len(req.Questions) != 1 {
			t.Errorf("questions = %d, want 1", len(req.Questions))
		}
		for k := range map[string]bool{"reasoning_effort": true, "reasoning": true} {
			if strings.Contains(r.URL.RawQuery, k) {
				t.Errorf("wire must not carry %q", k)
			}
		}
		var body map[string]any
		raw, _ := json.Marshal(req)
		if err := json.Unmarshal(raw, &body); err == nil {
			if _, ok := body["reasoning_effort"]; ok {
				t.Errorf("wire body must not carry reasoning_effort")
			}
		}
		ans.ID = req.Questions[0].ID
		_ = json.NewEncoder(w).Encode(wireResponse{Answers: []wireAnswer{ans}})
	}))
}

func TestHTTPBackendsAnswerBoundedQuestions(t *testing.T) {
	for _, kind := range []struct {
		name string
		dec  *HTTPDecider
	}{
		{"jev", NewJevDecider("placeholder", "", "", nil)},
		{"pplx", NewPplxDecider("placeholder", "", "", nil)},
	} {
		_ = kind
	}
	jevSrv := newWireServer(t, wireAnswer{Probability: 0.83})
	defer jevSrv.Close()
	jev := NewJevDecider(jevSrv.URL, "key", "jev-1", nil)
	defer jev.Close()
	ans, err := jev.Ask(context.Background(), Question{Kind: KindNoul, Prompt: "needs clarification"})
	if err != nil || ans.Prob != 0.83 {
		t.Fatalf("jev: %+v %v", ans, err)
	}
	if jev.Kind() != "jev" {
		t.Fatalf("kind = %q", jev.Kind())
	}

	score := 2
	pplxSrv := newWireServer(t, wireAnswer{Score: &score, Probability: 0.71})
	defer pplxSrv.Close()
	pplx := NewPplxDecider(pplxSrv.URL, "", "pplx-4b", nil)
	defer pplx.Close()
	sans, err := pplx.Ask(context.Background(), Question{
		Kind: KindScore, Prompt: "pertinence", Options: []string{"low", "mid", "high"},
	})
	if err != nil || sans.Score != 2 || sans.Prob != 0.71 {
		t.Fatalf("pplx: %+v %v", sans, err)
	}
}

func TestHTTPBackendErrors(t *testing.T) {
	d := NewJevDecider("", "", "", nil)
	if _, err := d.Ask(context.Background(), Question{Kind: KindNoul, Prompt: "x"}); err == nil {
		t.Fatal("missing base URL should fail")
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	badDec := NewJevDecider(bad.URL, "", "", nil)
	defer badDec.Close()
	if _, err := badDec.Ask(context.Background(), Question{Kind: KindNoul, Prompt: "x"}); err == nil {
		t.Fatal("5xx should fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := badDec.Ask(ctx, Question{Kind: KindNoul, Prompt: "x"}); err == nil {
		t.Fatal("cancelled context should fail")
	}
}

func TestDecisionsModelConfigurable(t *testing.T) {
	cfg := DecisionsConfig{}
	if cfg.ModelName() != DefaultDecisionsModel || DefaultDecisionsModel != "gpt-6-luna" {
		t.Fatalf("default decisions model = %q", cfg.ModelName())
	}
	custom := DecisionsConfig{Model: "other-decider", ReasoningEffort: "low"}
	if custom.ModelName() != "other-decider" {
		t.Fatalf("custom model = %q", custom.ModelName())
	}
	chat := custom.ChatRequest("", []providers.Message{{Role: providers.RoleUser, Content: "hi"}})
	if chat.Model != "other-decider" || chat.ReasoningEffort != "low" {
		t.Fatalf("chat request = %+v", chat)
	}
	// Embedding operations never carry reasoning effort.
	emb := custom.EmbeddingRequest("recall this")
	if emb["model"] != DefaultEmbeddingModel {
		t.Fatalf("embedding model = %v", emb["model"])
	}
	if _, ok := emb["reasoning_effort"]; ok {
		t.Fatal("embedding request must not carry reasoning_effort")
	}
	if !OmitReasoningEffort("embeddings") || OmitReasoningEffort("chat") {
		t.Fatal("omit rule: embeddings true, chat false")
	}
}

func TestDeciderRoleOmitsEffortOnContractsWithoutIt(t *testing.T) {
	// A decider role entry pins reasoning_effort=low (cache-stable
	// per role, Section 19.2), but the DeepSeek contract has no
	// such field: the wire must omit it even though the gateway
	// request carries it.
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer srv.Close()
	ds, err := newOmitWireForTest("deepseek", srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	gw := NewGateway(GatewayConfig{
		Roles: map[types.ModelRole]RoleEntry{
			types.RoleDecider: {Primary: ModelRef{Provider: "deepseek", Model: "deepseek-chat", ReasoningEffort: "low"}},
		},
		MaxAttempts: 1,
	})
	gw.Register(ds)
	if _, _, err := gw.Call(context.Background(), types.RoleDecider, providers.ChatRequest{
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["reasoning_effort"]; ok {
		t.Fatalf("deepseek wire must omit reasoning_effort: %v", got)
	}
	// The fallback itself never sets an effort, so either the role
	// table or the adapter drop keeps every non-accepting wire
	// clean.
	cap := &effortCapture{}
	d := NewLLMDecider(cap, "")
	_, err = d.Ask(context.Background(), Question{Kind: KindNoul, Prompt: "x?"})
	if err != nil {
		t.Fatal(err)
	}
	if cap.req.ReasoningEffort != "" {
		t.Fatalf("fallback request carries effort %q", cap.req.ReasoningEffort)
	}
}
