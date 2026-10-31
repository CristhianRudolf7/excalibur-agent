package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func chatServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth = %q", got)
		}
		handler(w, r)
	}))
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func newAdapter(t *testing.T, srv *httptest.Server, mutate func(*Options)) *Adapter {
	t.Helper()
	o := Options{APIKey: "test-key", BaseURL: srv.URL}
	if mutate != nil {
		mutate(&o)
	}
	a, err := New(o)
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
	if a.Name() != "openai" {
		t.Fatalf("name = %q", a.Name())
	}
}

func TestMissingKeyIsAuth(t *testing.T) {
	t.Setenv(envAPIKey, "")
	a, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	// Rebuild against the local server but still without a key.
	b, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()
	_, err = b.Chat(context.Background(), userReq("m"))
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || perr.Kind != providers.KindAuth {
		t.Fatalf("chat err = %v, want auth", err)
	}
	if err := b.Stream(context.Background(), userReq("m"), func(providers.StreamEvent) error { return nil }); !errors.As(err, &perr) || perr.Kind != providers.KindAuth {
		t.Fatalf("stream err = %v, want auth", err)
	}
	if hit {
		t.Fatalf("server hit without credentials")
	}
}

func TestChatTextUsageCache(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		if got["reasoning_effort"] != "low" {
			t.Errorf("reasoning_effort = %v", got["reasoning_effort"])
		}
		if got["max_completion_tokens"] != float64(64) {
			t.Errorf("max_completion_tokens = %v", got["max_completion_tokens"])
		}
		if _, ok := got["max_tokens"]; ok {
			t.Errorf("newer OpenAI models reject max_tokens")
		}
		writeJSON(t, w, map[string]any{
			"model": "gpt-x",
			"choices": []any{map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "hello",
					"tool_calls": []any{map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "read",
							"arguments": `{"path":"a"}`,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
			"usage": map[string]any{
				"prompt_tokens":     1500,
				"completion_tokens": 40,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 1024,
				},
				"completion_tokens_details": map[string]any{
					"reasoning_tokens": 10,
				},
			},
		})
	})
	defer srv.Close()
	a := newAdapter(t, srv, nil)
	resp, err := a.Chat(context.Background(), providers.ChatRequest{
		Model:           "gpt-x",
		Messages:        []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
		MaxTokens:       64,
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "hello" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 1 || resp.Message.ToolCalls[0].Name != "read" {
		t.Fatalf("tool calls = %+v", resp.Message.ToolCalls)
	}
	if string(resp.Message.ToolCalls[0].Arguments) != `{"path":"a"}` {
		t.Fatalf("args = %s", resp.Message.ToolCalls[0].Arguments)
	}
	want := providers.Usage{Input: 1500, CacheRead: 1024, Output: 40, Reasoning: 10}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestStreamReassemblesFragmentedToolCall(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range []string{
			`{"choices":[{"delta":{"content":"Sure"},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_7","function":{"name":"exec","arguments":"{\"cm"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"d\":\"ls\"}"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2000,"completion_tokens":25,"prompt_tokens_details":{"cached_tokens":1500}}}`,
			`[DONE]`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
	})
	defer srv.Close()
	a := newAdapter(t, srv, nil)
	var events []providers.StreamEvent
	if err := a.Stream(context.Background(), userReq("gpt-x"), func(ev providers.StreamEvent) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %+v", events)
	}
	tc := events[1].ToolCallDelta
	if tc == nil || tc.ID != "call_7" || tc.Name != "exec" || tc.Arguments != `{"cmd":"ls"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if events[2].Usage == nil || *events[2].Usage != (providers.Usage{Input: 2000, CacheRead: 1500, Output: 25}) {
		t.Fatalf("usage = %+v", events[2])
	}
	if !events[3].Done {
		t.Fatalf("last = %+v, want done", events[3])
	}
}

func TestErrorClasses(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		retryAfter string
		body       string
		kind       providers.ErrorKind
	}{
		{"transient", 500, "", `{"error":{"message":"boom"}}`, providers.KindTransient},
		{"rate limited", 429, "2", `{"error":{"message":"slow"}}`, providers.KindRateLimited},
		{"auth", 401, "", `{"error":{"message":"bad key"}}`, providers.KindAuth},
		{"input", 400, "", `{"error":{"message":"bad request"}}`, providers.KindInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := chatServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			defer srv.Close()
			a := newAdapter(t, srv, nil)
			_, err := a.Chat(context.Background(), userReq("m"))
			var perr *providers.ProviderError
			if !errors.As(err, &perr) || perr.Kind != tc.kind {
				t.Fatalf("chat err = %v, want %s", err, tc.kind)
			}
			if tc.retryAfter == "2" && perr.RetryAfter != 2*time.Second {
				t.Fatalf("retryAfter = %v", perr.RetryAfter)
			}
			err = a.Stream(context.Background(), userReq("m"), func(providers.StreamEvent) error { return nil })
			if !errors.As(err, &perr) || perr.Kind != tc.kind {
				t.Fatalf("stream err = %v, want %s", err, tc.kind)
			}
		})
	}
}

func TestTruncatedAndRefusal(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"model": "m",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "partial"},
				"finish_reason": "length",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
		})
	})
	defer srv.Close()
	a := newAdapter(t, srv, nil)
	resp, err := a.Chat(context.Background(), userReq("m"))
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || perr.Kind != providers.KindTruncated {
		t.Fatalf("err = %v, want truncated", err)
	}
	if resp.Message.Content != "partial" {
		t.Fatalf("partial content lost: %+v", resp.Message)
	}
}

func TestOrganizationHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("OpenAI-Organization") != "org-1" {
			t.Errorf("org = %q", r.Header.Get("OpenAI-Organization"))
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
	a := newAdapter(t, srv, func(o *Options) { o.Organization = "org-1" })
	if _, err := a.Chat(context.Background(), userReq("m")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestCancellation(t *testing.T) {
	srv := chatServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	defer srv.Close()
	a := newAdapter(t, srv, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Chat(ctx, userReq("m")); !errors.Is(err, context.Canceled) {
		t.Fatalf("chat err = %v", err)
	}
}
