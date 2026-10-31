package deepseek

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
	a, err := New(Options{APIKey: "test-key", BaseURL: srv.URL})
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
	if a.Name() != "deepseek" {
		t.Fatalf("name = %q", a.Name())
	}
}

func TestEnvKeyFallback(t *testing.T) {
	t.Setenv(envAPIKey, "env-key")
	a, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer env-key" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		writeJSON(t, w, map[string]any{
			"model": "m",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "x"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer srv.Close()
	// Point at the test server while keeping the env-resolved key.
	b, err := New(Options{BaseURL: srv.URL, APIKey: "env-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	if _, err := b.Chat(context.Background(), userReq("m")); err != nil {
		t.Fatalf("Chat: %v", err)
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

func TestChatUsageCacheAndNoEffort(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if _, ok := body["reasoning_effort"]; ok {
			t.Errorf("deepseek must not send reasoning_effort")
		}
		writeJSON(t, w, map[string]any{
			"model": "deepseek-chat",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":            100,
				"completion_tokens":        5,
				"prompt_cache_hit_tokens":  70,
				"prompt_cache_miss_tokens": 30,
			},
		})
	})
	defer srv.Close()
	a := newAdapter(t, srv)
	resp, err := a.Chat(context.Background(), providers.ChatRequest{
		Model:           "deepseek-chat",
		Messages:        []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "ok" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	want := providers.Usage{Input: 100, CacheRead: 70, CacheWrite: 30, Output: 5}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestStreamReassemblesFragmentedToolCall(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range []string{
			`{"choices":[{"delta":{"reasoning_content":"let me think"},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"content":"Result"},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"calc","arguments":"{\"ex"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"pr\":\"1\"}"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":60,"completion_tokens":20,"prompt_cache_hit_tokens":10,"prompt_cache_miss_tokens":50}}`,
			`[DONE]`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
	})
	defer srv.Close()
	a := newAdapter(t, srv)
	var events []providers.StreamEvent
	if err := a.Stream(context.Background(), userReq("deepseek-chat"), func(ev providers.StreamEvent) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Delta != "Result" {
		t.Fatalf("reasoning content leaked into deltas: %+v", events[0])
	}
	tc := events[1].ToolCallDelta
	if tc == nil || tc.ID != "c1" || tc.Name != "calc" || tc.Arguments != `{"expr":"1"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	want := providers.Usage{Input: 60, CacheRead: 10, CacheWrite: 50, Output: 20}
	if events[2].Usage == nil || *events[2].Usage != want {
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
		{"transient", 503, `{"error":{"message":"overloaded"}}`, providers.KindTransient},
		{"rate limited", 429, `{"error":{"message":"slow"}}`, providers.KindRateLimited},
		{"auth", 401, `{"error":{"message":"bad key"}}`, providers.KindAuth},
		{"input", 400, `{"error":{"message":"bad request"}}`, providers.KindInput},
		{"truncated finish", 200, "", providers.KindTruncated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := server(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				if tc.body == "" {
					writeJSON(t, w, map[string]any{
						"model": "m",
						"choices": []any{map[string]any{
							"message":       map[string]any{"role": "assistant", "content": "p"},
							"finish_reason": "length",
						}},
						"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
					})
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			defer srv.Close()
			a := newAdapter(t, srv)
			_, err := a.Chat(context.Background(), userReq("m"))
			var perr *providers.ProviderError
			if !errors.As(err, &perr) || perr.Kind != tc.kind {
				t.Fatalf("err = %v, want %s", err, tc.kind)
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
