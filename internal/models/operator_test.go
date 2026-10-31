package models

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func envGetter(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

// TestResolvePrecedence covers the Section 19.2 layering: explicit
// env aliases beat the config file, which beats the credentials
// file, which beats the built-in defaults.
func TestResolvePrecedence(t *testing.T) {
	dir := t.TempDir()
	creds := `{"base_url":"https://creds.invalid/v1","api_key":"creds-key",` +
		`"model":"creds-model","reasoning_effort":"low",` +
		`"embedding_model":"text-embedding-3-small","decisions_model":"gpt-6-luna"}`
	credsPath := filepath.Join(dir, "openai.json")
	if err := os.WriteFile(credsPath, []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.ModelsConfig{Provider: "openai", Model: "cfg-model", BaseURL: "https://cfg.invalid/v1"}
	vars := map[string]string{
		"EXCALIBUR_MODEL":            "env-model",
		"EXCALIBUR_CREDENTIALS_FILE": credsPath,
	}
	s, err := ResolveModelSettings(cfg, envGetter(vars))
	if err != nil {
		t.Fatal(err)
	}
	if s.Provider != "openai" {
		t.Fatalf("provider from config: %q", s.Provider)
	}
	if s.Model != "env-model" {
		t.Fatalf("env beats file: %q", s.Model)
	}
	if s.BaseURL != "https://cfg.invalid/v1" {
		t.Fatalf("config beats creds file: %q", s.BaseURL)
	}
	if s.ReasoningEffort != "low" || s.EmbeddingModel != "text-embedding-3-small" || s.DecisionsModel != "gpt-6-luna" {
		t.Fatalf("creds/defaults: %+v", s)
	}
	if s.APIKey != "creds-key" || s.CredentialsFile != credsPath {
		t.Fatalf("creds key/path: %+v", s)
	}
}

// TestResolveDefaults covers an empty operator: effort low and the
// Section 19.4 models apply, the provider stays unconfigured.
func TestResolveDefaults(t *testing.T) {
	s, err := ResolveModelSettings(config.ModelsConfig{}, envGetter(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Configured() || s.Stub() {
		t.Fatalf("empty must be unconfigured: %+v", s)
	}
	if s.ReasoningEffort != "low" || s.EmbeddingModel != DefaultEmbeddingModel || s.DecisionsModel != DefaultDecisionsModel {
		t.Fatalf("defaults: %+v", s)
	}
	verr := s.Validate()
	if verr == nil {
		t.Fatal("unconfigured must fail validation")
	}
	if _, ok := verr.(*NoProviderError); !ok {
		t.Fatalf("error type = %T", verr)
	}
}

// TestResolveCredentialsSelectsOpenAI covers the operator-file
// inference: an OpenAI-style file without an explicit provider
// selects the openai kind.
func TestResolveCredentialsSelectsOpenAI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openai.json")
	if err := os.WriteFile(path, []byte(`{"model":"gpt-6-luna","api_key":"k"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := ResolveModelSettings(config.ModelsConfig{}, envGetter(map[string]string{"EXCALIBUR_CREDENTIALS_FILE": path}))
	if err != nil {
		t.Fatal(err)
	}
	if s.Provider != "openai" || s.Model != "gpt-6-luna" {
		t.Fatalf("inferred: %+v", s)
	}
}

// TestResolveMalformedCredentialsFails covers corrupt operator
// files: resolution fails instead of silently running unconfigured.
func TestResolveMalformedCredentialsFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openai.json")
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveModelSettings(config.ModelsConfig{}, envGetter(map[string]string{"EXCALIBUR_CREDENTIALS_FILE": path})); err == nil {
		t.Fatal("malformed credentials must fail")
	}
}

// TestRoleTableSingleModel covers Section 19.1: every core role
// routes to the one configured provider/model with the fixed
// effort, including the decider role.
func TestRoleTableSingleModel(t *testing.T) {
	s := ModelSettings{Provider: "openai", Model: "gpt-6-luna", ReasoningEffort: "low"}
	roles := s.RoleTable()
	for _, r := range CoreRoles() {
		entry, ok := roles[r]
		if !ok {
			t.Fatalf("role %q missing", string(r))
		}
		if entry.Primary.Provider != "openai" || entry.Primary.Model != "gpt-6-luna" {
			t.Fatalf("role %q = %+v", string(r), entry.Primary)
		}
		if entry.Primary.ReasoningEffort != "low" {
			t.Fatalf("role %q effort = %q", string(r), entry.Primary.ReasoningEffort)
		}
	}
	if err := ValidateRoles(roles); err != nil {
		t.Fatalf("role table must validate: %v", err)
	}
	if got := RequireRoles(roles, CoreRoles()); len(got) != 0 {
		t.Fatalf("missing roles: %v", got)
	}
}

// TestProviderDefMapping covers the registry handoff: kind, model,
// endpoint and credential references travel together.
func TestProviderDefMapping(t *testing.T) {
	s := ModelSettings{
		Provider: "openai", Model: "gpt-6-luna", BaseURL: "https://api.openai.com/v1",
		APIKeySecret: "secret://provider-key", FallbackKey: "fb",
	}
	def := s.ProviderDef()
	if def.Kind != "openai" || def.Model != "gpt-6-luna" || def.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("def = %+v", def)
	}
	if def.APIKeySecret != "secret://provider-key" || def.FallbackKey != "fb" {
		t.Fatalf("key refs = %+v", def)
	}
}

// TestEmbeddingOmitsEffort pins the contract rule through the
// settings: chat decisions carry the effort, embedding operations
// never do.
func TestEmbeddingOmitsEffort(t *testing.T) {
	s := ModelSettings{ReasoningEffort: "low", EmbeddingModel: "text-embedding-3-small", DecisionsModel: "gpt-6-luna"}
	d := s.Decisions()
	if !OmitReasoningEffort("embeddings") {
		t.Fatal("embeddings must omit reasoning_effort")
	}
	if OmitReasoningEffort("chat") {
		t.Fatal("chat must carry reasoning_effort")
	}
	if got := d.EmbeddingRequest("hi"); got["model"] != "text-embedding-3-small" {
		t.Fatalf("embedding request = %v", got)
	}
	if _, ok := d.EmbeddingRequest("hi")["reasoning_effort"]; ok {
		t.Fatal("embedding request must not carry reasoning_effort")
	}
	if chat := d.ChatRequest("", nil); chat.ReasoningEffort != "low" {
		t.Fatalf("chat effort = %q", chat.ReasoningEffort)
	}
}

// TestKeyRequired covers the startup gate: local keyless servers run
// without a credential, every remote kind needs one.
func TestKeyRequired(t *testing.T) {
	if KeyRequired("ollama") {
		t.Fatal("ollama needs no key")
	}
	for _, kind := range []string{"openai", "anthropic", "gemini", "deepseek", "openrouter", "generic"} {
		if !KeyRequired(kind) {
			t.Fatalf("%s needs a key", kind)
		}
	}
}

// TestStubSelection covers the only silent-run mode: the explicit
// stub provider validates without a model or a key.
func TestStubSelection(t *testing.T) {
	s, err := ResolveModelSettings(config.ModelsConfig{Provider: "stub"}, envGetter(map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Stub() || !s.Configured() {
		t.Fatalf("stub: %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("stub validates: %v", err)
	}
	var _ = types.RoleExecutor
}
