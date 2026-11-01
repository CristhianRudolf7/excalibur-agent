package models

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestExhaustedCredentialRecovery(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		primaryQuota, all, disabled bool
		want                        []string
	}{
		{name: "fallback exhausted", want: []string{"Bearer primary", "Bearer secondary", "Bearer primary", "Bearer primary", "Bearer primary"}},
		{name: "primary exhausted", primaryQuota: true, want: []string{"Bearer primary", "Bearer secondary", "Bearer secondary"}},
		{name: "both exhausted", primaryQuota: true, all: true, want: []string{"Bearer primary", "Bearer secondary"}},
		{name: "switch off", disabled: true, want: []string{"Bearer primary", "Bearer secondary", "Bearer primary", "Bearer secondary", "Bearer primary"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("EXCALIBUR_BUDGET_RETRIES", "1")
			t.Setenv("EXCALIBUR_EXHAUSTED_KEYS", "1")
			if tc.disabled {
				t.Setenv("EXCALIBUR_EXHAUSTED_KEYS", "0")
			}
			t.Setenv("EXCALIBUR_API_KEY", "")
			t.Setenv("OPENAI_API_KEY", "")
			t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "")
			var mu sync.Mutex
			var keys []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				key := r.Header.Get("Authorization")
				keys = append(keys, key)
				quota := tc.all || (key == "Bearer primary" && tc.primaryQuota) || (key == "Bearer secondary" && !tc.primaryQuota)
				if quota {
					w.Header().Set("Retry-After", "0.001")
					w.WriteHeader(429)
					// Code-only exhaustion must survive normalization dropping the code.
					fmt.Fprint(w, `{"error":{"code":"insufficient_quota","message":"account unavailable"}}`)
				} else if !tc.primaryQuota && len(keys) <= 3 {
					w.Header().Set("Retry-After", "0.001")
					w.WriteHeader(429)
					fmt.Fprint(w, `{"error":{"code":"rate_limit_exceeded","message":"slow"}}`)
				} else {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"id":"r","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`)
				}
			}))
			defer srv.Close()
			def := registry.ProviderDef{Kind: "openai", APIKey: "primary", FallbackKey: "secondary", BaseURL: srv.URL}
			call := func() {
				t.Helper()
				p, err := NewProvider(context.Background(), def, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer p.Close()
				g := NewGateway(GatewayConfig{Roles: map[types.ModelRole]RoleEntry{types.RoleExecutor: {Primary: ModelRef{Provider: "openai", Model: "m"}}}, BaseDelay: time.Millisecond})
				g.Register(p)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_, _, err = g.Call(ctx, types.RoleExecutor, providers.ChatRequest{})
				if tc.all {
					if !errors.Is(err, ErrProviderQuotaExhausted) {
						t.Fatalf("want exhausted, got %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			}
			call()
			if !tc.disabled {
				call()
			} // Newly built provider must retain process quarantine.
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(keys, tc.want) {
				t.Fatalf("credential selection %v; want %v", keys, tc.want)
			}
		})
	}
}

func TestPermanentQuotaMarkers(t *testing.T) {
	for _, text := range []string{"insufficient_quota", "billing_hard_limit_reached", "billing_not_active", "You have no credits remaining", "Your credit balance is too low", "Payment required"} {
		if !permanentQuota(text) {
			t.Errorf("missed %q", text)
		}
	}
	for _, text := range []string{"rate_limit_exceeded", "tokens per minute exceeded", "requests quota resets in 1s", "invalid api key"} {
		if permanentQuota(text) {
			t.Errorf("false exhaustion %q", text)
		}
	}
}

func TestQuotaStreamQuarantine(t *testing.T) {
	t.Setenv("EXCALIBUR_API_KEY", "")
	t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "")
	t.Setenv("EXCALIBUR_EXHAUSTED_KEYS", "1")
	var mu sync.Mutex
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		keys = append(keys, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer primary" {
			w.WriteHeader(402)
			fmt.Fprint(w, `{"error":{"message":"Payment required"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"c\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	p, err := NewProvider(context.Background(), registry.ProviderDef{Kind: "generic", APIKey: "primary", FallbackKey: "secondary", BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for i := 0; i < 2; i++ {
		if err := p.Stream(context.Background(), providers.ChatRequest{Model: "m"}, func(providers.StreamEvent) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(keys, []string{"Bearer primary", "Bearer secondary", "Bearer secondary"}) {
		t.Fatalf("stream keys %v", keys)
	}
}

func TestCredentialQuarantineLogsOnce(t *testing.T) {
	t.Setenv("EXCALIBUR_EXHAUSTED_KEYS", "1")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	id := credentialID(t.Name(), "test-credential-must-not-be-logged")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		safego.Go("quota-test", nil, func() { defer wg.Done(); exhaustCredential(id) })
	}
	wg.Wait()
	if !credentialExhausted(id) {
		t.Fatal("credential not quarantined")
	}
	if strings.Count(logs.String(), "provider credential exhausted;") != 1 {
		t.Fatalf("warnings: %s", logs.String())
	}
	if strings.Contains(logs.String(), "test-credential-must-not-be-logged") {
		t.Fatal("credential leaked")
	}
}
