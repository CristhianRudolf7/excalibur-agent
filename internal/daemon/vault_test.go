package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

const testPassphrase = "daemon test passphrase"

// openTestVault opens a fresh vault unlocked by a passphrase file, the
// unattended start of Section 20.1.
func openTestVault(t *testing.T) (*vault.Vault, string) {
	t.Helper()
	dir := t.TempDir()
	pf := filepath.Join(dir, "passphrase")
	if err := os.WriteFile(pf, []byte(testPassphrase), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := OpenVault(context.Background(), VaultOpenOptions{
		Path: filepath.Join(dir, "vault.db"), PassphraseFile: pf,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v, pf
}

// TestOpenVaultStartFlows covers the two starts that matter: a
// passphrase file unlocks at open, and with no key source the vault
// starts locked without failing the daemon.
func TestOpenVaultStartFlows(t *testing.T) {
	ctx := context.Background()
	v, pf := openTestVault(t)
	path := filepath.Join(filepath.Dir(pf), "vault.db")
	if v.IsLocked() || v.KEKKind() != "passphrase" {
		t.Fatalf("passphrase file must unlock at open (locked=%v kind=%q)", v.IsLocked(), v.KEKKind())
	}
	_ = v.Close()

	// Reopening the same vault without its passphrase must stay locked:
	// an empty passphrase never derives a key.
	again, err := OpenVault(ctx, VaultOpenOptions{Path: path})
	if err != nil {
		t.Fatalf("reopen without a key source must not fail the daemon: %v", err)
	}
	if !again.IsLocked() {
		t.Fatal("a passphrase vault reopened without its passphrase must stay locked")
	}
	_ = again.Close()

	dir := t.TempDir()
	locked, err := OpenVault(ctx, VaultOpenOptions{Path: filepath.Join(dir, "vault.db")})
	if err != nil {
		t.Fatalf("a vault without a key source must still open: %v", err)
	}
	defer func() { _ = locked.Close() }()
	if !locked.IsLocked() {
		t.Fatal("no key source must leave the vault locked")
	}
}

// TestVaultAPIRoundTripThroughGateway drives the vault.* family over the
// gate: create, list (never values), lock, unlock, delete.
func TestVaultAPIRoundTripThroughGateway(t *testing.T) {
	ctx := context.Background()
	v, _ := openTestVault(t)
	bus := telemetry.NewBus(nil)
	backend := NewDaemonBackend(nil, nil, bus)
	backend.AttachServices(BackendServices{Vault: v})
	g := gateway.New(gateway.DefaultConfig(), backend, gateway.NewMemoryBlobs(), bus)
	addr, err := g.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Stop(context.Background()) })
	c := gateway.Dial(addr, "vault-test")
	call := func(method string, params any) (json.RawMessage, error) {
		return c.Call(ctx, "req-"+method, method, params)
	}

	const secret = "sk-daemon-roundtrip-7731"
	if _, err := call(api.MethodVaultCreate, api.VaultEntryParams{
		Name: "daemon-key", Type: vault.TypeAPIKey, Value: secret,
		Metadata: map[string]string{"service": "example"},
	}); err != nil {
		t.Fatalf("vault.create: %v", err)
	}
	raw, err := call(api.MethodVaultListMetadata, map[string]any{})
	if err != nil {
		t.Fatalf("vault.list_metadata: %v", err)
	}
	if !strings.Contains(string(raw), "daemon-key") {
		t.Fatalf("listing lacks the entry: %s", raw)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("metadata listing carried the secret value")
	}
	if _, err := call(api.MethodVaultLock, map[string]any{}); err != nil {
		t.Fatalf("vault.lock: %v", err)
	}
	if !v.IsLocked() {
		t.Fatal("vault.lock must lock the daemon vault")
	}
	if _, err := call(api.MethodVaultUnlock, map[string]any{"passphrase": "wrong passphrase"}); err == nil {
		t.Fatal("a wrong passphrase must not unlock")
	}
	if _, err := call(api.MethodVaultUnlock, map[string]any{"passphrase": testPassphrase}); err != nil {
		t.Fatalf("vault.unlock: %v", err)
	}
	if _, err := call(api.MethodVaultDelete, map[string]any{"name": "daemon-key"}); err == nil {
		t.Fatal("vault.delete without confirmed must refuse")
	}
	if _, err := call(api.MethodVaultDelete, map[string]any{"name": "daemon-key", "confirmed": true}); err != nil {
		t.Fatalf("vault.delete: %v", err)
	}
}

// TestProviderHookResolvesAndLocks covers the provider credential path:
// a secret:// reference resolves through the vault at call time, and a
// locked vault refuses rather than falling through silently.
func TestProviderHookResolvesAndLocks(t *testing.T) {
	ctx := context.Background()
	v, _ := openTestVault(t)
	const key = "sk-provider-hook-test-123"
	if _, err := v.Create(ctx, vault.CreateParams{
		Name: "provider-key", Type: vault.TypeProviderCredential, Value: []byte(key),
	}); err != nil {
		t.Fatal(err)
	}
	hook := VaultSecretResolver(v)
	got, err := hook(ctx, "secret://provider-key")
	if err != nil || got != key {
		t.Fatalf("provider hook = %q, %v; want the vault value", got, err)
	}
	v.LockVault()
	if _, err := hook(ctx, "secret://provider-key"); err == nil {
		t.Fatal("a locked vault must not resolve provider keys")
	}
}

// TestDaemonExecutorRedactsVaultValue runs one exec tool call on the
// daemon executor with a vault reference in its environment. The value
// must reach the process, and the output must carry the redaction
// marker instead of the value (Section 20.1 steps 5 and env destination).
func TestDaemonExecutorRedactsVaultValue(t *testing.T) {
	ctx := context.Background()
	v, _ := openTestVault(t)
	const value = "tok-redact-test-9921"
	if _, err := v.Create(ctx, vault.CreateParams{
		Name: "exec-token", Type: vault.TypeToken, Value: []byte(value),
	}); err != nil {
		t.Fatal(err)
	}
	svc, err := Assemble(AssembleOptions{Vault: v})
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{
		"command":   `printf '%s' "$EXEC_TOKEN"`,
		"env":       []string{"EXEC_TOKEN=secret://exec-token"},
		"timeout_s": 20,
	})
	res, err := svc.Exec.Execute(ctx, "exec", args, tools.ExecuteOptions{})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if strings.Contains(res.Output, value) {
		t.Fatalf("output leaked the secret value: %q", res.Output)
	}
	if !strings.Contains(res.Output, "‹secret:exec-token›") {
		t.Fatalf("output lacks the redaction marker; resolution or redaction did not run: %q", res.Output)
	}
}
