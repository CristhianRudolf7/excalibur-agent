package registry

import (
	"context"
	"errors"
	"os"
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

// TestBuiltinKindsAll covers the Section 19.2 adapter table: every
// documented provider kind builds through the registry.
func TestBuiltinKindsAll(t *testing.T) {
	for _, kind := range []string{KindOpenAI, KindDeepSeek, KindOpenRouter, KindOllama, KindGeneric} {
		p, err := New(context.Background(), ProviderDef{Kind: kind, Model: "m", BaseURL: "http://127.0.0.1:9", APIKey: "k"}, nil)
		if err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
		if p.Name() != kind {
			t.Fatalf("kind %q built %q", kind, p.Name())
		}
		_ = p.Close()
	}
}

// TestOperatorKeyAlias covers the deployment-wide EXCALIBUR_API_KEY
// alias: kind-specific variables win, the alias beats config.
func TestOperatorKeyAlias(t *testing.T) {
	ctx := context.Background()
	t.Setenv("OPENAI_API_KEY", "kind-key")
	t.Setenv("EXCALIBUR_API_KEY", "alias-key")
	key, src, err := ResolveKey(ctx, ProviderDef{Kind: KindOpenAI, APIKey: "cfg"}, nil)
	if err != nil || key != "kind-key" {
		t.Fatalf("kind env must win: key=%q src=%q err=%v", key, src, err)
	}
	t.Setenv("OPENAI_API_KEY", "")
	key, src, err = ResolveKey(ctx, ProviderDef{Kind: KindOpenAI, APIKey: "cfg"}, nil)
	if err != nil || key != "alias-key" || src != "env:EXCALIBUR_API_KEY" {
		t.Fatalf("alias must beat config: key=%q src=%q err=%v", key, src, err)
	}
}

// TestFallbackKeyResolution covers the Section 19.5 fallback
// credential order: vault, then environment, then config.
func TestFallbackKeyResolution(t *testing.T) {
	ctx := context.Background()
	t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "fb-env")
	key, src, err := ResolveFallbackKey(ctx, ProviderDef{FallbackKey: "fb-cfg"}, nil)
	if err != nil || key != "fb-env" || src != "env:EXCALIBUR_FALLBACK_API_KEY" {
		t.Fatalf("env must beat config: key=%q src=%q err=%v", key, src, err)
	}
	t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "")
	key, src, err = ResolveFallbackKey(ctx, ProviderDef{FallbackKey: "fb-cfg"}, nil)
	if err != nil || key != "fb-cfg" || src != "config" {
		t.Fatalf("config fallback: key=%q src=%q err=%v", key, src, err)
	}
	vault := func(context.Context, string) (string, error) { return "fb-vault", nil }
	key, src, err = ResolveFallbackKey(ctx, ProviderDef{FallbackKeySecret: "s", FallbackKey: "fb-cfg"}, vault)
	if err != nil || key != "fb-vault" || src != "vault" {
		t.Fatalf("vault must win: key=%q src=%q err=%v", key, src, err)
	}
}

// TestFallbackUsedOnlyOnQuotaLike covers the Section 19.5 rule: the
// fallback key serves exactly one retry on transient/rate-limited
// failures and never sees auth, input or content errors.
func TestFallbackUsedOnlyOnQuotaLike(t *testing.T) {
	rateLimited := &providers.ProviderError{Kind: providers.KindRateLimited, Err: errors.New("slow")}
	auth := &providers.ProviderError{Kind: providers.KindAuth, Err: errors.New("denied")}
	cases := []struct {
		name      string
		primary   error
		wantCalls int
		wantErr   error
	}{
		{"rate-limited falls back", rateLimited, 2, nil},
		{"transient falls back", &providers.ProviderError{Kind: providers.KindTransient, Err: errors.New("boom")}, 2, nil},
		{"auth never falls back", auth, 1, auth},
		{"input never falls back", &providers.ProviderError{Kind: providers.KindInput, Err: errors.New("bad")}, 1, nil},
		{"content never falls back", &providers.ProviderError{Kind: providers.KindContentRejected, Err: errors.New("no")}, 1, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			primary := &stubProvider{name: "p", chat: func(providers.ChatRequest) (providers.ChatResponse, error) {
				calls++
				return providers.ChatResponse{}, tc.primary
			}}
			fallback := &stubProvider{name: "p", chat: func(providers.ChatRequest) (providers.ChatResponse, error) {
				calls++
				return providers.ChatResponse{Message: providers.Message{Content: "ok"}}, nil
			}}
			got, err := withFallback(primary, fallback).Chat(context.Background(), providers.ChatRequest{})
			if calls != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tc.wantCalls)
			}
			if tc.wantErr != nil {
				if err != tc.wantErr {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if tc.primary != nil && tc.wantCalls == 1 {
				if err == nil {
					t.Fatal("primary error must propagate without fallback")
				}
				return
			}
			if err != nil || got.Message.Content != "ok" {
				t.Fatalf("fallback must serve: resp=%+v err=%v", got, err)
			}
		})
	}
}

