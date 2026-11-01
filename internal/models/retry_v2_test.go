// Provider retry v2, per-request deadline, quota probe and output-limit
// continuation tests (work package F). Network tests use httptest servers;
// no test calls a paid API.
package models

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// isolateCredentials clears operator credential overrides for one test.
func isolateCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("EXCALIBUR_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "")
}

// openAIResponsesOK is a minimal successful Responses API body.
const openAIResponsesOK = `{"id":"r","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`

func TestRetryV2Schedule(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	e := &providers.ProviderError{Kind: providers.KindRateLimited, Err: errors.New("slow")}
	for attempt := 1; attempt <= 9; attempt++ {
		nominal := min(time.Second<<(attempt-1), 60*time.Second)
		for i := 0; i < 200; i++ {
			d := retryDelay(e, time.Millisecond, attempt)
			if d > nominal || d < nominal*8/10 {
				t.Fatalf("attempt %d: delay %v outside [0.8, 1.0] x %v", attempt, d, nominal)
			}
		}
	}
}

func TestRetryV2HonorsRetryAfter(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	e := &providers.ProviderError{Kind: providers.KindRateLimited, RetryAfter: 3 * time.Second, Err: errors.New("slow")}
	for attempt := 1; attempt <= 5; attempt++ {
		if d := retryDelay(e, time.Millisecond, attempt); d < 3*time.Second || d > 3100*time.Millisecond {
			t.Fatalf("attempt %d: server wait not honored: %v", attempt, d)
		}
	}
}

func TestBudgetRetryAllowedClasses(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	cases := []struct {
		name    string
		kind    providers.ErrorKind
		text    string
		attempt int
		want    bool
	}{
		{"rate limit keeps retrying", providers.KindRateLimited, "slow", 100, true},
		{"5xx keeps retrying", providers.KindTransient, "http 503: overloaded", 100, true},
		{"network failure within budget", providers.KindTransient, "connection reset", 5, true},
		{"network failure at budget", providers.KindTransient, "connection reset", transportRetryAttempts, false},
		{"auth never retried", providers.KindAuth, "http 402", 1, false},
		{"input never retried", providers.KindInput, "bad request", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &providers.ProviderError{Kind: tc.kind, Err: errors.New(tc.text)}
			if got := budgetRetryAllowed(err, tc.attempt); got != tc.want {
				t.Fatalf("budgetRetryAllowed = %v, want %v", got, tc.want)
			}
		})
	}
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "0")
	legacy := &providers.ProviderError{Kind: providers.KindTransient, Err: errors.New("connection reset")}
	if budgetRetryAllowed(legacy, 1) {
		t.Fatal("legacy policy must not extend network retries past the attempt budget")
	}
}

// TestTransportRetryBudget checks that v2 makes six tries on a network-class
// failure before the gateway gives up, and reports the count.
func TestTransportRetryBudget(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	p := &scriptedProvider{name: "p", fails: 99, kind: providers.KindTransient, retryAft: time.Millisecond}
	g := NewGateway(testConfig(p, nil))
	g.Register(p)
	_, meta, err := g.Call(context.Background(), types.RoleExecutor, providers.ChatRequest{})
	if err == nil {
		t.Fatal("expected failure after the transport budget")
	}
	if meta.Attempts != transportRetryAttempts || p.calls != transportRetryAttempts {
		t.Fatalf("attempts meta=%d calls=%d, want %d", meta.Attempts, p.calls, transportRetryAttempts)
	}
}

func TestModelRequestTimeoutParsing(t *testing.T) {
	cases := map[string]time.Duration{
		"":      DefaultModelRequestTimeout,
		"45":    45 * time.Second,
		"2.5":   2500 * time.Millisecond,
		"1m":    time.Minute,
		"0":     0,
		"bogus": DefaultModelRequestTimeout,
	}
	for raw, want := range cases {
		t.Setenv("EXCALIBUR_MODEL_REQUEST_TIMEOUT", raw)
		if got := ModelRequestTimeout(); got != want {
			t.Fatalf("%q: got %v want %v", raw, got, want)
		}
	}
}

