package models

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestOverloadServerWaits(t *testing.T) {
	for _, tc := range []struct {
		name, header, value, text string
		status                    int
	}{
		{"retry seconds", "Retry-After", "0.01", "slow", 429},
		{"retry date", "Retry-After", time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat), "slow", 429},
		{"reset tokens", "x-ratelimit-reset-tokens", "10ms", "slow", 429},
		{"reset requests", "x-ratelimit-reset-requests", "0.01", "slow", 429},
		{"text ms", "", "", "Please try again in 10ms", 429},
		{"text seconds", "", "", "Please try again in 0.01s", 429},
		{"overload", "Retry-After", "0.01", "overloaded", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var keys []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				keys = append(keys, r.Header.Get("Authorization"))
				if len(keys) <= 4 {
					if tc.header != "" {
						w.Header().Set(tc.header, tc.value)
					}
					w.WriteHeader(tc.status)
					fmt.Fprintf(w, `{"error":{"message":%q}}`, tc.text)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"r","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`)
			}))
			defer srv.Close()
			// Isolate credentials from the operator environment.
			t.Setenv("EXCALIBUR_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "")
			p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: "primary", FallbackKey: "secondary", BaseURL: srv.URL}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			cfg := GatewayConfig{Roles: map[types.ModelRole]RoleEntry{types.RoleExecutor: {Primary: ModelRef{Provider: "openai", Model: "m"}}}, BaseDelay: time.Millisecond}
			g := NewGateway(cfg)
			g.Register(p)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, meta, err := g.Call(ctx, types.RoleExecutor, providers.ChatRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if meta.Attempts != 5 || meta.RetryWaitCount != 4 || meta.RetryWaitSeconds < 0.039 {
				t.Fatalf("meta: %+v", meta)
			}
			if !reflect.DeepEqual(keys, []string{"Bearer primary", "Bearer secondary", "Bearer primary", "Bearer secondary", "Bearer primary"}) {
				t.Fatalf("keys did not alternate")
			}
		})
	}
}

func TestOverloadBudgetAndSwitch(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		p := &scriptedProvider{name: "p", fails: 100, kind: providers.KindRateLimited, retryAft: time.Millisecond}
		cfg := testConfig(p, nil)
		cfg.DisableBudgetRetries = disabled
		g := NewGateway(cfg)
		g.Register(p)
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		_, meta, err := g.Call(ctx, types.RoleExecutor, providers.ChatRequest{})
		cancel()
		if disabled {
			if meta.Attempts != 3 {
				t.Fatalf("bounded: %+v", meta)
			}
		} else {
			if !errors.Is(err, context.DeadlineExceeded) || meta.Attempts <= 3 || meta.RetryWaitCount < 3 {
				t.Fatalf("budget: %+v %v", meta, err)
			}
			if g.BreakerOpen("p") {
				t.Fatal("budget expiration must not trip breaker")
			}
		}
	}
}

func TestRetryDelayBounds(t *testing.T) {
	e := &providers.ProviderError{Kind: providers.KindRateLimited, Err: errors.New("slow")}
	if d := retryDelay(e, time.Second, 1000000); d != 30*time.Second {
		t.Fatalf("cap %v", d)
	}
	e.RetryAfter = 45 * time.Second
	if d := retryDelay(e, time.Second, 1); d < 45*time.Second || d > 45100*time.Millisecond {
		t.Fatalf("server wait %v", d)
	}
	now := time.Now()
	h := http.Header{"X-Ratelimit-Reset-Tokens": {"10ms"}, "X-Ratelimit-Reset-Requests": {"20ms"}}
	if d := serverDelay(h, now); d != 20*time.Millisecond {
		t.Fatalf("latest %v", d)
	}
}

func TestObservedOverloadWithoutStatusInMessage(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 4 {
			w.Header().Set("x-ratelimit-reset-tokens", "1ms")
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"status":"UNAVAILABLE","message":"overloaded"}}`)
			return
		}
		fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()
	t.Setenv("EXCALIBUR_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "")
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "gemini", APIKey: "test", BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	g := NewGateway(GatewayConfig{Roles: map[types.ModelRole]RoleEntry{types.RoleExecutor: {Primary: ModelRef{Provider: "gemini", Model: "m"}}}})
	g.Register(p)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, meta, err := g.Call(ctx, types.RoleExecutor, providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}}})
	if err != nil || meta.Attempts != 5 || meta.RetryWaitCount != 4 {
		t.Fatalf("meta=%+v err=%v", meta, err)
	}
}