// TestFallbackStreamNoDuplicate covers the streaming guard: a
// mid-stream failure never replays through the fallback key.
func TestFallbackStreamNoDuplicate(t *testing.T) {
	primary := &stubProvider{name: "p"}
	_ = primary
	calls := 0
	p := &streamStub{name: "p", failAfter: 1, err: &providers.ProviderError{Kind: providers.KindRateLimited, Err: errors.New("slow")}, calls: &calls}
	fb := &streamStub{name: "p", failAfter: -1, calls: &calls}
	if err := withFallback(p, fb).Stream(context.Background(), providers.ChatRequest{}, func(providers.StreamEvent) error { return nil }); err == nil {
		t.Fatal("mid-stream failure must propagate")
	}
	if calls != 1 {
		t.Fatalf("fallback must not replay a started stream: calls = %d", calls)
	}
}

type streamStub struct {
	name      string
	failAfter int
	err       error
	calls     *int
}

func (s *streamStub) Name() string { return s.name }

func (s *streamStub) Chat(context.Context, providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (s *streamStub) Stream(_ context.Context, _ providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	*s.calls++
	if s.failAfter >= 0 {
		for i := 0; i <= s.failAfter; i++ {
			if err := emit(providers.StreamEvent{Delta: "x"}); err != nil {
				return err
			}
		}
		return s.err
	}
	return nil
}

func (s *streamStub) Close() error { return nil }

// TestNewWrapsFallback covers New: a distinct fallback credential
// wraps the primary transparently under the same provider name.
func TestNewWrapsFallback(t *testing.T) {
	t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "fb-key")
	p, err := New(context.Background(), ProviderDef{Kind: KindAnthropic, Model: "m", APIKey: "primary"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.Name() != KindAnthropic {
		t.Fatalf("wrapped name = %q", p.Name())
	}
	if _, ok := p.(*fallbackProvider); !ok {
		t.Fatal("distinct fallback key must wrap the primary")
	}
	t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "")
	same, err := New(context.Background(), ProviderDef{Kind: KindAnthropic, Model: "m", APIKey: "same", FallbackKey: "same"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer same.Close()
	if _, ok := same.(*fallbackProvider); ok {
		t.Fatal("identical fallback key must not wrap")
	}
}

// TestLoadOperatorCredentials covers the credentials-file layer:
// explicit paths decode, missing files are an empty layer, and
// malformed JSON fails.
func TestLoadOperatorCredentials(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/openai.json"
	raw := `{"base_url":"https://api.openai.com/v1","api_key":"k1","fallback_api_key":"k2",` +
		`"model":"gpt-6-luna","reasoning_effort":"low","embedding_model":"text-embedding-3-small",` +
		`"decisions_model":"gpt-6-luna","unknown_future":"ignored"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadOperatorCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if creds.APIKey != "k1" || creds.FallbackAPIKey != "k2" || creds.Model != "gpt-6-luna" ||
		creds.ReasoningEffort != "low" || creds.EmbeddingModel != "text-embedding-3-small" ||
		creds.DecisionsModel != "gpt-6-luna" || creds.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("decoded = %+v", creds)
	}
	empty, err := LoadOperatorCredentials(dir + "/missing.json")
	if err != nil || empty.APIKey != "" {
		t.Fatalf("missing file must be an empty layer: %+v %v", empty, err)
	}
	bad := dir + "/bad.json"
	if err := os.WriteFile(bad, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOperatorCredentials(bad); err == nil {
		t.Fatal("malformed JSON must fail")
	}
	filled := OperatorCredentials{}.FillDefaults()
	if filled.ReasoningEffort != "low" || filled.EmbeddingModel != "text-embedding-3-small" || filled.DecisionsModel != "gpt-6-luna" {
		t.Fatalf("defaults = %+v", filled)
	}
}

type stubProvider struct {
	name string
	chat func(providers.ChatRequest) (providers.ChatResponse, error)
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if s.chat != nil {
		return s.chat(req)
	}
	return providers.ChatResponse{}, nil
}

func (s *stubProvider) Stream(context.Context, providers.ChatRequest, func(providers.StreamEvent) error) error {
	return nil
}

func (s *stubProvider) Close() error { return nil }
