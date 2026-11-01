package cli

// Production model wiring for every entry point (Sections 19.1,
// 19.2, 19.4, 20.1): excalibur exec, task background runs, the MCP
// and ACP servers, the daemon/master, memory embeddings (10.8) and
// the decider. All providers build through
// internal/providers/registry from the layered config, the
// EXCALIBUR_* environment, the operator credentials file and
// secret:// vault refs; every model call runs through the
// internal/models gateway role table (single-model mode in eval).
//
// Ordinary tests never touch this file's production loader: they
// inject providers.Provider fakes through ExecOptions.Provider, or
// set EXCALIBUR_PROVIDER=stub for the explicit deterministic path.
// resolveSettings is the single seam for tests that need to simulate
// an unconfigured operator without touching real files.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// resolveSettings loads the production model wiring from the layered
// config, environment aliases and the operator credentials file.
// Tests override it to simulate wiring states without reading the
// operator's real files.
var resolveSettings = func() (models.ModelSettings, config.Config, error) {
	return ResolveSettings(config.LoadOptions{})
}

// ResolveSettings merges the operator layers with opts controlling
// the file layers (production passes zero LoadOptions: system, user,
// project and scope TOML plus the environment). SkipFiles keeps the
// environment aliases and any explicit EXCALIBUR_CREDENTIALS_FILE
// but ignores every file layer including the default operator path,
// which is what unit tests use for isolation. A malformed layer is
// an error, never a silent fallback.
func ResolveSettings(opts config.LoadOptions) (models.ModelSettings, config.Config, error) {
	cfg, err := config.Load(opts)
	if err != nil {
		return models.ModelSettings{}, config.Config{}, fmt.Errorf("cli: load config: %w", err)
	}
	// Thread the same environment list the loader used into alias
	// resolution, so tests with an explicit Env list stay hermetic
	// and production (nil Env) reads the process environment.
	get := envGetter(opts.Env)
	settings, err := models.ResolveModelSettings(cfg.Models, get)
	if err != nil {
		return models.ModelSettings{}, config.Config{}, fmt.Errorf("cli: resolve model wiring: %w", err)
	}
	if settings.CredentialsFile == "" && !opts.SkipFiles {
		if path := registry.DefaultCredentialsPath(); path != "" {
			if _, statErr := os.Stat(path); statErr == nil {
				layered := cfg.Models
				layered.CredentialsFile = path
				settings, err = models.ResolveModelSettings(layered, get)
				if err != nil {
					return models.ModelSettings{}, config.Config{}, fmt.Errorf("cli: resolve model wiring: %w", err)
				}
			}
		}
	}
	return settings, cfg, nil
}

// envGetter adapts an explicit environment list to the alias lookup;
// nil means the process environment.
func envGetter(env []string) func(string) string {
	if env == nil {
		return nil
	}
	table := make(map[string]string, len(env))
	for _, kv := range env {
		if name, value, ok := cutKV(kv); ok {
			table[name] = value
		}
	}
	return func(k string) string { return table[k] }
}

// cutKV splits one "NAME=value" entry.
func cutKV(kv string) (string, string, bool) {
	for i := 0; i < len(kv); i++ {
		if kv[i] == '=' {
			return kv[:i], kv[i+1:], true
		}
	}
	return "", "", false
}

// VaultSecretResolver adapts an unlocked vault to the registry key
// hook (Section 20.1: the vault wins over environment and config).
// A nil vault returns a nil hook so keys resolve from the
// environment, the config and the credentials file. Eval callers
// always pass nil: the eval profile never opens the vault and keys
// arrive by environment only (Section 23.2).
func VaultSecretResolver(v *vault.Vault, runID string) registry.SecretResolver {
	if v == nil {
		return nil
	}
	return registry.SecretResolver(v.SecretResolverFunc(runID))
}

// BuildProvider constructs the registry provider for settings: kind,
// model, endpoint and credential references resolve here, the key in
// vault, environment, explicit order. Kinds needing a credential
// fail fast with the variables to set instead of failing at the
// first model call. The fallback credential, when distinct, wraps
// the primary for one quota/rate-limit retry per call.
func BuildProvider(ctx context.Context, settings models.ModelSettings, resolve registry.SecretResolver) (providers.Provider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if settings.Stub() {
		return nil, fmt.Errorf("cli: stub provider has no registry adapter (deterministic path only)")
	}
	if err := settings.Validate(); err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	def := settings.ProviderDef()
	if !knownKind(def.Kind) {
		kinds := append([]string(nil), registry.Kinds()...)
		sort.Strings(kinds)
		return nil, &CodedError{Code: ExitError, Err: fmt.Errorf(
			"cli: unknown provider kind %q (one of: %s)", def.Kind, strings.Join(kinds, ", "))}
	}
	if models.KeyRequired(def.Kind) {
		key, _, err := registry.ResolveKey(ctx, def, resolve)
		if err != nil {
			return nil, &CodedError{Code: ExitError, Err: err}
		}
		if key == "" {
			return nil, &CodedError{Code: ExitError, Err: fmt.Errorf(
				"cli: no API key for provider %q (set %s, EXCALIBUR_API_KEY, [models].api_key, a secret:// vault ref, or the operator credentials file)",
				def.Kind, envKeyHint(def.Kind))}
		}
	}
	p, err := models.NewProvider(ctx, def, resolve)
	if err != nil {
		return nil, &CodedError{Code: ExitError, Err: err}
	}
	return p, nil
}

