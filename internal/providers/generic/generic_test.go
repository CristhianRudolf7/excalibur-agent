package generic

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

func server(t *testing.T, key string, handler func(w http.ResponseWriter, r *http.Request, body map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		want := ""
		if key != "" {
			want = "Bearer " + key
		}
		if got := r.Header.Get("Authorization"); got != want {
			t.Errorf("auth = %q, want %q", got, want)
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

func userReq(model string) providers.ChatRequest {
	return providers.ChatRequest{
		Model:    model,
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
	}
}

func TestName(t *testing.T) {
	a, err := New(Options{BaseURL: "http://localhost:1/v1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	if a.Name() != "generic" {
		t.Fatalf("name = %q", a.Name())
	}
}

func TestBaseURLRequired(t *testing.T) {
	_, err := New(Options{})
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || perr.Kind != providers.KindInput {
		t.Fatalf("err = %v, want input", err)
	}
}

func TestChatTextUsageAndEffortPassthrough(t *testing.T) {
	srv := server(t, "k", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if body["reasoning_effort"] != "low" {
			t.Errorf("reasoning_effort = %v", body["reasoning_effort"])
		}
		writeJSON(t, w, map[string]any{
			"model": "custom-1",
			"choices": []any{map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "served",
					"tool_calls": []any{map[string]any{
						"id":   "c1",
						"type": "function",
						"function": map[string]any{
							"name":      "lookup",
							"arguments": `{"k":"v"}`,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
			"usage": map[string]any{
				"prompt_tokens":     500,
				"completion_tokens": 11,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 200,
				},
			},
		})
	})
	defer srv.Close()
	a, err := New(Options{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	resp, err := a.Chat(context.Background(), providers.ChatRequest{
		Model:           "custom-1",
		Messages:        []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "served" || len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("response = %+v", resp.Message)
	}
	want := providers.Usage{Input: 500, CacheRead: 200, Output: 11}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestKeylessServer(t *testing.T) {
	srv := server(t, "", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		writeJSON(t, w, map[string]any{
			"model": "local",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "x"},
				"finish_reason": "stop",
			}},
		})
	})
	defer srv.Close()
	a, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	if _, err := a.Chat(context.Background(), userReq("local")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestStreamReassemblesFragmentedToolCall(t *testing.T) {
	srv := server(t, "k", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range []string{
			`{"choices":[{"delta":{"content":"Okay"},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"fetch","arguments":"{\"u"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"rl\":\"u\"}"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":120,"completion_tokens":8}}`,
			`[DONE]`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
	})
	defer srv.Close()
	a, err := New(Options{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	var events []providers.StreamEvent
	if err := a.Stream(context.Background(), userReq("custom-1"), func(ev providers.StreamEvent) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %+v", events)
	}
	tc := events[1].ToolCallDelta
	if tc == nil || tc.Name != "fetch" || tc.Arguments != `{"url":"u"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if events[2].Usage == nil || *events[2].Usage != (providers.Usage{Input: 120, Output: 8}) {
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
		{"transient", 500, `{"error":{"message":"boom"}}`, providers.KindTransient},
		{"rate limited", 429, `{"error":{"message":"slow"}}`, providers.KindRateLimited},
		{"auth", 401, `{"error":{"message":"bad key"}}`, providers.KindAuth},
		{"input", 400, `{"error":{"message":"bad"}}`, providers.KindInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := server(t, "k", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			defer srv.Close()
			a, err := New(Options{BaseURL: srv.URL, APIKey: "k"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer a.Close()
			_, err = a.Chat(context.Background(), userReq("m"))
			var perr *providers.ProviderError
			if !errors.As(err, &perr) || perr.Kind != tc.kind {
				t.Fatalf("err = %v, want %s", err, tc.kind)
			}
		})
	}
}

func TestCancellation(t *testing.T) {
	srv := server(t, "k", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		<-r.Context().Done()
	})
	defer srv.Close()
	a, err := New(Options{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Chat(ctx, userReq("m")); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
