package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
)

// testVault opens an unlocked vault in a temp dir with a fake keyring.
func testVault(t *testing.T, opts *Options) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	var o Options
	if opts != nil {
		o = *opts
	}
	if o.Keyring == nil {
		o.Keyring = platformfake.NewKeyring()
	}
	ctx := context.Background()
	v, err := Open(ctx, filepath.Join(dir, "vault.db"), o)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	if err := v.Unlock(ctx, nil); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if v.IsLocked() {
		t.Fatal("vault still locked after unlock")
	}
	return v, dir
}

// testVaultLocked opens a vault without unlocking.
func testVaultLocked(t *testing.T) (*Vault, string) {
	t.Helper()
	dir := t.TempDir()
	v, err := Open(context.Background(), filepath.Join(dir, "vault.db"), Options{Keyring: platformfake.NewKeyring()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v, dir
}

// TestVault_AllTypes_NoCleartext covers 20.2 row 1: every secret type
// round-trips and no value (raw, base64 or URL-encoded) appears in the
// database file or in a raw dump of its tables.
func TestVault_AllTypes_NoCleartext(t *testing.T) {
	v, dir := testVault(t, nil)
	ctx := context.Background()
	secrets := map[string]struct {
		typ   string
		value string
		meta  map[string]string
	}{
		"github-token":   {TypeAPIKey, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123", map[string]string{"service": "github", "base_url": "https://api.github.com"}},
		"openai-cred":    {TypeProviderCredential, "sk-ant-testproviderkey0123456789abcdef", map[string]string{"provider": "anthropic"}},
		"banco-web":      {TypeWebsitePassword, "S3cur3-P@ssw0rd-banco-xyz!", map[string]string{"url": "https://banco.example.com", "user": "ana"}},
		"home-server":    {TypeHost, "ssh-private-key-material-home-server-001", map[string]string{"host": "192.168.1.10", "port": "22", "user": "cristhian"}},
		"deploy-key":     {TypeSSHKey, "-----BEGIN OPENSSH PRIVATE KEY-----\nfakekeydeploy001\n-----END OPENSSH PRIVATE KEY-----", map[string]string{"hosts": "git.example.com"}},
		"ci-token":       {TypeToken, "ci-token-value-abcdef-123456", map[string]string{"service": "ci"}},
		"spare-note":     {TypeNote, "note text that is also secret material 007", map[string]string{"title": "spare"}},
		"api-key-withip": {TypeAPIKey, "192.168.99.100-secret-suffix", map[string]string{"service": "ip-bound"}},
	}
	for name, s := range secrets {
		if _, err := v.Set(ctx, SetParams{Name: name, Type: s.typ, Value: []byte(s.value), Meta: s.meta}); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}
	// Resolve each and check the value matches.
	for name, s := range secrets {
		res, err := v.Resolve(ctx, ResolveRequest{Ref: "secret://" + name, Tool: "exec", RunID: "run_test"})
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		if string(res.Value) != s.value {
			t.Fatalf("resolve %s: mismatch", name)
		}
		ZeroBytes(res.Value)
	}
	assertPrivateFile(t, filepath.Join(dir, "vault.db"))
	// Raw file and logical dump must not contain any value or encoding.
	raw, err := os.ReadFile(filepath.Join(dir, "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	dump := dumpTables(t, v)
	for name, s := range secrets {
		for _, variant := range valueVariants(s.value) {
			if variant == "" {
				continue
			}
			if strings.Contains(string(raw), variant) {
				t.Fatalf("vault.db file contains %s value variant of %s", redactLen(variant), name)
			}
			if strings.Contains(dump, variant) {
				t.Fatalf("vault.db dump contains value variant of %s", name)
			}
		}
	}
}

func redactLen(s string) string {
	if len(s) > 8 {
		return s[:4] + "...(len " + itoa(len(s)) + ")"
	}
	return "short"
}

// assertPrivateFile checks the native protection (0600 on POSIX, DACL on Windows).
func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	if ok, err := platform.IsOwnerOnly(path); err != nil || !ok {
		t.Fatalf("file %q is not private: %v", path, err)
	}
}
