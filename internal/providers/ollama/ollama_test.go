package ollama

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
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("auth must be empty, got %q", got)
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
	a, err := New(Options{BaseURL: srv.URL})
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
	a, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()
	if a.Name() != "ollama" {
		t.Fatalf("name = %q", a.Name())
	}
}

func TestBaseResolution(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "")
	t.Setenv("OLLAMA_HOST", "")
	a, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	t.Setenv("OLLAMA_HOST", "myhost:11434")
	b, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer b.Close()

	t.Setenv("OLLAMA_BASE_URL", "http://custom:9999/v1")
	c, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
}

func TestChatTextUsageAndNoEffort(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if _, ok := body["reasoning_effort"]; ok {
			t.Errorf("ollama must not send reasoning_effort")
		}
		writeJSON(t, w, map[string]any{
			"model": "llama-test",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "local ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     40,
				"completion_tokens": 6,
				"total_tokens":      46,
			},
		})
	})
	defer srv.Close()
	a := newAdapter(t, srv)
	resp, err := a.Chat(context.Background(), providers.ChatRequest{
		Model:           "llama-test",
		Messages:        []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "local ok" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	want := providers.Usage{Input: 40, Output: 6}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
	if got := CostUSD(resp.Usage); got != 0 {
		t.Fatalf("cost = %v, want 0", got)
	}
}

func TestChatWithoutUsage(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		writeJSON(t, w, map[string]any{
			"model": "llama-test",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "x"},
				"finish_reason": "stop",
			}},
		})
	})
	defer srv.Close()
	a := newAdapter(t, srv)
	resp, err := a.Chat(context.Background(), userReq("llama-test"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Usage != (providers.Usage{}) {
		t.Fatalf("usage = %+v, want zero", resp.Usage)
	}
}

func TestStreamReassemblesFragmentedToolCall(t *testing.T) {
	srv := server(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range []string{
			`{"choices":[{"delta":{"content":"On it"},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"run","arguments":"{\"a"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":1}"}}]},"finish_reason":""}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
	})
	defer srv.Close()
	a := newAdapter(t, srv)
	var events []providers.StreamEvent
	if err := a.Stream(context.Background(), userReq("llama-test"), func(ev providers.StreamEvent) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %+v", events)
	}
	tc := events[1].ToolCallDelta
	if tc == nil || tc.Name != "run" || tc.Arguments != `{"a":1}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if events[2].Usage != nil {
		t.Fatalf("usage without server payload must be absent, got %+v", events[2])
	}
	if !events[2].Done {
		t.Fatalf("last = %+v, want done", events[2])
	}
}

func TestErrorClasses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		kind   providers.ErrorKind
	}{
		{"transient", 500, `connection refused`, providers.KindTransient},
		{"input", 400, `{"error":{"message":"bad"}}`, providers.KindInput},
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