// knownKind reports whether kind is a registered provider kind,
// case-insensitively. It fails fast with a clear error before key
// resolution so typos surface as unknown kinds, not missing keys.
func knownKind(kind string) bool {
	for _, k := range registry.Kinds() {
		if strings.EqualFold(k, kind) {
			return true
		}
	}
	return false
}

// envKeyHint names the kind-specific key variable for errors.
func envKeyHint(kind string) string {
	switch kind {
	case registry.KindOpenAI:
		return "OPENAI_API_KEY"
	case registry.KindAnthropic:
		return "ANTHROPIC_API_KEY"
	case registry.KindGemini:
		return "GEMINI_API_KEY or GOOGLE_API_KEY"
	case registry.KindDeepSeek:
		return "DEEPSEEK_API_KEY"
	case registry.KindOpenRouter:
		return "OPENROUTER_API_KEY"
	case registry.KindGeneric:
		return "the server key"
	default:
		return "the provider key"
	}
}

// BuildGateway registers p on the Section 19.1 role table: every
// core role routes to the single configured provider/model with the
// fixed reasoning effort (single-model mode in eval; one configured
// model elsewhere).
func BuildGateway(settings models.ModelSettings, cfg config.Config, p providers.Provider) *models.Gateway {
	gw := models.NewGateway(settings.GatewayConfig(cfg.Provider))
	gw.Register(p)
	return gw
}

// BuildEmbedder constructs the Section 10.8 remote embedding
// provider from the same operator wiring: the embedding model with
// the shared key and endpoint, the fallback key for one
// quota/rate-limit retry per call. It returns (nil, nil) when no
// endpoint can be built (no key or no URL for a keyless-inapplicable
// kind): the caller degrades to lexical, temporal and relational
// retrieval with a notice instead of failing to start. Embedding
// requests never carry reasoning effort.
func BuildEmbedder(settings models.ModelSettings, resolve registry.SecretResolver) (*memory.HTTPEmbedder, error) {
	if settings.Stub() || !settings.Configured() {
		return nil, nil
	}
	base := settings.BaseURL
	if base == "" && settings.Provider == registry.KindOpenAI {
		// Mirror the adapter default; explicit config always wins.
		base = openai.DefaultBaseURL
	}
	if base == "" {
		return nil, nil
	}
	def := settings.ProviderDef()
	key, _, err := registry.ResolveKey(context.Background(), def, resolve)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, nil
	}
	fallback, _, err := registry.ResolveFallbackKey(context.Background(), def, resolve)
	if err != nil {
		return nil, err
	}
	return memory.NewHTTPEmbedder(memory.HTTPOptions{
		BaseURL: base, APIKey: key, FallbackKey: fallback,
		Model: settings.EmbeddingModel,
	})
}

// BuildDecider returns the Section 19.4 decider over the gateway: a
// cheap-model LLM fallback on the decisions model with the
// configured effort. A nil gateway (unconfigured or stub operator)
// yields a nil decider and callers degrade to deterministic rules.
func BuildDecider(settings models.ModelSettings, gw models.ChatCaller) models.Decider {
	if gw == nil || settings.Stub() || !settings.Configured() {
		return nil
	}
	return models.NewLLMDecider(gw, settings.DecisionsModel)
}

// runCredentialValues resolves the same settings as the provider so literal-only
// output redaction also protects keys from environment, config and operator files.
func runCredentialValues(ctx context.Context, settings models.ModelSettings, v *vault.Vault, runID string) [][]byte {
	def := settings.ProviderDef()
	resolve := VaultSecretResolver(v, runID)
	var values [][]byte
	if key, _, err := registry.ResolveKey(ctx, def, resolve); err == nil && key != "" {
		values = append(values, []byte(key))
	}
	if key, _, err := registry.ResolveFallbackKey(ctx, def, resolve); err == nil && key != "" {
		values = append(values, []byte(key))
	}
	return values
}
