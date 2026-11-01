package models

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/anthropic"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/deepseek"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/gemini"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/generic"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/ollama"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openrouter"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
)

// quotaProbeTimeout bounds the minimal request that confirms a quota or
// billing response (EXCALIBUR_QUOTA_PROBE).
const quotaProbeTimeout = 30 * time.Second

// NewProvider uses the existing adapters with a gateway-owned HTTP observer.
// Credentials resolve through the registry; neither headers nor keys are logged.
func NewProvider(ctx context.Context, def registry.ProviderDef, resolve registry.SecretResolver) (providers.Provider, error) {
	def.Kind = strings.ToLower(strings.TrimSpace(def.Kind))
	switch def.Kind {
	case registry.KindOpenAI, registry.KindAnthropic, registry.KindGemini, registry.KindDeepSeek, registry.KindOpenRouter, registry.KindOllama, registry.KindGeneric:
	default:
		return registry.New(ctx, def, resolve)
	}
	key, _, err := registry.ResolveKey(ctx, def, resolve)
	if err != nil {
		return nil, err
	}
	fallback, _, err := registry.ResolveFallbackKey(ctx, def, resolve)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: retryTransport{base: http.DefaultTransport}}
	build := func(key string) (providers.Provider, error) {
		switch def.Kind {
		case registry.KindOpenAI:
			return openai.New(openai.Options{APIKey: key, BaseURL: def.BaseURL, UseResponses: true, HTTPClient: client})
		case registry.KindAnthropic:
			return anthropic.New(anthropic.Options{APIKey: key, BaseURL: def.BaseURL, Model: def.Model, MaxAttempts: 1, HTTPClient: client}), nil
		case registry.KindGemini:
			return gemini.New(gemini.Options{APIKey: key, BaseURL: def.BaseURL, Model: def.Model, MaxAttempts: 1, HTTPClient: client}), nil
		case registry.KindDeepSeek:
			return deepseek.New(deepseek.Options{APIKey: key, BaseURL: def.BaseURL, HTTPClient: client})
		case registry.KindOpenRouter:
			return openrouter.New(openrouter.Options{APIKey: key, BaseURL: def.BaseURL, HTTPClient: client})
		case registry.KindOllama:
			return ollama.New(ollama.Options{BaseURL: def.BaseURL, HTTPClient: client})
		case registry.KindGeneric:
			return generic.New(generic.Options{APIKey: key, BaseURL: def.BaseURL, HTTPClient: client})
		default:
			return registry.New(ctx, def, resolve)
		}
	}
	primary, err := build(key)
	if err != nil {
		return nil, err
	}
	p := &observedProvider{kind: def.Kind, primary: primary, primaryID: credentialID(def.BaseURL, key), fallbackID: credentialID(def.BaseURL, fallback), timeout: ModelRequestTimeout()}
	if fallback != "" && fallback != key {
		p.fallback, err = build(fallback)
		if err != nil {
			_ = primary.Close()
			return nil, err
		}
	}
	return p, nil
}

type delayKey struct{}
type responseDelay struct {
	mu            sync.Mutex
	delay         time.Duration
	quota         bool
	status        int
	provider      string
	quotaEvidence quotaEvidence
}

// quotaVerdict reports whether the response carried quota or billing evidence.
func (s *responseDelay) quotaVerdict() (bool, quotaEvidence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quota, s.quotaEvidence
}

type retryTransport struct{ base http.RoundTripper }

func (t retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil && resp.StatusCode >= 400 {
		observeQuota(req.Context(), resp)
		if state, ok := req.Context().Value(delayKey{}).(*responseDelay); ok {
			state.mu.Lock()
			state.delay = max(state.delay, serverDelay(resp.Header, time.Now()))
			state.status = resp.StatusCode
			state.mu.Unlock()
		}
	}
	return resp, err
}

// observedProvider serves one provider with an optional fallback credential.
// It bounds each request by the model request timeout, confirms quota
// evidence before quarantining a credential, and answers a response cut at
// the output limit with one follow-up call (EXCALIBUR_PROVIDER_RETRY_V2).
type observedProvider struct {
	primary, fallback     providers.Provider
	kind                  string
	primaryID, fallbackID [32]byte
	timeout               time.Duration
}

func (p *observedProvider) Name() string { return p.primary.Name() }

