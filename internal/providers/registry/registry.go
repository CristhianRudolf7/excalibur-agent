package registry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/anthropic"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/gemini"
)

// Provider kinds built in here. Further H1 kinds (OpenAI family)
// register themselves from their own packages via Register.
const (
	KindAnthropic = "anthropic"
	KindGemini    = "gemini"
)

// Default environment variables per kind, in lookup order.
var defaultKeyEnv = map[string][]string{
	KindAnthropic: {"ANTHROPIC_API_KEY"},
	KindGemini:    {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
}

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
	if def.APIKey != "" {
		return def.APIKey, "config", nil
	}
	return "", "", nil
}

// New builds the provider described by def, resolving its key first.
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
	return ctor(ResolvedDef{ProviderDef: def, ResolvedKey: key, KeySource: source})
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
