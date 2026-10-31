package openai

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

func respServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		handler(w, r, body)
	}))
}

func newRespAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	a, err := New(Options{APIKey: "test-key", BaseURL: srv.URL, UseResponses: true, PromptCacheKey: "role:scope"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func TestResponsesChatTextToolsUsage(t *testing.T) {
	srv := respServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if body["prompt_cache_key"] != "role:scope" {
			t.Errorf("prompt_cache_key = %v", body["prompt_cache_key"])
		}
		if body["reasoning"].(map[string]any)["effort"] != "low" {
			t.Errorf("reasoning = %v", body["reasoning"])
		}
		if body["max_output_tokens"] != float64(64) {
			t.Errorf("max_output_tokens = %v", body["max_output_tokens"])
		}
		inputs, _ := body["input"].([]any)
		if len(inputs) != 3 {
			t.Errorf("input items = %v", body["input"])
		}
		tools, _ := body["tools"].([]any)
		if len(tools) != 1 || tools[0].(map[string]any)["type"] != "function" {
			t.Errorf("tools = %v", body["tools"])
		}
		writeJSON(t, w, map[string]any{
			"id":     "resp-1",
			"model":  "gpt-x",
			"status": "completed",
			"output": []any{
				map[string]any{
					"type": "reasoning",
				},
				map[string]any{
					"type": "message",
					"content": []any{map[string]any{
						"type": "output_text", "text": "here",
					}},
				},
				map[string]any{
					"type":      "function_call",
					"call_id":   "call_9",
					"name":      "read",
					"arguments": `{"path":"b"}`,
				},
			},
			"usage": map[string]any{
				"input_tokens":  3000,
				"output_tokens": 50,
				"input_tokens_details": map[string]any{
					"cached_tokens": 2048,
				},
				"output_tokens_details": map[string]any{
					"reasoning_tokens": 20,
				},
			},
		})
	})
	defer srv.Close()
	a := newRespAdapter(t, srv)
	resp, err := a.Chat(context.Background(), providers.ChatRequest{
		Model: "gpt-x",
		Messages: []providers.Message{
			{Role: providers.RoleUser, Content: "go"},
			{Role: providers.RoleAssistant, Content: "", ToolCalls: []providers.ToolCall{
				{ID: "call_8", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)},
			}},
			{Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "call_8", Name: "read", Output: "data"}},
		},
		Tools:           []providers.ToolDefinition{{Name: "read", Description: "read", Schema: json.RawMessage(`{"type":"object"}`)}},
		MaxTokens:       64,
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Message.Content != "here" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", resp.Message.ToolCalls)
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "call_9" || tc.Name != "read" || string(tc.Arguments) != `{"path":"b"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	want := providers.Usage{Input: 3000, CacheRead: 2048, Output: 50, Reasoning: 20}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestResponsesStreamReassembles(t *testing.T) {
	srv := respServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if body["stream"] != true {
			t.Errorf("stream = %v", body["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		events := []struct{ event, data string }{
			{"response.output_text.delta", `{"delta":"Hi"}`},
			{"response.output_item.added", `{"output_index":0,"item":{"type":"function_call","call_id":"call_3","name":"exec"}}`},
			{"response.function_call_arguments.delta", `{"output_index":0,"delta":"{\"cm"}`},
			{"response.function_call_arguments.delta", `{"output_index":0,"delta":"d\":\"x\"}"}`},
			{"response.output_item.done", `{"output_index":0,"item":{"type":"function_call","call_id":"call_3","name":"exec","arguments":"{\"cmd\":\"x\"}"}}`},
			{"response.completed", `{"response":{"status":"completed","usage":{"input_tokens":4000,"output_tokens":12,"input_tokens_details":{"cached_tokens":3000}}}}`},
		}
		for _, e := range events {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.event, e.data)
			flusher.Flush()
		}
	})
	defer srv.Close()
	a := newRespAdapter(t, srv)
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
	if events[0].Delta != "Hi" {
		t.Fatalf("delta = %+v", events[0])
	}
	tc := events[1].ToolCallDelta
	if tc == nil || tc.ID != "call_3" || tc.Name != "exec" || tc.Arguments != `{"cmd":"x"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if events[2].Usage == nil || *events[2].Usage != (providers.Usage{Input: 4000, CacheRead: 3000, Output: 12}) {
		t.Fatalf("usage = %+v", events[2])
	}
	if !events[3].Done {
		t.Fatalf("last = %+v", events[3])
	}
}

func TestResponsesErrors(t *testing.T) {
	t.Run("http", func(t *testing.T) {
		srv := respServer(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
		})
		defer srv.Close()
		a := newRespAdapter(t, srv)
		_, err := a.Chat(context.Background(), userReq("m"))
		var perr *providers.ProviderError
		if !errors.As(err, &perr) || perr.Kind != providers.KindAuth {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		srv := respServer(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
			writeJSON(t, w, map[string]any{
				"model":  "m",
				"status": "incomplete",
				"incomplete_details": map[string]any{
					"reason": "max_output_tokens",
				},
				"output": []any{},
				"usage":  map[string]any{"input_tokens": 1},
			})
		})
		defer srv.Close()
		a := newRespAdapter(t, srv)
		_, err := a.Chat(context.Background(), userReq("m"))
		var perr *providers.ProviderError
		if !errors.As(err, &perr) || perr.Kind != providers.KindTruncated {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("validation", func(t *testing.T) {
		srv := respServer(t, func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
			writeJSON(t, w, map[string]any{"status": "completed"})
		})
		defer srv.Close()
		a := newRespAdapter(t, srv)
		_, err := a.Chat(context.Background(), providers.ChatRequest{Model: ""})
		var perr *providers.ProviderError
		if !errors.As(err, &perr) || perr.Kind != providers.KindInput {
			t.Fatalf("err = %v, want input", err)
		}
	})
}
