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
	p := &observedProvider{primary: primary}
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
	status int
}
type retryTransport struct{ base http.RoundTripper }

func (t retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil && (resp.StatusCode == 429 || resp.StatusCode >= 500) {
		if state, ok := req.Context().Value(delayKey{}).(*responseDelay); ok {
			state.mu.Lock()
			state.delay = max(state.delay, serverDelay(resp.Header, time.Now()))
			state.status = resp.StatusCode
			state.mu.Unlock()
		}
	}
	return resp, err
}

type observedProvider struct{ primary, fallback providers.Provider }

func (p *observedProvider) Name() string { return p.primary.Name() }
func (p *observedProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	state := &responseDelay{}
	chosen := p.primary
	if attempt, _ := ctx.Value(retryAttemptKey{}).(int); attempt > 0 && attempt%2 == 0 && p.fallback != nil {
		chosen = p.fallback
	}
	resp, err := chosen.Chat(context.WithValue(ctx, delayKey{}, state), req)
	var pe *providers.ProviderError
	if errors.As(err, &pe) && pe.Retryable() {
		copy := *pe
		state.mu.Lock()
		copy.RetryAfter = max(copy.RetryAfter, state.delay)
		if state.status >= 500 && state.status <= 599 {
			copy.Err = fmt.Errorf("http %d: %w", state.status, copy.Err)
		}
		state.mu.Unlock()
		return resp, &copy
	}
	return resp, err
}
func (p *observedProvider) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	emitted := false
	err := p.primary.Stream(ctx, req, func(ev providers.StreamEvent) error { emitted = true; return emit(ev) })
	var pe *providers.ProviderError
	if !emitted && ctx.Err() == nil && p.fallback != nil && errors.As(err, &pe) && pe.Retryable() {
		return p.fallback.Stream(ctx, req, emit)
	}
	return err
}
func (p *observedProvider) Close() error {
	err := p.primary.Close()
	if p.fallback != nil {
		err = errors.Join(err, p.fallback.Close())
	}
	return err
}