// callOnce performs one provider request under the per-request deadline.
// A deadline that fires while the caller's context is still live becomes a
// transient provider error, so the gateway retries it like a connection reset.
func (p *observedProvider) callOnce(ctx context.Context, chosen providers.Provider, req providers.ChatRequest) (providers.ChatResponse, *responseDelay, error) {
	callCtx := ctx
	if p.timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}
	state := &responseDelay{provider: p.kind}
	resp, err := chosen.Chat(context.WithValue(callCtx, delayKey{}, state), req)
	return resp, state, p.timeoutError(ctx, callCtx, chosen, req, err)
}

// timeoutError converts the per-request deadline into a retryable error.
func (p *observedProvider) timeoutError(ctx, callCtx context.Context, chosen providers.Provider, req providers.ChatRequest, err error) error {
	if err == nil || p.timeout <= 0 || ctx.Err() != nil || callCtx.Err() == nil {
		return err
	}
	return &providers.ProviderError{
		Kind: providers.KindTransient, Provider: chosen.Name(), Model: req.Model,
		Err: fmt.Errorf("model request exceeded %s", p.timeout),
	}
}

// confirmQuota decides whether a quota or billing response quarantines its
// credential. With EXCALIBUR_QUOTA_PROBE off the response decides alone.
// With the probe on, a minimal request on the same credential must repeat
// the billing evidence. Rate limits and transient failures never quarantine.
func (p *observedProvider) confirmQuota(ctx context.Context, chosen providers.Provider, model string) bool {
	if !QuotaProbeEnabled() {
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, quotaProbeTimeout)
	defer cancel()
	probe := providers.ChatRequest{
		Model: model, MaxTokens: 1,
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "ping"}},
	}
	_, state, err := p.callOnce(probeCtx, chosen, probe)
	if err == nil {
		return false
	}
	quota, _ := state.quotaVerdict()
	return quota
}

// recordSwitch counts one move to the fallback credential in the call stats,
// which the gateway reports in CallMeta.CredentialSwitches.
func (p *observedProvider) recordSwitch(ctx context.Context) {
	if stats, ok := ctx.Value(retryStatsKey{}).(*retryWaitStats); ok {
		stats.Switches++
	}
	slog.Info("provider credential switched to fallback after confirmed exhaustion", "provider", p.Name())
}

// truncatedError reports whether err is an output-limit cut.
func truncatedError(err error) bool {
	var pe *providers.ProviderError
	return errors.As(err, &pe) && pe.Kind == providers.KindTruncated
}

// continueTruncated answers a response cut at the output limit with one
// follow-up call (EXCALIBUR_PROVIDER_RETRY_V2). A cut tool call is sent again
// as the same request. Cut text is continued from the partial answer with an
// instruction to finish concisely. It reports false when the follow-up also
// fails, which leaves the gateway's existing token doubling in place.
func (p *observedProvider) continueTruncated(ctx context.Context, chosen providers.Provider, req providers.ChatRequest, partial providers.ChatResponse) (providers.ChatResponse, bool) {
	if !ProviderRetryV2Enabled() {
		return providers.ChatResponse{}, false
	}
	var resp providers.ChatResponse
	var err error
	if len(partial.Message.ToolCalls) > 0 {
		resp, _, err = p.callOnce(ctx, chosen, req)
	} else {
		follow := req
		follow.Messages = append([]providers.Message(nil), req.Messages...)
		const note = "The previous reply was cut off at the output limit. Continue concisely and finish within the limit."
		if partial.Message.Content != "" {
			follow.Messages = append(follow.Messages,
				providers.Message{Role: providers.RoleAssistant, Content: partial.Message.Content},
				providers.Message{Role: providers.RoleUser, Content: note})
		} else {
			follow.Messages = append(follow.Messages,
				providers.Message{Role: providers.RoleUser, Content: "Answer again concisely within the output limit."})
		}
		resp, _, err = p.callOnce(ctx, chosen, follow)
		if err == nil {
			resp.Message.Content = partial.Message.Content + resp.Message.Content
		}
	}
	if err != nil {
		return providers.ChatResponse{}, false
	}
	usage := partial.Usage
	usage.Add(resp.Usage)
	resp.Usage = usage
	return resp, true
}

