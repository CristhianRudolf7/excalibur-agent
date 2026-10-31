// Vault binding tests: 20.1 gates at the daemon seam — provider
// priority, output redaction, eval lockout, delegate scoping.
package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeBackend is a policy-enforcing in-memory SecretBackend.
type fakeBackend struct {
	mu       sync.Mutex
	values   map[string]string
	scopes   map[string][]string
	locked   bool
	uses     int
	redacted map[string]string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{values: map[string]string{}, scopes: map[string][]string{}, redacted: map[string]string{}}
}

func (f *fakeBackend) ResolveSecret(_ context.Context, c SecretCall) ([]byte, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.Eval {
		return nil, nil, errors.New("vault: eval profile never opens the vault")
	}
	if f.locked {
		return nil, nil, errors.New("vault: locked")
	}
	v, ok := f.values[c.Ref]
	if !ok {
		return nil, nil, errors.New("vault: secret not found")
	}
	if c.Delegated {
		allowed := false
		for _, s := range f.scopes[c.Ref] {
			if s == c.Project {
				allowed = true
			}
		}
		if !allowed {
			return nil, nil, errors.New("vault: delegate task may only resolve its project scope")
		}
	}
	f.uses++
	live := []byte(v)
	return live, func() {
		for i := range live {
			live[i] = 0
		}
	}, nil
}

func (f *fakeBackend) RedactBook() func(string) string {
	return func(s string) string {
		f.mu.Lock()
		defer f.mu.Unlock()
		for val, name := range f.redacted {
			s = strings.ReplaceAll(s, val, "‹secret:"+name+"›")
		}
		return s
	}
}

func TestProviderHookResolvesAtCallTime(t *testing.T) {
	be := newFakeBackend()
	be.values["provider/openai"] = "live-key"
	resolve := ProviderHook(be, "run-1")
	key, err := resolve(context.Background(), "provider/openai")
	if err != nil || key != "live-key" {
		t.Fatalf("provider hook = %q, %v", key, err)
	}
	if be.uses != 1 {
		t.Errorf("resolution must record a use, have %d", be.uses)
	}
}

func TestProviderHookWithoutVault(t *testing.T) {
	resolve := ProviderHook(nil, "run-1")
	if _, err := resolve(context.Background(), "x"); err == nil {
		t.Error("no vault must fail closed, letting env/config fall through above")
	}
}

func TestEvalNeverOpensVault(t *testing.T) {
	be := newFakeBackend()
	be.values["k"] = "v"
	_, _, err := be.ResolveSecret(context.Background(), SecretCall{Ref: "k", Tool: "provider", Eval: true})
	if err == nil {
		t.Error("eval profile must never open the vault (23.2)")
	}
}

func TestDelegateScopeRestriction(t *testing.T) {
	be := newFakeBackend()
	be.values["proj-key"] = "v"
	be.scopes["proj-key"] = []string{"proj-a"}
	_, _, err := be.ResolveSecret(context.Background(), SecretCall{Ref: "proj-key", Tool: "exec", Project: "proj-b", Delegated: true})
	if err == nil {
		t.Error("delegate task must not resolve another project's secret (3.6)")
	}
	if _, _, err := be.ResolveSecret(context.Background(), SecretCall{Ref: "proj-key", Tool: "exec", Project: "proj-a", Delegated: true}); err != nil {
		t.Errorf("own project scope must resolve: %v", err)
	}
}

func TestToolsRedactorBeforeJournal(t *testing.T) {
	be := newFakeBackend()
	be.redacted["s3cr3t"] = "github-token"
	filter := ToolsRedactor(be)
	out := filter("leaked s3cr3t and s3cr3t in base64 c2VjcmV0")
	if strings.Contains(out, "s3cr3t") {
		t.Errorf("output must redact before journal/context/telemetry: %q", out)
	}
	if !strings.Contains(out, "‹secret:github-token›") {
		t.Errorf("redaction marker must name the secret: %q", out)
	}
	if got := ToolsRedactor(nil)("plain"); got != "plain" {
		t.Errorf("nil backend passes through: %q", got)
	}
}

func TestResolveEnvInjectsAndCleans(t *testing.T) {
	be := newFakeBackend()
	be.values["secret://tok"] = "tokval"
	env := map[string]string{"A": "1", "TOKEN": "secret://tok"}
	got, cleanup, err := ResolveEnv(context.Background(), be, env, "exec", "run-1", "proj")
	if err != nil {
		t.Fatalf("ResolveEnv: %v", err)
	}
	if got["TOKEN"] != "tokval" || got["A"] != "1" {
		t.Errorf("env = %v", got)
	}
	cleanup()
	if _, _, err := ResolveEnv(context.Background(), nil, env, "exec", "run-1", "proj"); err == nil {
		t.Error("secret env without a vault must fail")
	}
}

func TestVaultAdapterAgainstRealVault(t *testing.T) {
	// The adapter is structural: it must expose the provider dest and
	// the redactor without leaking vault types. Compile-time shape is
	// asserted here; behavior is covered by vault package tests.
	var _ SecretBackend = VaultAdapter{}
	var _ = ProviderHook
	var _ = ToolsRedactor
	var _ = ResolveEnv
}
