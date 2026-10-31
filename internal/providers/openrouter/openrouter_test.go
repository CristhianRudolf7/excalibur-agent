package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func server(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth = %q", got)
		}
		if got := r.Header.Get("X-Title"); got != "Excalibur" {
			t.Errorf("title = %q", got)
		}
		if got := r.Header.Get("HTTP-Referer"); got != "https://example.test/app" {
			t.Errorf("referer = %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		handler(w, r, body)
	}))
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func newAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	t.Setenv(envAPIKey, "")
	a, err := New(Options{APIKey: "test-key", BaseURL: srv.URL, AppURL: "https://example.test/app"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func userReq(model string) providers.ChatRequest {
	return providers.ChatRequest{
		Model:    model,
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
	}
}

func TestName(t *testing.T) {
	a, err := New(Options{APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	if a.Name() != "openrouter" {
		t.Fatalf("name = %q", a.Name())
	}
}

func TestMissingKeyIsAuth(t *testing.T) {
	t.Setenv(envAPIKey, "")
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	a, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	_, err = a.Chat(context.Background(), userReq("m"))
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || perr.Kind != providers.KindAuth {
		t.Fatalf("err = %v, want auth", err)
	}
	if hit {
		t.Fatalf("server hit without credentials")
	}
}

func TestChatReasoningObjectAndUsage(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok || reasoning["effort"] != "low" {
			t.Errorf("reasoning = %v", body["reasoning"])
		}
		if _, ok := body["reasoning_effort"]; ok {
			t.Errorf("must not send reasoning_effort")
		}
		writeJSON(t, w, map[string]any{
			"model": "vendor/model",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     800,
				"completion_tokens": 9,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 100,
				},
			},
		})
	})
	defer srv.Close()
	a := newAdapter(t, srv)
	resp, err := a.Chat(context.Background(), providers.ChatRequest{
		Model:           "vendor/model",
		Messages:        []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	want := providers.Usage{Input: 800, CacheRead: 100, Output: 9}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestStreamReassemblesFragmentedToolCall(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range []string{
			`{"choices":[{"delta":{"content":"Working"},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"search","arguments":"{\"q"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"x\"}"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":300,"completion_tokens":15}}`,
			`[DONE]`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
	})
	defer srv.Close()
	a := newAdapter(t, srv)
	var events []providers.StreamEvent
	if err := a.Stream(context.Background(), userReq("vendor/model"), func(ev providers.StreamEvent) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %+v", events)
	}
	tc := events[1].ToolCallDelta
	if tc == nil || tc.Name != "search" || tc.Arguments != `{"q":"x"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if events[2].Usage == nil || *events[2].Usage != (providers.Usage{Input: 300, Output: 15}) {
		t.Fatalf("usage = %+v", events[2])
	}
	if !events[3].Done {
		t.Fatalf("last = %+v, want done", events[3])
	}
}

func TestErrorClasses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		kind   providers.ErrorKind
	}{
		{"transient", 502, `{"error":{"message":"upstream"}}`, providers.KindTransient},
		{"rate limited", 429, `{"error":{"message":"slow","code":429}}`, providers.KindRateLimited},
		{"auth", 401, `{"error":{"message":"bad key"}}`, providers.KindAuth},
		{"quota", 402, `{"error":{"message":"credits exhausted"}}`, providers.KindAuth},
		{"input", 404, `{"error":{"message":"no such model"}}`, providers.KindInput},
		{"content", 400, `{"error":{"message":"blocked","code":"content_filter"}}`, providers.KindContentRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := server(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			defer srv.Close()
			a := newAdapter(t, srv)
			_, err := a.Chat(context.Background(), userReq("m"))
			var perr *providers.ProviderError
			if !errors.As(err, &perr) || perr.Kind != tc.kind {
				t.Fatalf("chat err = %v, want %s", err, tc.kind)
			}
			err = a.Stream(context.Background(), userReq("m"), func(providers.StreamEvent) error { return nil })
			if !errors.As(err, &perr) || perr.Kind != tc.kind {
				t.Fatalf("stream err = %v, want %s", err, tc.kind)
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		<-r.Context().Done()
	})
	defer srv.Close()
	a := newAdapter(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Chat(ctx, userReq("m")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
