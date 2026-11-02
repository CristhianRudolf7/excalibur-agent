package wireengine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// openTestVault returns an unlocked vault backed by a temp file.
func openTestVault(t *testing.T) *vault.Vault {
	t.Helper()
	dir := t.TempDir()
	kr := platformfake.NewKeyring()
	v, err := vault.Open(context.Background(), filepath.Join(dir, "vault.db"), vault.Options{Keyring: kr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	if err := v.Unlock(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return v
}

func setSecret(t *testing.T, v *vault.Vault, name, value string) {
	t.Helper()
	if _, err := v.Set(context.Background(), vault.SetParams{Name: name, Type: "token", Value: []byte(value)}); err != nil {
		t.Fatal(err)
	}
}

// TestVaultProviderResolution covers Section 20.1 provider
// credentials: the vault has priority over the environment, and the
// resolved key never reaches the journal or context in clear.
func TestVaultProviderResolution(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	setSecret(t, v, "openai-key", "sk-live-vault-key-1234567890")
	t.Setenv("ANTHROPIC_API_KEY", "env-key-should-lose")
	resolve := VaultResolver(v, "run_vault_1")
	key, source, err := registry.ResolveKey(ctx, registry.ProviderDef{
		Kind: "anthropic", APIKeySecret: "openai-key",
	}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk-live-vault-key-1234567890" || source != "vault" {
		t.Fatalf("key = %q source = %q", key, source)
	}
	// Without a vault the environment still works (eval profile).
	key, source, err = registry.ResolveKey(ctx, registry.ProviderDef{Kind: "anthropic"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if key != "env-key-should-lose" || source != "env:ANTHROPIC_API_KEY" {
		t.Fatalf("env fallback = %q/%q", key, source)
	}
}

// TestVaultExecEnvAndRedaction covers Section 20.1 tool processes:
// secret:// env entries resolve inside the tool process, and every
// output passes through the redactor (value plus base64 variants).
func TestVaultExecEnvAndRedaction(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	secret := "vault-secret-value-abcdef123"
	setSecret(t, v, "github-token", secret)
	emb, err := BuildEmbedded(EmbeddedOptions{RunID: "run_vault_2", Vault: v, Project: "proj1"})
	if err != nil {
		t.Fatal(err)
	}
	// Exec resolves secret:// env entries inside the process.
	args, _ := json.Marshal(map[string]any{
		"command": "echo masked",
		"env":     []string{"GITHUB_TOKEN=secret://github-token"},
	})
	res, err := emb.Exec.Execute(ctx, "exec", args, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK {
		t.Fatalf("exec: %+v", res)
	}
	// The output carries the redaction marker, never the value.
	if strings.Contains(res.Output, secret) {
		t.Fatalf("output leaks the secret: %q", res.Output)
	}
	// Direct redaction covers raw, base64 and URL forms.
	raw := "leaked " + secret + " end"
	if got := v.Redact(raw); !strings.Contains(got, "‹secret:github-token›") || strings.Contains(got, secret) {
		t.Fatalf("redact = %q", got)
	}
	// A locked vault denies with a clear error instead of leaking.
	v.Lock()
	res, err = emb.Exec.Execute(ctx, "exec", args, tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status == tools.StatusOK {
		t.Fatalf("locked vault must not succeed: %+v", res)
	}
	_ = os.Getenv
}

func TestAutonomousLiteralRedactionPreservesWorkspaceCandidates(t *testing.T) {
	t.Setenv("EXCALIBUR_REDACT_PATTERNS_AUTONOMOUS", "off")
	v := openTestVault(t)
	setSecret(t, v, "short-vault-value", "short42")
	providerKey := "provider-credential-for-unit-test"
	candidate := "AKIAABCDEFGHIJKLMNOP"
	path := filepath.Join(t.TempDir(), "workspace.txt")
	if err := os.WriteFile(path, []byte(candidate+" "+providerKey+" "+base64.StdEncoding.EncodeToString([]byte(providerKey))), 0600); err != nil {
		t.Fatal(err)
	}

	for _, autonomous := range []bool{true, false} {
		emb, err := BuildEmbedded(EmbeddedOptions{Vault: v, Autonomous: autonomous, KnownSecrets: [][]byte{[]byte(providerKey)}})
		if err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(map[string]any{"path": path})
		res, err := emb.Exec.Execute(context.Background(), "read_file", args, tools.ExecuteOptions{})
		if err != nil || res.Status != tools.StatusOK {
			t.Fatalf("workspace read: %+v %v", res, err)
		}
		if strings.Contains(res.Output, providerKey) || strings.Contains(res.Output, base64.StdEncoding.EncodeToString([]byte(providerKey))) || strings.Contains(res.Output, candidate) != autonomous {
			t.Fatal("workspace read must preserve candidates and hide known provider credentials")
		}

		got := emb.Exec.Redact(candidate + " " + providerKey + " short42")
		if strings.Contains(got, providerKey) || (autonomous && strings.Contains(got, "short42")) {
			t.Fatal("known credential was not redacted")
		}
		if strings.Contains(got, candidate) != autonomous {
			t.Fatalf("workspace candidate visibility: %q", got)
		}
	}
	t.Setenv("EXCALIBUR_REDACT_PATTERNS_AUTONOMOUS", "on")
	if got := ProfileRedactor(nil, true, nil)(candidate); strings.Contains(got, candidate) {
		t.Fatal("pattern opt-in failed")
	}
}

func TestPDFSecretResolverPolicyAndIdentity(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	setSecret(t, v, "pdf-password", "local-pdf-test-secret")
	if PDFSecretResolver(nil, "run", "project", false) != nil {
		t.Fatal("nil vault must not provide a resolver")
	}
	resolve := PDFSecretResolver(v, "pdf-run", "project", false)
	value, err := resolve(ctx, "secret://pdf-password")
	if err != nil || value != "local-pdf-test-secret" {
		t.Fatalf("resolution failed: %v", err)
	}
	uses, err := v.Usage(ctx, "pdf-password", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) != 1 || uses[0].Tool != "pdf_ops" || uses[0].Dest != string(vault.DestFile) {
		t.Fatalf("wrong secret use: %+v", uses)
	}
	if _, err = PDFSecretResolver(v, "delegated", "unrelated", true)(ctx, "secret://pdf-password"); err == nil {
		t.Fatal("delegate resolved a general secret")
	}
	v.Lock()
	if _, err = resolve(ctx, "secret://pdf-password"); err == nil {
		t.Fatal("locked vault resolved a password")
	}
}