func (p *observedProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return providers.ChatResponse{}, err
	}
	attempt, _ := ctx.Value(retryAttemptKey{}).(int)
	start := 0
	if attempt > 0 && attempt%2 == 0 && p.fallback != nil {
		start = 1
	}
	for n := 0; n < 2; n++ {
		slot := (start + n) % 2
		chosen, id := p.primary, p.primaryID
		if slot == 1 {
			chosen, id = p.fallback, p.fallbackID
		}
		if chosen == nil || credentialExhausted(id) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return providers.ChatResponse{}, err
		}
		resp, state, err := p.callOnce(ctx, chosen, req)
		if quota, ev := state.quotaVerdict(); quota && p.confirmQuota(ctx, chosen, req.Model) {
			exhaustCredential(id, ev)
			if slot == 0 && p.fallback != nil {
				p.recordSwitch(ctx)
			}
			continue
		}
		if err != nil && truncatedError(err) {
			if cont, ok := p.continueTruncated(ctx, chosen, req, resp); ok {
				return cont, nil
			}
		}
		if err == nil {
			return resp, nil
		}
		_, delay, status := state.quotaVerdictDelay()
		var pe *providers.ProviderError
		if errors.As(err, &pe) && pe.Retryable() {
			copy := *pe
			copy.RetryAfter = max(copy.RetryAfter, delay)
			if status >= 500 && status <= 599 {
				copy.Err = fmt.Errorf("http %d: %w", status, copy.Err)
			}
			return resp, &copy
		}
		return resp, err
	}
	return providers.ChatResponse{}, quotaExhaustedError(p.Name())
}

// quotaVerdictDelay returns the server delay and status observed for a try.
func (s *responseDelay) quotaVerdictDelay() (bool, time.Duration, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quota, s.delay, s.status
}

func (p *observedProvider) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	var last error
	for slot := 0; slot < 2; slot++ {
		chosen, id := p.primary, p.primaryID
		if slot == 1 {
			chosen, id = p.fallback, p.fallbackID
		}
		if chosen == nil || credentialExhausted(id) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		callCtx := ctx
		if p.timeout > 0 {
			var cancel context.CancelFunc
			callCtx, cancel = context.WithTimeout(ctx, p.timeout)
			defer cancel()
		}
		state := &responseDelay{provider: p.kind}
		emitted := false
		err := chosen.Stream(context.WithValue(callCtx, delayKey{}, state), req, func(ev providers.StreamEvent) error { emitted = true; return emit(ev) })
		err = p.timeoutError(ctx, callCtx, chosen, req, err)
		if quota, ev := state.quotaVerdict(); quota && p.confirmQuota(ctx, chosen, req.Model) {
			exhaustCredential(id, ev)
			if emitted {
				return err
			}
			if slot == 0 && p.fallback != nil {
				p.recordSwitch(ctx)
			}
			continue
		}
		var pe *providers.ProviderError
		if emitted || err == nil || !errors.As(err, &pe) || !pe.Retryable() {
			return err
		}
		last = err
	}
	if last != nil {
		return last
	}
	return quotaExhaustedError(p.Name())
}

func (p *observedProvider) Close() error {
	err := p.primary.Close()
	if p.fallback != nil {
		err = errors.Join(err, p.fallback.Close())
	}
	return err
}

// ProviderErrorReason names why one model call failed with a single
// allow-listed token for the run records and the infrastructure
// classification. It never carries provider message text.
func ProviderErrorReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrProviderQuotaExhausted) {
		return "quota_exhausted"
	}
	var pe *providers.ProviderError
	if !errors.As(err, &pe) {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return "deadline"
		case errors.Is(err, context.Canceled):
			return "canceled"
		}
		return "unknown"
	}
	text := strings.ToLower(pe.Error())
	switch {
	case strings.Contains(text, "http 402") || strings.Contains(text, "payment required") || permanentBillingText(text):
		return "billing"
	case pe.Kind == providers.KindRateLimited:
		return "rate_limited"
	case overloadStatus.MatchString(text):
		return "http_5xx"
	case strings.Contains(text, "exceeded") || strings.Contains(text, "timed out") || strings.Contains(text, "deadline exceeded"):
		return "timeout"
	case pe.Kind == providers.KindInput && providers.IsCertificateError(err):
		return "tls"
	case pe.Kind == providers.KindAuth:
		return "auth"
	case pe.Kind == providers.KindTransient:
		return "connection"
	}
	return string(pe.Kind)
}

// permanentBillingText matches the billing identifiers of quota responses in
// lower-cased provider error text (the normalized message carries them when
// the provider echoes the error code).
func permanentBillingText(lower string) bool {
	for _, marker := range []string{"insufficient_quota", "billing_hard_limit", "billing_not_active", "billing_error", "credit_balance_exhausted", "credit_balance_too_low"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
