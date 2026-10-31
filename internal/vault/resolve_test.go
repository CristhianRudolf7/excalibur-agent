package vault

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestResolve_Policy covers 20.2 row 5: secret://banco-web with exec
// and with the browser on a wrong domain is rejected with a clear
// error; on the right domain the form fills. Uses are recorded.
func TestResolve_Policy(t *testing.T) {
	v, _ := testVault(t, nil)
	ctx := context.Background()
	if _, err := v.Set(ctx, SetParams{
		Name: "banco-web", Type: TypeWebsitePassword, Value: []byte("BancoS3cret!"),
		Meta:         map[string]string{"url": "https://banco.example.com", "user": "ana"},
		Scopes:       []string{"proj-web"},
		AllowedTools: []string{"browser"},
		Domains:      []string{"banco.example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	// exec is not an allowed tool.
	_, err := v.Resolve(ctx, ResolveRequest{Ref: "secret://banco-web", Tool: "exec", RunID: "run_1", Project: "proj-web"})
	if err == nil || !strings.Contains(err.Error(), "not allowed for") {
		t.Fatalf("exec resolve err = %v, want denial", err)
	}
	// Browser on a wrong domain is rejected.
	_, err = v.Resolve(ctx, ResolveRequest{Ref: "secret://banco-web", Tool: "browser", RunID: "run_1", Project: "proj-web", Domain: "evil.example.com", Dest: DestForm})
	if err == nil || !strings.Contains(err.Error(), "not allowed for domain") {
		t.Fatalf("wrong-domain resolve err = %v, want denial", err)
	}
	// Right domain fills.
	res, err := v.Resolve(ctx, ResolveRequest{Ref: "secret://banco-web", Tool: "browser", RunID: "run_1", Project: "proj-web", Domain: "https://banco.example.com/login", Dest: DestForm})
	if err != nil {
		t.Fatalf("right-domain resolve: %v", err)
	}
	if string(res.Value) != "BancoS3cret!" {
		t.Fatal("form value mismatch")
	}
	ZeroBytes(res.Value)
	// Uses recorded, values not.
	uses, err := v.Usage(ctx, "banco-web", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) != 1 || uses[0].Tool != "browser" || uses[0].Dest != string(DestForm) {
		t.Fatalf("uses = %+v", uses)
	}
	dump := dumpTables(t, v)
	if strings.Contains(dump, "BancoS3cret!") {
		t.Fatal("secret_uses or tables leak the value")
	}
}

// TestResolve_Scopes checks project scoping for non-delegate callers.
func TestResolve_Scopes(t *testing.T) {
	v, _ := testVault(t, nil)
	ctx := context.Background()
	if _, err := v.Set(ctx, SetParams{Name: "scoped", Type: TypeToken, Value: []byte("scoped-value-1"), Scopes: []string{"proj-a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Resolve(ctx, ResolveRequest{Ref: "scoped", Tool: "exec", Project: "proj-a"}); err != nil {
		t.Fatalf("own project: %v", err)
	}
	if _, err := v.Resolve(ctx, ResolveRequest{Ref: "scoped", Tool: "exec", Project: "proj-b"}); err == nil {
		t.Fatal("other project resolved a scoped secret")
	}
}

// TestResolve_Expiry checks expired entries do not resolve.
func TestResolve_Expiry(t *testing.T) {
	v, _ := testVault(t, nil)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	if _, err := v.Set(ctx, SetParams{Name: "old", Type: TypeToken, Value: []byte("old-value-1"), ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Resolve(ctx, ResolveRequest{Ref: "old", Tool: "exec"}); err == nil {
		t.Fatal("expired secret resolved")
	} else if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("wrong error: %v", err)
	}
}

// TestVault_LockedNeedsUnlock covers 20.2 row 6 (first half): a task
// needing a secret while locked gets the unlock question; after unlock
// it continues.
func TestVault_LockedNeedsUnlock(t *testing.T) {
	v, _ := testVault(t, nil)
	ctx := context.Background()
	if _, err := v.Set(ctx, SetParams{Name: "k", Type: TypeToken, Value: []byte("k-value-1")}); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if !v.IsLocked() {
		t.Fatal("expected locked")
	}
	_, err := v.Resolve(ctx, ResolveRequest{Ref: "k", Tool: "exec", RunID: "run_9"})
	if err == nil {
		t.Fatal("locked resolve succeeded")
	}
	if !strings.Contains(err.Error(), "unlock") {
		t.Fatalf("locked error is not an unlock question: %v", err)
	}
	if err := v.Unlock(ctx, nil); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	res, err := v.Resolve(ctx, ResolveRequest{Ref: "k", Tool: "exec", RunID: "run_9"})
	if err != nil {
		t.Fatalf("post-unlock resolve: %v", err)
	}
	ZeroBytes(res.Value)
}

// TestVault_EvalAndDelegate covers 20.2 row 9: eval never opens the
// vault; delegate tasks only resolve entries scoped to their project.
func TestVault_EvalAndDelegate(t *testing.T) {
	if _, err := Open(context.Background(), t.TempDir()+"/vault.db", Options{EvalProfile: true}); err != ErrEvalBlocked {
		t.Fatalf("eval open err = %v, want ErrEvalBlocked", err)
	}
	v, _ := testVault(t, nil)
	ctx := context.Background()
	if _, err := v.Set(ctx, SetParams{Name: "general-key", Type: TypeToken, Value: []byte("general-value-1")}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Set(ctx, SetParams{Name: "proj-key", Type: TypeToken, Value: []byte("proj-value-2"), Scopes: []string{"proj-x"}}); err != nil {
		t.Fatal(err)
	}
	// Evaluator guard on Resolve.
	if _, err := v.Resolve(ctx, ResolveRequest{Ref: "proj-key", Tool: "exec", Eval: true}); err != ErrEvalBlocked {
		t.Fatalf("eval resolve err = %v", err)
	}
	// Delegate: project-scoped entry resolves...
	if _, err := v.Resolve(ctx, ResolveRequest{Ref: "proj-key", Tool: "exec", Project: "proj-x", Delegated: true}); err != nil {
		t.Fatalf("delegate own scope: %v", err)
	}
	// ...but general entries do NOT resolve for delegates...
	if _, err := v.Resolve(ctx, ResolveRequest{Ref: "general-key", Tool: "exec", Project: "proj-x", Delegated: true}); err == nil {
		t.Fatal("delegate resolved a general secret")
	}
	// ...and other projects do not resolve either.
	if _, err := v.Resolve(ctx, ResolveRequest{Ref: "proj-key", Tool: "exec", Project: "proj-y", Delegated: true}); err == nil {
		t.Fatal("delegate resolved another project's secret")
	}
	// Non-delegate callers still see general entries.
	if _, err := v.Resolve(ctx, ResolveRequest{Ref: "general-key", Tool: "exec", Project: "proj-x"}); err != nil {
		t.Fatalf("non-delegate general: %v", err)
	}
}
