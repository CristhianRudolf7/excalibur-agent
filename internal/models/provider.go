package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
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

// NewProvider uses the existing adapters with a gateway-owned HTTP observer.
// Credentials resolve through the registry; neither headers nor keys are logged.
func NewProvider(ctx context.Context, def registry.ProviderDef, resolve registry.SecretResolver) (providers.Provider, error) {
	if os.Getenv("EXCALIBUR_BUDGET_RETRIES") == "0" {
		return registry.New(ctx, def, resolve)
	}
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
	p := &observedProvider{primary: primary, primaryID: credentialID(def.BaseURL, key), fallbackID: credentialID(def.BaseURL, fallback)}
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
	mu     sync.Mutex
	delay  time.Duration
	quota  bool
	status int
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

type observedProvider struct {
	primary, fallback     providers.Provider
	primaryID, fallbackID [32]byte
}

func (p *observedProvider) Name() string { return p.primary.Name() }
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
		state := &responseDelay{}
		resp, err := chosen.Chat(context.WithValue(ctx, delayKey{}, state), req)
		state.mu.Lock()
		quota, delay, status := state.quota, state.delay, state.status
		state.mu.Unlock()
		if quota || quotaError(err) {
			exhaustCredential(id)
			continue
		}
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
		state := &responseDelay{}
		emitted := false
		err := chosen.Stream(context.WithValue(ctx, delayKey{}, state), req, func(ev providers.StreamEvent) error { emitted = true; return emit(ev) })
		state.mu.Lock()
		quota := state.quota
		state.mu.Unlock()
		if quota || quotaError(err) {
			exhaustCredential(id)
			if emitted {
				return err
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
