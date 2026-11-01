package cli

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
)

// TestResolveSettingsLayers covers the production loader: the
// environment aliases beat the layered config, the explicit
// credentials file fills gaps, and SkipFiles isolates tests from
// the operator's real files.
func TestResolveSettingsLayers(t *testing.T) {
	dir := t.TempDir()
	creds := `{"base_url":"https://creds.invalid/v1","api_key":"creds-key",` +
		`"fallback_api_key":"creds-fb","model":"creds-model","reasoning_effort":"low",` +
		`"embedding_model":"text-embedding-3-small","decisions_model":"gpt-6-luna"}`
	credsPath := filepath.Join(dir, "openai.json")
	if err := os.WriteFile(credsPath, []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[models]\nprovider = \"openai\"\nmodel = \"cfg-model\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, _, err := ResolveSettings(config.LoadOptions{
		UserPath: cfgPath, SkipFiles: false, NoEnv: false,
		Env: []string{"EXCALIBUR_MODEL=env-model", "EXCALIBUR_CREDENTIALS_FILE=" + credsPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider != "openai" {
		t.Fatalf("provider from file: %q", settings.Provider)
	}
	if settings.Model != "env-model" {
		t.Fatalf("env beats file: %q", settings.Model)
	}
	if settings.BaseURL != "https://creds.invalid/v1" || settings.APIKey != "creds-key" {
		t.Fatalf("creds fill gaps: %+v", settings)
	}
	if settings.CredentialsFile != credsPath {
		t.Fatalf("creds path: %q", settings.CredentialsFile)
	}
	def := settings.ProviderDef()
	if def.Kind != "openai" || def.Model != "env-model" || def.FallbackKey != "creds-fb" {
		t.Fatalf("provider def = %+v", def)
	}
}

// TestResolveSettingsIsolated covers SkipFiles: no file layer loads,
// aliases and built-in defaults still apply.
func TestResolveSettingsIsolated(t *testing.T) {
	settings, _, err := ResolveSettings(config.LoadOptions{
		SkipFiles: true, Env: []string{"EXCALIBUR_PROVIDER=openai"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider != "openai" || settings.ReasoningEffort != "low" {
		t.Fatalf("isolated = %+v", settings)
	}
}

// TestBuildProviderErrors covers the fail-fast startup rules:
// unknown kinds and missing keys are harness errors, never silent.
func TestBuildProviderErrors(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("EXCALIBUR_API_KEY", "")
	if _, err := BuildProvider(ctx, models.ModelSettings{Provider: "acme", Model: "m"}, nil); err == nil {
		t.Fatal("unknown kind must fail")
	} else if ExitCode(err) != ExitError {
		t.Fatalf("exit = %d", ExitCode(err))
	}
	if _, err := BuildProvider(ctx, models.ModelSettings{Provider: "openai", Model: "m"}, nil); err == nil {
		t.Fatal("missing key must fail")
	} else if ExitCode(err) != ExitError {
		t.Fatalf("exit = %d", ExitCode(err))
	}
	if _, err := BuildProvider(ctx, models.ModelSettings{Provider: "stub"}, nil); err == nil {
		t.Fatal("stub has no registry adapter")
	}
	// Keyless local servers build without credentials.
	p, err := BuildProvider(ctx, models.ModelSettings{Provider: "ollama", Model: "m", BaseURL: "http://127.0.0.1:9"}, nil)
	if err != nil {
		t.Fatalf("ollama builds keyless: %v", err)
	}
	_ = p.Close()
}

// TestBuildGatewayRoles covers the gateway handoff: every core role
// routes to the configured provider/model with the fixed effort.
func TestBuildGatewayRoles(t *testing.T) {
	ctx := context.Background()
	p, err := BuildProvider(ctx, models.ModelSettings{Provider: "ollama", Model: "m", BaseURL: "http://127.0.0.1:9"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	settings := models.ModelSettings{Provider: "ollama", Model: "m", ReasoningEffort: "low"}
	gw := BuildGateway(settings, config.Default(), p)
	defer gw.Close()
	if gw == nil {
		t.Fatal("gateway must build")
	}
	roles := settings.RoleTable()
	for _, r := range models.CoreRoles() {
		entry, ok := roles[r]
		if !ok || entry.Primary.Provider != "ollama" || entry.Primary.Model != "m" {
			t.Fatalf("role %q = %+v", string(r), entry)
		}
	}
}

// TestBuildEmbedderDegrades covers Section 10.8: without an endpoint
// or a key the builder returns nil so callers degrade to
// lexical/temporal/relational retrieval; a configured operator
// yields an embedder whose model carries no reasoning effort.
func TestBuildEmbedderDegrades(t *testing.T) {
	if emb, err := BuildEmbedder(models.ModelSettings{}, nil); err != nil || emb != nil {
		t.Fatalf("unconfigured must degrade: %v %v", emb, err)
	}
	if emb, err := BuildEmbedder(models.ModelSettings{Provider: "stub"}, nil); err != nil || emb != nil {
		t.Fatalf("stub must degrade: %v %v", emb, err)
	}
	t.Setenv("OPENAI_API_KEY", "test-key")
	emb, err := BuildEmbedder(models.ModelSettings{
		Provider: "openai", Model: "gpt-6-luna", EmbeddingModel: "text-embedding-3-small",
	}, nil)
	if err != nil || emb == nil {
		t.Fatalf("configured must build: %v %v", emb, err)
	}
	if emb.Model() != "text-embedding-3-small" {
		t.Fatalf("embedding model = %q", emb.Model())
	}
}

// TestBuildDeciderDegrades covers Section 19.4: no gateway (or a
// stub operator) yields no decider and callers use deterministic
// rules; a configured operator yields the LLM fallback on the
// decisions model.
func TestBuildDeciderDegrades(t *testing.T) {
	if d := BuildDecider(models.ModelSettings{}, nil); d != nil {
		t.Fatal("no gateway must yield no decider")
	}
	if d := BuildDecider(models.ModelSettings{Provider: "stub", Model: "m"}, nil); d != nil {
		t.Fatal("stub must yield no decider")
	}
	ctx := context.Background()
	p, err := BuildProvider(ctx, models.ModelSettings{Provider: "ollama", Model: "m", BaseURL: "http://127.0.0.1:9"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	settings := models.ModelSettings{Provider: "ollama", Model: "m", DecisionsModel: "gpt-6-luna"}
	gw := BuildGateway(settings, config.Default(), p)
	defer gw.Close()
	d := BuildDecider(settings, gw)
	if d == nil {
		t.Fatal("configured must yield a decider")
	}
	ld, ok := d.(*models.LLMDecider)
	if !ok {
		t.Fatalf("decider type = %T", d)
	}
	if ld.Model() != "gpt-6-luna" {
		t.Fatalf("decisions model = %q", ld.Model())
	}
}

// TestVaultSecretResolverNil covers the eval rule: without a vault
// the hook is nil and keys resolve from the environment only.
func TestVaultSecretResolverNil(t *testing.T) {
	if VaultSecretResolver(nil, "run-1") != nil {
		t.Fatal("nil vault must yield a nil hook")
	}
}

func TestRunCredentialsProtectProviderValuesInEval(t *testing.T) {
	t.Setenv("EXCALIBUR_API_KEY", "unit-provider-credential-value")
	t.Setenv("EXCALIBUR_REDACT_PATTERNS_AUTONOMOUS", "off")
	settings := models.ModelSettings{Provider: "openai"}
	values := runCredentialValues(context.Background(), settings, nil, "test-run")
	redact := wireengine.ProfileRedactor(nil, true, values)
	got := redact("unit-provider-credential-value AKIAABCDEFGHIJKLMNOP")
	if strings.Contains(got, "unit-provider-credential-value") || !strings.Contains(got, "AKIAABCDEFGHIJKLMNOP") {
		t.Fatal("eval must hide provider credentials and preserve workspace candidates")
	}
}
