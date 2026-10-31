package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
)

// TestExport_Import covers 20.2 row 8: vault export/import into a fresh
// installation keeps names, metadata and values; the exported file
// holds no cleartext values.
func TestExport_Import(t *testing.T) {
	v, dir := testVault(t, nil)
	ctx := context.Background()
	exp := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	if _, err := v.Set(ctx, SetParams{
		Name: "github-token", Type: TypeAPIKey, Value: []byte("ghp_EXPORTTESTVALUE0123456789"),
		Meta: map[string]string{"service": "github"}, Scopes: []string{"proj-a"},
		AllowedTools: []string{"exec"}, Domains: []string{"api.github.com"}, ExpiresAt: &exp,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Set(ctx, SetParams{Name: "note-1", Type: TypeNote, Value: []byte("export-note-value-1")}); err != nil {
		t.Fatal(err)
	}
	backupPP := []byte("backup-passphrase-001")
	data, err := v.Export(ctx, backupPP)
	ZeroBytes(backupPP)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	for _, needle := range []string{"ghp_EXPORTTESTVALUE0123456789", "export-note-value-1"} {
		if strings.Contains(string(data), needle) {
			t.Fatalf("backup contains cleartext value %q", needle)
		}
		for _, variant := range valueVariants(needle) {
			if len(variant) >= 8 && strings.Contains(string(data), variant) {
				t.Fatalf("backup contains encoded value of %q", needle)
			}
		}
	}
	// Wrong passphrase fails.
	if _, err := v.Import(ctx, data, []byte("wrong-backup-pass")); err == nil {
		t.Fatal("import with wrong passphrase succeeded")
	}
	// Fresh installation: new vault.db, new keyring.
	v2, err := Open(ctx, filepath.Join(dir, "fresh.db"), Options{Keyring: platformfake.NewKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v2.Close() }()
	if err := v2.Unlock(ctx, nil); err != nil {
		t.Fatal(err)
	}
	pp2 := []byte("backup-passphrase-001")
	n, err := v2.Import(ctx, data, pp2)
	ZeroBytes(pp2)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if n != 2 {
		t.Fatalf("imported %d entries, want 2", n)
	}
	got, err := v2.Describe(ctx, "github-token")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeAPIKey || got.Meta["service"] != "github" || len(got.Scopes) == 0 || got.AllowedTools[0] != "exec" || got.Domains[0] != "api.github.com" {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Fatalf("expiry mismatch: %+v", got.ExpiresAt)
	}
	res, err := v2.Resolve(ctx, ResolveRequest{Ref: "github-token", Tool: "exec", Project: "proj-a"})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Value) != "ghp_EXPORTTESTVALUE0123456789" {
		t.Fatal("imported value mismatch")
	}
	ZeroBytes(res.Value)
}

// TestExport_LockedFails checks export needs an unlocked vault.
func TestExport_LockedFails(t *testing.T) {
	v, _ := testVaultLocked(t)
	if _, err := v.Export(context.Background(), []byte("x")); err != ErrLocked {
		t.Fatalf("locked export err = %v", err)
	}
}

// TestExport_FileRoundtrip covers vault export/import via files.
func TestExport_FileRoundtrip(t *testing.T) {
	v, dir := testVault(t, nil)
	ctx := context.Background()
	if _, err := v.Set(ctx, SetParams{Name: "f", Type: TypeToken, Value: []byte("file-roundtrip-1")}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "backup.json")
	if err := v.ExportToFile(ctx, path, []byte("file-backup-pp")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup perms = %o", fi.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "file-roundtrip-1") {
		t.Fatal("backup file leaks value")
	}
	v2, err := Open(ctx, filepath.Join(dir, "fresh2.db"), Options{Keyring: platformfake.NewKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v2.Close() }()
	if err := v2.Unlock(ctx, nil); err != nil {
		t.Fatal(err)
	}
	n, err := v2.ImportFromFile(ctx, path, []byte("file-backup-pp"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("imported %d", n)
	}
}
