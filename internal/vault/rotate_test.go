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

// TestRotate_CrashSafe covers 20.2 row 7: after set and rotate, every
// entry decrypts; reopening (simulated restart) keeps all entries
// valid in their old or new version, never corrupt.
func TestRotate_CrashSafe(t *testing.T) {
	v, dir := testVault(t, nil)
	ctx := context.Background()
	values := map[string]string{
		"a": "alpha-value-0001",
		"b": "beta-value-0002",
		"c": "gamma-value-0003",
	}
	for n, val := range values {
		if _, err := v.Set(ctx, SetParams{Name: n, Type: TypeToken, Value: []byte(val)}); err != nil {
			t.Fatal(err)
		}
	}
	oldV, newV, err := v.Rotate(ctx)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newV != oldV+1 {
		t.Fatalf("versions %d -> %d", oldV, newV)
	}
	if err := v.VerifyAll(ctx); err != nil {
		t.Fatalf("verify after rotate: %v", err)
	}
	// Simulate kill -9 mid-use: close without Lock and reopen against
	// the same keyring, then verify every entry decrypts.
	kr := v.opts.Keyring
	path := filepath.Join(dir, "vault.db")
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	v2, err := Open(ctx, path, Options{Keyring: kr})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v2.Close() }()
	if err := v2.Unlock(ctx, nil); err != nil {
		t.Fatalf("unlock after restart: %v", err)
	}
	if err := v2.VerifyAll(ctx); err != nil {
		t.Fatalf("verify after restart: %v", err)
	}
	for n, val := range values {
		res, err := v2.Resolve(ctx, ResolveRequest{Ref: n, Tool: "exec"})
		if err != nil {
			t.Fatalf("resolve %s after restart: %v", n, err)
		}
		if string(res.Value) != val {
			t.Fatalf("value changed for %s", n)
		}
		ZeroBytes(res.Value)
	}
}

// TestRotate_PassphraseVault covers rotation for passphrase-origin
// vaults and proves Rotate (keyring path) refuses them.
func TestRotate_PassphraseVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	ctx := context.Background()
	v, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v.Close() }()
	pp := []byte("test-passphrase-rotate-001")
	if err := v.Unlock(ctx, pp); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	ZeroBytes(pp)
	if kind := v.KEKKind(); kind != kekPassphrase {
		t.Fatalf("kek kind = %q", kind)
	}
	if _, err := v.Set(ctx, SetParams{Name: "p", Type: TypeToken, Value: []byte("p-value-1")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Rotate(ctx); err == nil {
		t.Fatal("keyring Rotate on a passphrase vault must fail")
	}
	pp2 := []byte("test-passphrase-rotate-001")
	oldV, newV, err := v.RotateWithPassphrase(ctx, pp2)
	ZeroBytes(pp2)
	if err != nil {
		t.Fatalf("rotate with passphrase: %v", err)
	}
	if newV != oldV+1 {
		t.Fatalf("versions %d -> %d", oldV, newV)
	}
	if err := v.VerifyAll(ctx); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// Wrong passphrase on a fresh handle fails.
	v3, err := Open(ctx, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v3.Close() }()
	if err := v3.Unlock(ctx, []byte("wrong-passphrase")); err == nil {
		t.Fatal("wrong passphrase unlocked")
	}
}

// TestVault_AutoLock locks after the idle timeout.
func TestVault_AutoLock(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	v, err := Open(ctx, filepath.Join(dir, "vault.db"), Options{IdleTimeout: 50 * time.Millisecond, IdleTimeoutSet: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v.Close() }()
	v.watchTick = 10 * time.Millisecond
	if err := v.Unlock(ctx, []byte("autolock-pass-001")); err != nil {
		t.Fatal(err)
	}
	if v.IsLocked() {
		t.Fatal("should start unlocked")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !v.IsLocked() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !v.IsLocked() {
		t.Fatal("vault did not auto-lock after idle timeout")
	}
}

// TestVault_PassphraseFile checks 0600 enforcement on the passphrase file.
func TestVault_PassphraseFile(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	good := filepath.Join(dir, "pp")
	if err := os.WriteFile(good, []byte("file-pass-001\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := Open(ctx, filepath.Join(dir, "vault.db"), Options{PassphraseFile: good})
	if err != nil {
		t.Fatalf("open with passphrase file: %v", err)
	}
	defer func() { _ = v.Close() }()
	if v.IsLocked() {
		t.Fatal("passphrase file should unlock at open")
	}
	if kind := v.KEKKind(); kind != kekPassphrase {
		t.Fatalf("kek kind = %q", kind)
	}
	loose := filepath.Join(dir, "loose")
	if err := os.WriteFile(loose, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, filepath.Join(dir, "v2.db"), Options{PassphraseFile: loose}); err == nil {
		t.Fatal("0644 passphrase file must be rejected")
	} else if !strings.Contains(err.Error(), "0600") {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestVault_SecretServicePreferred checks the keyring path stores no
// salt and survives restarts via the keyring.
func TestVault_SecretServicePreferred(t *testing.T) {
	kr := platformfake.NewKeyring()
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.db")
	ctx := context.Background()
	v, err := Open(ctx, path, Options{Keyring: kr})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if kind := v.KEKKind(); kind != kekSecretService {
		t.Fatalf("kek kind = %q", kind)
	}
	if _, err := v.Set(ctx, SetParams{Name: "s", Type: TypeToken, Value: []byte("s-value-1")}); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	v2, err := Open(ctx, path, Options{Keyring: kr})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v2.Close() }()
	if err := v2.Unlock(ctx, nil); err != nil {
		t.Fatalf("re-unlock via keyring: %v", err)
	}
	res, err := v2.Resolve(ctx, ResolveRequest{Ref: "s", Tool: "exec"})
	if err != nil {
		t.Fatal(err)
	}
	ZeroBytes(res.Value)
}