// TestProviderRequestTimeoutRetried serves the first request after the
// per-request deadline and the second promptly. The timed-out try must be a
// retryable error, so the gateway succeeds on the second try.
func TestProviderRequestTimeoutRetried(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("EXCALIBUR_MODEL_REQUEST_TIMEOUT", "80ms")
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			time.Sleep(400 * time.Millisecond)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, openAIResponsesOK)
	}))
	defer srv.Close()
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: "timeout-key-" + t.Name(), BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	g := NewGateway(GatewayConfig{Roles: map[types.ModelRole]RoleEntry{types.RoleExecutor: {Primary: ModelRef{Provider: "openai", Model: "m"}}}})
	g.Register(p)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, meta, err := g.Call(ctx, types.RoleExecutor, providers.ChatRequest{})
	if err != nil {
		t.Fatalf("call failed: %v meta=%+v", err, meta)
	}
	if resp.Message.Content != "ok" || meta.Attempts != 2 {
		t.Fatalf("resp=%q meta=%+v", resp.Message.Content, meta)
	}
}

// TestConnectionResetRetried closes the first connection without a response.
func TestConnectionResetRetried(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.(*net.TCPConn).SetLinger(0)
				_ = conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, openAIResponsesOK)
	}))
	defer srv.Close()
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: "reset-key-" + t.Name(), BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	g := NewGateway(GatewayConfig{Roles: map[types.ModelRole]RoleEntry{types.RoleExecutor: {Primary: ModelRef{Provider: "openai", Model: "m"}}}})
	g.Register(p)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, meta, err := g.Call(ctx, types.RoleExecutor, providers.ChatRequest{})
	if err != nil || resp.Message.Content != "ok" || meta.Attempts != 2 {
		t.Fatalf("resp=%q meta=%+v err=%v", resp.Message.Content, meta, err)
	}
}

// quotaServer answers the primary credential with status and body and any
// other credential with success. Requests carrying the probe text are counted
// as quota probes.
type quotaServer struct {
	mu      sync.Mutex
	probes  int
	primary string
	status  int
	body    string
}

func (q *quotaServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	q.mu.Lock()
	defer q.mu.Unlock()
	if bytes.Contains(raw, []byte("ping")) {
		q.probes++
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Header.Get("Authorization") == "Bearer "+q.primary {
		w.WriteHeader(q.status)
		fmt.Fprint(w, q.body)
		return
	}
	fmt.Fprint(w, openAIResponsesOK)
}

func TestQuotaProbeConfirmsAndSwitchesToFallback(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("EXCALIBUR_QUOTA_PROBE", "1")
	primary, fallback := "probe-primary-"+t.Name(), "probe-fallback-"+t.Name()
	q := &quotaServer{primary: primary, status: 402, body: `{"error":{"message":"payment required","type":"insufficient_quota","code":"insufficient_quota"}}`}
	srv := httptest.NewServer(q)
	defer srv.Close()
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: primary, FallbackKey: fallback, BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	g := NewGateway(GatewayConfig{Roles: map[types.ModelRole]RoleEntry{types.RoleExecutor: {Primary: ModelRef{Provider: "openai", Model: "m"}}}})
	g.Register(p)
	resp, meta, err := g.Call(context.Background(), types.RoleExecutor, providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}}})
	if err != nil || resp.Message.Content != "ok" {
		t.Fatalf("call: %v %q", err, resp.Message.Content)
	}
	if meta.CredentialSwitches != 1 {
		t.Fatalf("credential switches = %d, want 1 (meta=%+v)", meta.CredentialSwitches, meta)
	}
	if q.probes == 0 {
		t.Fatal("quota response must be confirmed by a probe before quarantine")
	}
	if !credentialExhausted(credentialID(srv.URL, primary)) {
		t.Fatal("confirmed exhaustion must quarantine the primary credential")
	}
}

func TestQuotaProbeOffQuarantinesWithoutProbe(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("EXCALIBUR_QUOTA_PROBE", "0")
	primary, fallback := "direct-primary-"+t.Name(), "direct-fallback-"+t.Name()
	q := &quotaServer{primary: primary, status: 402, body: `{"error":{"message":"payment required"}}`}
	srv := httptest.NewServer(q)
	defer srv.Close()
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: primary, FallbackKey: fallback, BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	g := NewGateway(GatewayConfig{Roles: map[types.ModelRole]RoleEntry{types.RoleExecutor: {Primary: ModelRef{Provider: "openai", Model: "m"}}}})
	g.Register(p)
	_, meta, err := g.Call(context.Background(), types.RoleExecutor, providers.ChatRequest{})
	if err != nil || meta.CredentialSwitches != 1 {
		t.Fatalf("switch=%d err=%v", meta.CredentialSwitches, err)
	}
	if q.probes != 0 {
		t.Fatalf("probe disabled but %d probe requests were sent", q.probes)
	}
}

