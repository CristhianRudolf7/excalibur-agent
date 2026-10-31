// Production model wiring for Sections 19.1, 19.2 and 19.4.
//
// ModelSettings is the resolved operator configuration: which
// provider kind and chat model serve every role, the reasoning effort
// fixed per role, the embedding and decisions models, and where the
// credentials live. Resolution merges, in precedence order, the
// EXCALIBUR_* environment aliases, the layered [models] config
// (internal/config), the OpenAI-style operator credentials file and
// the built-in defaults. Keys resolve later at provider build time in
// Section 20.1 order (vault, environment, explicit value) through
// internal/providers/registry; the eval profile passes a nil vault
// resolver so keys arrive by environment only (Section 23.2).
package models

import (
	"fmt"
	"os"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Environment aliases honoured above the layered config file.
const (
	EnvProvider       = "EXCALIBUR_PROVIDER"
	EnvModel          = "EXCALIBUR_MODEL"
	EnvBaseURL        = "EXCALIBUR_BASE_URL"
	EnvReasoning      = "EXCALIBUR_REASONING_EFFORT"
	EnvEmbeddingModel = "EXCALIBUR_EMBEDDING_MODEL"
	EnvDecisionsModel = "EXCALIBUR_DECISIONS_MODEL"
	EnvAPIKey         = "EXCALIBUR_API_KEY"
	EnvAPIKeySecret   = "EXCALIBUR_API_KEY_SECRET"
	EnvFallbackKey    = "EXCALIBUR_FALLBACK_API_KEY"
	EnvFallbackSecret = "EXCALIBUR_FALLBACK_API_KEY_SECRET"
)

// StubProvider selects the explicit deterministic stub instead of a
// real model provider. It is the only silent-run mode; anything else
// without a configured provider is a startup error.
const StubProvider = "stub"

// NoProviderError reports an unconfigured production provider.
type NoProviderError struct {
	Hint string
}

// Error implements error.
func (e *NoProviderError) Error() string {
	return "models: no model provider configured (set " + EnvProvider +
		" or [models].provider, or point " + registry.CredentialsEnv +
		" at the operator credentials file)" + e.Hint
}

// ModelSettings is the resolved production wiring.
type ModelSettings struct {
	// Provider is the registry kind (openai, anthropic, gemini,
	// deepseek, openrouter, ollama, generic), "stub" for the
	// explicit deterministic stub, or "" when unconfigured.
	Provider string
	// Model is the chat model id serving every role (single-model
	// mode in eval; one configured model elsewhere).
	Model string
	// BaseURL overrides the provider default endpoint.
	BaseURL string
	// ReasoningEffort is fixed per role (Section 19.2).
	ReasoningEffort string
	// EmbeddingModel names the embedding model; its operations never
	// carry reasoning effort.
	EmbeddingModel string
	// DecisionsModel names the decisions chat model.
	DecisionsModel string
	// CredentialsFile is the operator file actually loaded, or "".
	CredentialsFile string
	// APIKey, APIKeyEnv, APIKeySecret resolve the credential in
	// vault, environment, explicit order at build time.
	APIKey       string
	APIKeyEnv    string
	APIKeySecret string
	// FallbackKey trio resolves the quota/rate-limit fallback
	// credential, used once per call on retryable failures only.
	FallbackKey       string
	FallbackKeyEnv    string
	FallbackKeySecret string
}

// Stub reports explicit stub selection.
func (s ModelSettings) Stub() bool {
	return strings.ToLower(strings.TrimSpace(s.Provider)) == StubProvider
}

// Configured reports whether a provider was selected at all.
func (s ModelSettings) Configured() bool {
	return strings.TrimSpace(s.Provider) != ""
}

// Validate rejects an unconfigured or stub-less-empty wiring with the
// stable startup error production entry points return.
func (s ModelSettings) Validate() error {
	if !s.Configured() {
		return &NoProviderError{}
	}
	return nil
}

// firstNonEmpty returns the first non-blank string.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ResolveModelSettings merges the operator layers into one effective
// wiring: EXCALIBUR_* aliases first, then the layered config file,
// then the operator credentials file, then built-in defaults.
// getenv reads variables; nil means os.Getenv. When no explicit
// credentials path is set, the default operator path fills gaps only
// if the file exists. An OpenAI-style credentials file without an
// explicit provider selects the openai kind.
func ResolveModelSettings(cfg config.ModelsConfig, getenv func(string) string) (ModelSettings, error) {
	get := getenv
	if get == nil {
		get = os.Getenv
	}
	s := ModelSettings{
		Provider:          firstNonEmpty(get(EnvProvider), cfg.Provider),
		Model:             firstNonEmpty(get(EnvModel), cfg.Model),
		BaseURL:           firstNonEmpty(get(EnvBaseURL), cfg.BaseURL),
		ReasoningEffort:   firstNonEmpty(get(EnvReasoning), cfg.ReasoningEffort),
		EmbeddingModel:    firstNonEmpty(get(EnvEmbeddingModel), cfg.EmbeddingModel),
		DecisionsModel:    firstNonEmpty(get(EnvDecisionsModel), cfg.DecisionsModel),
		APIKey:            firstNonEmpty(get(EnvAPIKey), cfg.APIKey),
		APIKeySecret:      firstNonEmpty(get(EnvAPIKeySecret), cfg.APIKeySecret),
		FallbackKey:       firstNonEmpty(get(EnvFallbackKey), cfg.FallbackKey),
		FallbackKeySecret: firstNonEmpty(get(EnvFallbackSecret), cfg.FallbackKeySecret),
		CredentialsFile:   firstNonEmpty(get(registry.CredentialsEnv), cfg.CredentialsFile),
	}
	if s.APIKeyEnv == "" {
		s.APIKeyEnv = strings.TrimSpace(cfg.APIKeyEnv)
	}
	if s.FallbackKeyEnv == "" {
		s.FallbackKeyEnv = strings.TrimSpace(cfg.FallbackKeyEnv)
	}
	// The explicit EXCALIBUR_API_KEY alias above folds into the
	// explicit value; keep the registry alias lookup as well so
	// kind-specific variables still win over it at build time.
	// Only an explicit path loads the file here (environment
	// alias or config value). The production entry points add the
	// default operator path as a convenience layer on top, so unit
	// tests never depend on the operator's real file.
	path := s.CredentialsFile
	creds, err := registry.LoadOperatorCredentials(path)
	if err != nil {
		return ModelSettings{}, err
	}
	loaded := creds.BaseURL != "" || creds.APIKey != "" || creds.Model != ""
	if loaded {
		s.CredentialsFile = path
		if s.BaseURL == "" {
			s.BaseURL = creds.BaseURL
		}
		if s.Model == "" {
			s.Model = creds.Model
		}
		if s.ReasoningEffort == "" {
			s.ReasoningEffort = creds.ReasoningEffort
		}
		if s.EmbeddingModel == "" {
			s.EmbeddingModel = creds.EmbeddingModel
		}
		if s.DecisionsModel == "" {
			s.DecisionsModel = creds.DecisionsModel
		}
		if s.APIKey == "" {
			s.APIKey = creds.APIKey
		}
		if s.FallbackKey == "" {
			s.FallbackKey = creds.FallbackAPIKey
		}
		if s.Provider == "" {
			s.Provider = registry.KindOpenAI
		}
	}
	with := creds.FillDefaults()
	if s.ReasoningEffort == "" {
		s.ReasoningEffort = with.ReasoningEffort
	}
	if s.EmbeddingModel == "" {
		s.EmbeddingModel = with.EmbeddingModel
	}
	if s.DecisionsModel == "" {
		s.DecisionsModel = with.DecisionsModel
	}
	s.Provider = strings.ToLower(strings.TrimSpace(s.Provider))
	return s, nil
}

// ProviderDef builds the registry definition for the primary
// provider: kind, model, endpoint and credential references. The
// registry resolves the key in vault, environment, explicit order,
// and wraps the fallback key for one quota/rate-limit retry.
func (s ModelSettings) ProviderDef() registry.ProviderDef {
	return registry.ProviderDef{
		Kind: s.Provider, Model: s.Model, BaseURL: s.BaseURL,
		APIKey: s.APIKey, APIKeyEnv: s.APIKeyEnv, APIKeySecret: s.APIKeySecret,
		FallbackKey: s.FallbackKey, FallbackKeyEnv: s.FallbackKeyEnv,
		FallbackKeySecret: s.FallbackKeySecret,
	}
}

// KeyRequired reports whether kind needs a credential to serve
// calls. Local servers without auth (ollama) run keyless; every
// other kind fails fast at startup when nothing resolves.
func KeyRequired(kind string) bool {
	return strings.ToLower(strings.TrimSpace(kind)) != registry.KindOllama
}

// RoleTable builds the Section 19.1 role table: every core role
// routes to the single configured provider/model with the fixed
// reasoning effort. Eval uses the same single-model shape with the
// evaluated model, and the vault never opens there (the caller
// passes a nil resolver). The verifier-different-family guidance
// holds when the operator configures a verifier fallback; with one
// configured model every role shares it.
func (s ModelSettings) RoleTable() map[types.ModelRole]RoleEntry {
	ref := ModelRef{
		Provider: s.Provider, Model: s.Model,
		ReasoningEffort: s.ReasoningEffort,
	}
	roles := make(map[types.ModelRole]RoleEntry, len(CoreRoles()))
	for _, r := range CoreRoles() {
		roles[r] = RoleEntry{Primary: ref}
	}
	return roles
}

// GatewayConfig builds the gateway configuration for the resolved
// wiring: the role table, breaker tuning from the layered config,
// and the operator price version. Per-model prices stay unknown
// (zero table): cost accounting records zero until the operator
// price table lands, which the price version marks.
func (s ModelSettings) GatewayConfig(cfg config.ProviderConfig) GatewayConfig {
	return GatewayConfig{
		Roles:           s.RoleTable(),
		Prices:          map[string]Price{},
		PriceVersion:    "operator-unpriced",
		BreakerAfter:    cfg.BreakerFailures,
		BreakerWindow:   DefaultBreakerWindow,
		BreakerCooldown: cfg.BreakerOpenFor,
		MaxAttempts:     DefaultMaxAttempts,
		BaseDelay:       DefaultBaseDelay,
	}
}

// Decisions returns the Section 19.4 decisions configuration: chat
// decisions carry the configured effort on the decisions model,
// embedding operations never carry it.
func (s ModelSettings) Decisions() DecisionsConfig {
	return DecisionsConfig{
		Model: s.DecisionsModel, ReasoningEffort: s.ReasoningEffort,
		EmbeddingModel: s.EmbeddingModel,
	}
}

// Describe renders the wiring for startup logs without secrets:
// provider, model, endpoint host and key source class only.
func (s ModelSettings) Describe() string {
	endpoint := s.BaseURL
	if endpoint == "" {
		endpoint = "provider-default"
	}
	return fmt.Sprintf("provider=%s model=%s endpoint=%s effort=%s embeddings=%s decisions=%s",
		s.Provider, s.Model, endpoint, s.ReasoningEffort, s.EmbeddingModel, s.DecisionsModel)
}
