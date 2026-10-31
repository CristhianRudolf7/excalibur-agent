package registry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/anthropic"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/deepseek"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/gemini"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/generic"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/ollama"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openrouter"
)

// Provider kinds built in here. The OpenAI-family adapters of
// milestone H1 register below; third parties add kinds via Register.
const (
	KindAnthropic  = "anthropic"
	KindGemini     = "gemini"
	KindOpenAI     = "openai"
	KindDeepSeek   = "deepseek"
	KindOpenRouter = "openrouter"
	KindOllama     = "ollama"
	KindGeneric    = "generic"
)

// Default environment variables per kind, in lookup order.
var defaultKeyEnv = map[string][]string{
	KindAnthropic:  {"ANTHROPIC_API_KEY"},
	KindGemini:     {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	KindOpenAI:     {"OPENAI_API_KEY"},
	KindDeepSeek:   {"DEEPSEEK_API_KEY"},
	KindOpenRouter: {"OPENROUTER_API_KEY"},
}

// operatorKeyEnv is the deployment-wide credential alias, consulted
// after the kind-specific variables and before the explicit config
// value, so one EXCALIBUR_API_KEY serves any provider kind.
var operatorKeyEnv = []string{"EXCALIBUR_API_KEY"}

// operatorFallbackEnv is the deployment-wide fallback credential
// alias for the per-definition fallback variables below.
var operatorFallbackEnv = []string{"EXCALIBUR_FALLBACK_API_KEY"}

// SecretResolver resolves a vault secret name to its value. It is
// the hook where the H10 secrets vault connects; until then callers
// pass nil or a test stub. A nil resolver is skipped.
type SecretResolver func(ctx context.Context, name string) (string, error)

// ProviderDef describes one provider instance to build.
type ProviderDef struct {
	// Kind selects the adapter: "anthropic", "gemini", or any kind
	// added with Register.
	Kind string `json:"kind"`
	// Model is the default model for calls that set none.
	Model string `json:"model,omitempty"`
	// BaseURL overrides the API endpoint (tests, proxies).
	BaseURL string `json:"base_url,omitempty"`
	// APIKey is the explicit credential: last resort after the
	// vault hook and the environment (Sections 19.2, 20.1).
	APIKey string `json:"api_key,omitempty"`
	// APIKeyEnv names the environment variable holding the key.
	// Empty means the kind's default variable list.
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// APIKeySecret names the vault secret holding the key,
	// resolved via the SecretResolver hook.
	APIKeySecret string `json:"api_key_secret,omitempty"`
	// FallbackKey is the explicit fallback credential, used only on
	// quota/rate-limit errors with bounded retries (Section 19.5).
	FallbackKey string `json:"fallback_api_key,omitempty"`
	// FallbackKeyEnv names the environment variable holding the
	// fallback credential. Empty means operatorFallbackEnv.
	FallbackKeyEnv string `json:"fallback_api_key_env,omitempty"`
	// FallbackKeySecret names the vault secret holding the fallback
	// credential, resolved via the SecretResolver hook.
	FallbackKeySecret string `json:"fallback_api_key_secret,omitempty"`
	// MaxAttempts bounds one call including the first try.
	MaxAttempts int `json:"max_attempts,omitempty"`
}

// Constructor builds a provider from a resolved definition.
type Constructor func(ResolvedDef) (providers.Provider, error)

// ResolvedDef is a ProviderDef with its credential resolved.
type ResolvedDef struct {
	ProviderDef
	// ResolvedKey is the credential from the vault, the
	// environment or the explicit value, in that order.
	ResolvedKey string
	// KeySource reports where ResolvedKey came from: "vault",
	// "env:<VAR>" or "config".
	KeySource string
}

var (
	mu           sync.RWMutex
	constructors = map[string]Constructor{}
)

func init() {
	_ = Register(KindAnthropic, func(d ResolvedDef) (providers.Provider, error) {
		return anthropic.New(anthropic.Options{
			APIKey:      d.ResolvedKey,
			BaseURL:     d.BaseURL,
			Model:       d.Model,
			MaxAttempts: d.MaxAttempts,
		}), nil
	})
	_ = Register(KindGemini, func(d ResolvedDef) (providers.Provider, error) {
		return gemini.New(gemini.Options{
			APIKey:      d.ResolvedKey,
			BaseURL:     d.BaseURL,
			Model:       d.Model,
			MaxAttempts: d.MaxAttempts,
		}), nil
	})
	// The Responses wire is the default OpenAI endpoint for agentic
	// tool calls: Chat Completions rejects reasoning effort with
	// function tools on reasoning models, while Responses carries
	// the fixed per-role effort (Section 19.2).
	_ = Register(KindOpenAI, func(d ResolvedDef) (providers.Provider, error) {
		return openai.New(openai.Options{
			APIKey:       d.ResolvedKey,
			BaseURL:      d.BaseURL,
			UseResponses: true,
		})
	})
	_ = Register(KindDeepSeek, func(d ResolvedDef) (providers.Provider, error) {
		return deepseek.New(deepseek.Options{
			APIKey:  d.ResolvedKey,
			BaseURL: d.BaseURL,
		})
	})
	_ = Register(KindOpenRouter, func(d ResolvedDef) (providers.Provider, error) {
		return openrouter.New(openrouter.Options{
			APIKey:  d.ResolvedKey,
			BaseURL: d.BaseURL,
		})
	})
	_ = Register(KindOllama, func(d ResolvedDef) (providers.Provider, error) {
		return ollama.New(ollama.Options{
			BaseURL: d.BaseURL,
		})
	})
	_ = Register(KindGeneric, func(d ResolvedDef) (providers.Provider, error) {
		return generic.New(generic.Options{
			APIKey:  d.ResolvedKey,
			BaseURL: d.BaseURL,
		})
	})
}

// Register makes a provider kind available. It returns an error
// when kind is blank; re-registering overwrites the previous
// constructor so tests can substitute fakes.
func Register(kind string, ctor Constructor) error {
	if strings.TrimSpace(kind) == "" || ctor == nil {
		return fmt.Errorf("registry: kind and constructor are required")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	mu.Lock()
	defer mu.Unlock()
	constructors[kind] = ctor
	return nil
}

// Kinds lists the registered provider kinds.
func Kinds() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(constructors))
	for k := range constructors {
		out = append(out, k)
	}
	return out
}