// TestRateLimitNeverQuarantines: a plain rate limit is retried, never probed
// and never quarantines the credential.
func TestRateLimitNeverQuarantines(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("EXCALIBUR_QUOTA_PROBE", "1")
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	key := "ratelimit-key-" + t.Name()
	q := &quotaServer{primary: key, status: 429, body: `{"error":{"message":"Rate limit reached","type":"tokens","code":"rate_limit_exceeded"}}`}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		q.ServeHTTP(w, r)
	}))
	defer srv.Close()
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: key, BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	g := NewGateway(GatewayConfig{Roles: map[types.ModelRole]RoleEntry{types.RoleExecutor: {Primary: ModelRef{Provider: "openai", Model: "m"}}}})
	g.Register(p)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _, _ = g.Call(ctx, types.RoleExecutor, providers.ChatRequest{})
	if q.probes != 0 {
		t.Fatalf("rate limit sent %d probes", q.probes)
	}
	if credentialExhausted(credentialID(srv.URL, key)) {
		t.Fatal("rate limit quarantined the credential")
	}
}

// TestUnconfirmedQuotaKeepsCredential: the response looks like billing, but
// the live probe succeeds, so the credential stays in service.
func TestUnconfirmedQuotaKeepsCredential(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("EXCALIBUR_QUOTA_PROBE", "1")
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	key := "unconfirmed-key-" + t.Name()
	var mu sync.Mutex
	probes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		isProbe := bytes.Contains(raw, []byte("ping"))
		if isProbe {
			probes++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if isProbe {
			fmt.Fprint(w, openAIResponsesOK)
			return
		}
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"type":"insufficient_quota","code":"insufficient_quota"}}`)
	}))
	defer srv.Close()
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: key, BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	g := NewGateway(GatewayConfig{Roles: map[types.ModelRole]RoleEntry{types.RoleExecutor: {Primary: ModelRef{Provider: "openai", Model: "m"}}}})
	g.Register(p)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _, _ = g.Call(ctx, types.RoleExecutor, providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}}})
	mu.Lock()
	defer mu.Unlock()
	if probes == 0 {
		t.Fatal("insufficient_quota candidate must be probed")
	}
	if credentialExhausted(credentialID(srv.URL, key)) {
		t.Fatal("a probe that succeeds must not quarantine the credential")
	}
}

// truncatingProvider returns a partial answer cut at the output limit first,
// then serves the scripted follow-up, recording every request.
type truncatingProvider struct {
	mu        sync.Mutex
	partial   providers.ChatResponse
	followUp  providers.ChatResponse
	followCut bool
	requests  []providers.ChatRequest
}

func (t *truncatingProvider) Name() string { return "truncating" }
func (t *truncatingProvider) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests = append(t.requests, req)
	trunc := &providers.ProviderError{Kind: providers.KindTruncated, Provider: "truncating", Err: errors.New("output cut by max_tokens limit")}
	if len(t.requests) == 1 {
		return t.partial, trunc
	}
	if t.followCut {
		return t.partial, trunc
	}
	return t.followUp, nil
}
func (t *truncatingProvider) Stream(context.Context, providers.ChatRequest, func(providers.StreamEvent) error) error {
	return nil
}
func (t *truncatingProvider) Close() error { return nil }

func TestContinueTruncatedTextMergesAnswer(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	tp := &truncatingProvider{
		partial:  providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "partial "}, Usage: providers.Usage{Output: 10}},
		followUp: providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "rest"}, Usage: providers.Usage{Output: 3}},
	}
	o := &observedProvider{primary: tp, primaryID: credentialID("cont", "k")}
	req := providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: "write it"}}}
	resp, err := o.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("continuation failed: %v", err)
	}
	if resp.Message.Content != "partial rest" || resp.Usage.Output != 13 {
		t.Fatalf("merged resp=%q usage=%+v", resp.Message.Content, resp.Usage)
	}
	if len(tp.requests) != 2 {
		t.Fatalf("requests=%d, want 2", len(tp.requests))
	}
	follow := tp.requests[1].Messages
	if len(follow) != 3 || follow[1].Role != providers.RoleAssistant || follow[1].Content != "partial " {
		t.Fatalf("follow-up must continue from the partial answer: %+v", follow)
	}
	if len(req.Messages) != 1 {
		t.Fatal("caller request must not be mutated")
	}
}

func TestContinueTruncatedToolCallRepeatsRequest(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	tp := &truncatingProvider{
		partial:  providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "c1", Name: "exec", Arguments: []byte(`{"command":"ls`)}}}},
		followUp: providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "c2", Name: "exec", Arguments: []byte(`{"command":"ls"}`)}}}},
	}
	o := &observedProvider{primary: tp, primaryID: credentialID("cont", "tc")}
	req := providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: "list"}}}
	resp, err := o.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("continuation failed: %v", err)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].ID != "c2" {
		t.Fatalf("resp=%+v", resp.Message)
	}
	if got := tp.requests[1]; len(got.Messages) != 1 {
		t.Fatalf("a cut tool call must repeat the same request, got %d messages", len(got.Messages))
	}
}

