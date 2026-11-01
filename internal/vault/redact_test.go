package vault

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

// TestRedact_EnvBase64URL covers 20.2 row 3: a command printing a
// resolved env variable, in raw, base64 and URL forms, reaches context
// and journal as ‹secret:name› in all three cases.
func TestRedact_EnvBase64URL(t *testing.T) {
	v, _ := testVault(t, nil)
	ctx := context.Background()
	value := "s3cr3t-value/with+special chars&more"
	if _, err := v.Set(ctx, SetParams{Name: "github-token", Type: TypeAPIKey, Value: []byte(value)}); err != nil {
		t.Fatal(err)
	}
	res, err := v.Resolve(ctx, ResolveRequest{Ref: "secret://github-token", Tool: "exec", RunID: "run_1"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(res.Value)
	ZeroBytes(res.Value)
	if got != value {
		t.Fatal("resolve mismatch")
	}
	// Simulate tool outputs in the three encodings.
	outputs := []string{
		"token is " + value + " done",
		"token is " + base64.StdEncoding.EncodeToString([]byte(value)) + " done",
		"token is " + url.QueryEscape(value) + " done",
	}
	for i, out := range outputs {
		red := v.Redact(out)
		if strings.Contains(red, value) {
			t.Fatalf("case %d: raw value leaked: %q", i, red)
		}
		if !strings.Contains(red, "‹secret:github-token›") {
			t.Fatalf("case %d: missing marker: %q", i, red)
		}
	}
}

// TestRedact_PatternWithoutVault covers pattern redaction of a token
// format that is not stored in the vault.
func TestRedact_PatternWithoutVault(t *testing.T) {
	v, _ := testVault(t, nil)
	out := "leaked ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123 in log"
	red := v.Redact(out)
	if strings.Contains(red, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123") {
		t.Fatalf("pattern token leaked: %q", red)
	}
	if !strings.Contains(red, RedactPatternMarker) {
		t.Fatalf("missing pattern marker: %q", red)
	}
	priv := "key:\n-----BEGIN OPENSSH PRIVATE KEY-----\nZmFrZXJpZ2h0\n-----END OPENSSH PRIVATE KEY-----\nend"
	if red := v.Redact(priv); strings.Contains(red, "ZmFrZXJpZ2h0") {
		t.Fatalf("private key leaked: %q", red)
	}
}

// TestRedact_RotateRebuilds checks redaction follows rotation.
func TestRedact_RotateRebuilds(t *testing.T) {
	v, _ := testVault(t, nil)
	ctx := context.Background()
	if _, err := v.Set(ctx, SetParams{Name: "api", Type: TypeAPIKey, Value: []byte("rotate-redact-value-1")}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Rotate(ctx); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if red := v.Redact("x rotate-redact-value-1 y"); strings.Contains(red, "rotate-redact-value-1") {
		t.Fatalf("post-rotate leak: %q", red)
	}
}

// TestRedact_LockedKeepsPatterns checks a locked vault still redacts
// known patterns (values unavailable).
func TestRedact_LockedKeepsPatterns(t *testing.T) {
	v, dir := testVaultLocked(t)
	_ = dir
	v.Lock()
	red := v.Redact("has AKIAIOSFODNN7EXAMPLE inside")
	if strings.Contains(red, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("pattern leaked while locked: %q", red)
	}
}

// TestContainsRedactionMarker covers the write-side guard predicate: a
// vault value marker and the pattern marker are detected, plain text is
// not.
func TestContainsRedactionMarker(t *testing.T) {
	r := NewRedactor()
	if !ContainsRedactionMarker("key=" + r.MarkerFor("github-token")) {
		t.Fatal("vault value marker must be detected")
	}
	if !ContainsRedactionMarker("token: " + RedactPatternMarker) {
		t.Fatal("pattern marker must be detected")
	}
	for _, plain := range []string{"", "ordinary text", "sk-redacted-by-name", "secret: value"} {
		if ContainsRedactionMarker(plain) {
			t.Fatalf("plain text %q must not be treated as a marker", plain)
		}
	}
}