// ResolveKey finds the credential for def in vault, environment and
// explicit value order. The returned source is "vault",
// "env:<VAR>" or "config"; both are empty when nothing is set.
func ResolveKey(ctx context.Context, def ProviderDef, resolve SecretResolver) (key, source string, err error) {
	if resolve != nil && strings.TrimSpace(def.APIKeySecret) != "" {
		key, err := resolve(ctx, def.APIKeySecret)
		if err != nil {
			return "", "", fmt.Errorf("registry: vault secret %q: %w", def.APIKeySecret, err)
		}
		if key != "" {
			return key, "vault", nil
		}
	}
	envs := []string{strings.TrimSpace(def.APIKeyEnv)}
	if envs[0] == "" {
		envs = defaultKeyEnv[strings.ToLower(strings.TrimSpace(def.Kind))]
	}
	for _, name := range envs {
		if name == "" {
			continue
		}
		if key := os.Getenv(name); key != "" {
			return key, "env:" + name, nil
		}
	}
	for _, name := range operatorKeyEnv {
		if key := os.Getenv(name); key != "" {
			return key, "env:" + name, nil
		}
	}
	if def.APIKey != "" {
		return def.APIKey, "config", nil
	}
	return "", "", nil
}

// ResolveFallbackKey finds the fallback credential in vault,
// environment and explicit value order. Empty key and source mean no
// fallback is configured; a vault error propagates.
func ResolveFallbackKey(ctx context.Context, def ProviderDef, resolve SecretResolver) (key, source string, err error) {
	if resolve != nil && strings.TrimSpace(def.FallbackKeySecret) != "" {
		key, err := resolve(ctx, def.FallbackKeySecret)
		if err != nil {
			return "", "", fmt.Errorf("registry: vault fallback secret %q: %w", def.FallbackKeySecret, err)
		}
		if key != "" {
			return key, "vault", nil
		}
	}
	envs := []string{strings.TrimSpace(def.FallbackKeyEnv)}
	if envs[0] == "" {
		envs = operatorFallbackEnv
	}
	for _, name := range envs {
		if name == "" {
			continue
		}
		if key := os.Getenv(name); key != "" {
			return key, "env:" + name, nil
		}
	}
	if def.FallbackKey != "" {
		return def.FallbackKey, "config", nil
	}
	return "", "", nil
}