func TestContinueTruncatedFailureKeepsTruncatedError(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	tp := &truncatingProvider{
		partial:   providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, Content: "a"}},
		followCut: true,
	}
	o := &observedProvider{primary: tp, primaryID: credentialID("cont", "fail")}
	_, err := o.Chat(context.Background(), providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}}})
	var pe *providers.ProviderError
	if !errors.As(err, &pe) || pe.Kind != providers.KindTruncated {
		t.Fatalf("err=%v, want the truncated error so the gateway doubles tokens", err)
	}
}

func TestContinueTruncatedSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "0")
	tp := &truncatingProvider{partial: providers.ChatResponse{Message: providers.Message{Content: "a"}}, followUp: providers.ChatResponse{Message: providers.Message{Content: "b"}}}
	o := &observedProvider{primary: tp, primaryID: credentialID("cont", "off")}
	_, err := o.Chat(context.Background(), providers.ChatRequest{})
	if err == nil || len(tp.requests) != 1 {
		t.Fatalf("legacy policy must not issue a follow-up: requests=%d err=%v", len(tp.requests), err)
	}
}

func TestProviderErrorReasonTokens(t *testing.T) {
	timeoutText := &providers.ProviderError{Kind: providers.KindTransient, Err: fmt.Errorf("model request exceeded %s", time.Second)}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"quota exhausted", ErrProviderQuotaExhausted, "quota_exhausted"},
		{"402 billing", &providers.ProviderError{Kind: providers.KindAuth, Err: errors.New("http 402: payment required")}, "billing"},
		{"billing identifier in text", &providers.ProviderError{Kind: providers.KindAuth, Err: errors.New("insufficient_quota")}, "billing"},
		{"rate limited", &providers.ProviderError{Kind: providers.KindRateLimited, Err: errors.New("http 429: slow down")}, "rate_limited"},
		{"5xx", &providers.ProviderError{Kind: providers.KindTransient, Err: errors.New("http 503: overloaded")}, "http_5xx"},
		{"timeout", timeoutText, "timeout"},
		{"connection", &providers.ProviderError{Kind: providers.KindTransient, Err: errors.New("connection reset by peer")}, "connection"},
		{"auth", &providers.ProviderError{Kind: providers.KindAuth, Err: errors.New("http 401: bad key")}, "auth"},
		{"deadline", context.DeadlineExceeded, "deadline"},
		{"canceled", context.Canceled, "canceled"},
		{"non provider", errors.New("engine failed"), "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProviderErrorReason(tc.err); got != tc.want {
				t.Fatalf("reason=%q want %q", got, tc.want)
			}
		})
	}
}

// Disabling budget retries must not disable the independent request deadline.
func TestRequestTimeoutWithoutBudgetRetries(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("EXCALIBUR_BUDGET_RETRIES", "0")
	t.Setenv("EXCALIBUR_MODEL_REQUEST_TIMEOUT", "20ms")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: "timeout-local", BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = p.Chat(ctx, providers.ChatRequest{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "ping"}}})
	if ctx.Err() != nil {
		t.Fatal("provider waited for the wall deadline instead of the request deadline")
	}
	var pe *providers.ProviderError
	if !errors.As(err, &pe) || pe.Kind != providers.KindTransient {
		t.Fatalf("timeout = %v", err)
	}
}

func TestHTTPMaxOutputTokensContinuesOnce(t *testing.T) {
	isolateCredentials(t)
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "1")
	var mu sync.Mutex
	calls := 0
	concise := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			fmt.Fprint(w, `{"id":"r","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial "}]}]}`)
			return
		}
		concise = bytes.Contains(body, []byte("Continue concisely"))
		fmt.Fprint(w, openAIResponsesOK)
	}))
	defer srv.Close()
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: "local-only", BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	resp, err := p.Chat(context.Background(), providers.ChatRequest{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "Respond briefly"}}})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 || !concise || resp.Message.Content != "partial ok" {
		t.Fatalf("calls=%d concise=%v output=%q", calls, concise, resp.Message.Content)
	}
}
