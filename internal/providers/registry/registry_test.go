package registry

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func TestBuiltinKinds(t *testing.T) {
	kinds := Kinds()
	if !slices.Contains(kinds, KindAnthropic) || !slices.Contains(kinds, KindGemini) {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestRegisterValidation(t *testing.T) {
	if err := Register("", nil); err == nil {
		t.Fatal("blank kind must fail")
	}
	if err := Register("x", nil); err == nil {
		t.Fatal("nil constructor must fail")
	}
}

func TestRegisterCustomKind(t *testing.T) {
	fake := &stubProvider{name: "custom"}
	if err := Register("custom-test", func(ResolvedDef) (providers.Provider, error) {
		return fake, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	p, err := New(context.Background(), ProviderDef{Kind: "CUSTOM-TEST"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.Name() != "custom" {
		t.Fatalf("name = %s", p.Name())
	}
}

func TestKeyResolutionOrder(t *testing.T) {
	ctx := context.Background()
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	vault := func(context.Context, string) (string, error) { return "vault-key", nil }

	key, src, err := ResolveKey(ctx, ProviderDef{Kind: KindAnthropic, APIKeySecret: "s", APIKey: "cfg"}, vault)
	if err != nil || key != "vault-key" || src != "vault" {
		t.Fatalf("vault must win: key=%q src=%q err=%v", key, src, err)
	}
	key, src, err = ResolveKey(ctx, ProviderDef{Kind: KindAnthropic, APIKey: "cfg"}, nil)
	if err != nil || key != "env-key" || src != "env:ANTHROPIC_API_KEY" {
		t.Fatalf("env must beat config: key=%q src=%q err=%v", key, src, err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")
	key, src, err = ResolveKey(ctx, ProviderDef{Kind: KindAnthropic, APIKey: "cfg"}, nil)
	if err != nil || key != "cfg" || src != "config" {
		t.Fatalf("config fallback: key=%q src=%q err=%v", key, src, err)
	}
	key, src, err = ResolveKey(ctx, ProviderDef{Kind: KindAnthropic}, nil)
	if err != nil || key != "" || src != "" {
		t.Fatalf("missing key: key=%q src=%q err=%v", key, src, err)
	}
}

func TestKeyResolutionCustomEnv(t *testing.T) {
	t.Setenv("MY_KEY", "mine")
	key, src, err := ResolveKey(context.Background(), ProviderDef{Kind: KindGemini, APIKeyEnv: "MY_KEY"}, nil)
	if err != nil || key != "mine" || src != "env:MY_KEY" {
		t.Fatalf("key=%q src=%q err=%v", key, src, err)
	}
}

func TestKeyResolutionVaultError(t *testing.T) {
	fail := func(context.Context, string) (string, error) { return "", errors.New("locked") }
	_, _, err := ResolveKey(context.Background(), ProviderDef{Kind: KindAnthropic, APIKeySecret: "s"}, fail)
	if err == nil || !strings.Contains(err.Error(), `"s"`) {
		t.Fatalf("vault errors must propagate with the secret name: %v", err)
	}
}

func TestGeminiEnvFallback(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "google-key")
	key, src, err := ResolveKey(context.Background(), ProviderDef{Kind: KindGemini}, nil)
	if err != nil || key != "google-key" || src != "env:GOOGLE_API_KEY" {
		t.Fatalf("key=%q src=%q err=%v", key, src, err)
	}
}

func TestNewBuildsAdapters(t *testing.T) {
	ctx := context.Background()
	a, err := New(ctx, ProviderDef{Kind: "anthropic", Model: "m", APIKey: "k"}, nil)
	if err != nil || a.Name() != KindAnthropic {
		t.Fatalf("anthropic: %v", err)
	}
	g, err := New(ctx, ProviderDef{Kind: "gemini", Model: "m", APIKey: "k"}, nil)
	if err != nil || g.Name() != KindGemini {
		t.Fatalf("gemini: %v", err)
	}
	if _, err := New(ctx, ProviderDef{Kind: "nope"}, nil); err == nil {
		t.Fatal("unknown kind must fail")
	}
	if _, err := New(ctx, ProviderDef{}, nil); err == nil {
		t.Fatal("blank kind must fail")
	}
}

func TestNewMany(t *testing.T) {
	ctx := context.Background()
	ps, err := NewMany(ctx, []ProviderDef{
		{Kind: KindAnthropic, APIKey: "k"},
		{Kind: KindGemini, APIKey: "k"},
	}, nil)
	if err != nil || len(ps) != 2 {
		t.Fatalf("NewMany: %v", err)
	}
	if _, err := NewMany(ctx, []ProviderDef{{Kind: "nope"}}, nil); err == nil {
		t.Fatal("batch with unknown kind must fail")
	}
}

type stubProvider struct{ name string }

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (s *stubProvider) Stream(context.Context, providers.ChatRequest, func(providers.StreamEvent) error) error {
	return nil
}

func (s *stubProvider) Close() error { return nil }