// New builds the provider described by def, resolving its key first.
// When a fallback credential resolves to a different non-empty key,
// the result is wrapped so quota/rate-limit errors retry once with
// the fallback key (Section 19.5); auth and input errors never touch
// it.
func New(ctx context.Context, def ProviderDef, resolve SecretResolver) (providers.Provider, error) {
	kind := strings.ToLower(strings.TrimSpace(def.Kind))
	if kind == "" {
		return nil, fmt.Errorf("registry: provider kind is required")
	}
	mu.RLock()
	ctor, ok := constructors[kind]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("registry: unknown provider kind %q", def.Kind)
	}
	key, source, err := ResolveKey(ctx, def, resolve)
	if err != nil {
		return nil, err
	}
	primary, err := ctor(ResolvedDef{ProviderDef: def, ResolvedKey: key, KeySource: source})
	if err != nil {
		return nil, err
	}
	fallbackKey, _, err := ResolveFallbackKey(ctx, def, resolve)
	if err != nil {
		_ = primary.Close()
		return nil, err
	}
	if fallbackKey == "" || fallbackKey == key {
		return primary, nil
	}
	fallback, err := ctor(ResolvedDef{ProviderDef: def, ResolvedKey: fallbackKey, KeySource: "fallback"})
	if err != nil {
		_ = primary.Close()
		return nil, err
	}
	return withFallback(primary, fallback), nil
}

// NewMany builds every definition, in order. The first failure
// aborts the batch.
func NewMany(ctx context.Context, defs []ProviderDef, resolve SecretResolver) ([]providers.Provider, error) {
	out := make([]providers.Provider, 0, len(defs))
	for i, def := range defs {
		p, err := New(ctx, def, resolve)
		if err != nil {
			return nil, fmt.Errorf("registry: provider %d: %w", i, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// fallbackProvider retries quota/rate-limit failures once with the
// fallback credential (Section 19.5). Any other error kind returns
// at once; the fallback key never sees auth, input or content
// errors. Streaming falls back only when the primary fails before
// emitting anything, so fragments never duplicate.
type fallbackProvider struct {
	primary  providers.Provider
	fallback providers.Provider
}

// withFallback wraps primary with its fallback sibling.
func withFallback(primary, fallback providers.Provider) providers.Provider {
	return &fallbackProvider{primary: primary, fallback: fallback}
}

// quotaLike reports whether err merits one fallback-key attempt:
// transient or rate-limited provider failures only.
func quotaLike(err error) bool {
	perr, ok := err.(*providers.ProviderError)
	return ok && perr.Retryable()
}

// Name implements providers.Provider: the decorator is transparent
// to gateway routing.
func (f *fallbackProvider) Name() string { return f.primary.Name() }

// Chat implements providers.Provider.
func (f *fallbackProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	resp, err := f.primary.Chat(ctx, req)
	if err == nil || !quotaLike(err) {
		return resp, err
	}
	if ctx.Err() != nil {
		return providers.ChatResponse{}, ctx.Err()
	}
	return f.fallback.Chat(ctx, req)
}

// Stream implements providers.Provider.
func (f *fallbackProvider) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	emitted := false
	watch := func(ev providers.StreamEvent) error {
		emitted = true
		return emit(ev)
	}
	err := f.primary.Stream(ctx, req, watch)
	if err == nil || emitted || !quotaLike(err) {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return f.fallback.Stream(ctx, req, emit)
}

// Close implements providers.Provider.
func (f *fallbackProvider) Close() error {
	first := f.primary.Close()
	if err := f.fallback.Close(); err != nil && first == nil {
		first = err
	}
	return first
}
