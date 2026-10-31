package openaiwire

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func testClient(t *testing.T, srv *httptest.Server, reason ReasonMode) *Client {
	t.Helper()
	c, err := New(Options{Provider: "test", BaseURL: srv.URL, Reasoning: reason})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func TestChatTextAndUsage(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writeJSON(t, w, map[string]any{
			"id":    "chatcmpl-1",
			"model": "m1",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "hello"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     1200,
				"completion_tokens": 30,
				"total_tokens":      1230,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 1024,
				},
				"completion_tokens_details": map[string]any{
					"reasoning_tokens": 12,
				},
			},
		})
	}))
	defer srv.Close()

	c := testClient(t, srv, ReasonEffort)
	resp, err := c.Chat(context.Background(), providers.ChatRequest{
		Model:           "m1",
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
	if resp.Model != "m1" {
		t.Fatalf("model = %q", resp.Model)
	}
	want := providers.Usage{Input: 1200, CacheRead: 1024, Output: 30, Reasoning: 12}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
	if got["reasoning_effort"] != "low" {
		t.Fatalf("reasoning_effort = %v", got["reasoning_effort"])
	}
	if got["max_tokens"] != float64(64) {
		t.Fatalf("max_tokens = %v", got["max_tokens"])
	}
	if _, ok := got["stream_options"]; ok {
		t.Fatalf("non-streaming request carries stream_options")
	}
}

func TestMaxTokensField(t *testing.T) {
	for _, tc := range []struct {
		name string
		flag bool
		key  string
	}{
		{"legacy", false, "max_tokens"},
		{"completion", true, "max_completion_tokens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&got)
				writeJSON(t, w, map[string]any{
					"model": "m",
					"choices": []any{map[string]any{
						"message":       map[string]any{"role": "assistant", "content": "x"},
						"finish_reason": "stop",
					}},
				})
			}))
			defer srv.Close()
			c, err := New(Options{Provider: "test", BaseURL: srv.URL, UseMaxCompletionTokens: tc.flag})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := c.Chat(context.Background(), providers.ChatRequest{
				Model:     "m",
				Messages:  []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
				MaxTokens: 32,
			}); err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if got[tc.key] != float64(32) {
				t.Fatalf("%s = %v", tc.key, got[tc.key])
			}
			other := "max_tokens"
			if tc.key == other {
				other = "max_completion_tokens"
			}
			if _, ok := got[other]; ok {
				t.Fatalf("both limit fields sent: %v", got)
			}
		})
	}
}

func TestChatReasoningStyles(t *testing.T) {
	cases := []struct {
		name   string
		reason ReasonMode
		check  func(t *testing.T, got map[string]any)
	}{
		{"omit", ReasonOmit, func(t *testing.T, got map[string]any) {
			t.Helper()
			if _, ok := got["reasoning_effort"]; ok {
				t.Fatalf("omit mode sent reasoning_effort")
			}
			if _, ok := got["reasoning"]; ok {
				t.Fatalf("omit mode sent reasoning")
			}
		}},
		{"object", ReasonObject, func(t *testing.T, got map[string]any) {
			t.Helper()
			r, ok := got["reasoning"].(map[string]any)
			if !ok || r["effort"] != "low" {
				t.Fatalf("reasoning = %v", got["reasoning"])
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&got)
				writeJSON(t, w, map[string]any{
					"model": "m",
					"choices": []any{map[string]any{
						"message":       map[string]any{"role": "assistant", "content": "x"},
						"finish_reason": "stop",
					}},
					"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
				})
			}))
			defer srv.Close()
			c := testClient(t, srv, tc.reason)
			_, err := c.Chat(context.Background(), providers.ChatRequest{
				Model:           "m",
				Messages:        []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
				ReasoningEffort: "low",
			})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			tc.check(t, got)
		})
	}
}

func TestChatDeepSeekUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	defer srv.Close()
	c := testClient(t, srv, ReasonOmit)
	resp, err := c.Chat(context.Background(), providers.ChatRequest{
		Model:    "deepseek-chat",
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	want := providers.Usage{Input: 100, CacheRead: 70, CacheWrite: 30, Output: 5}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestChatRequestShape(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeJSON(t, w, map[string]any{
			"model": "m",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": "done"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 2},
		})
	}))
	defer srv.Close()
	c := testClient(t, srv, ReasonEffort)
	_, err := c.Chat(context.Background(), providers.ChatRequest{
		Model: "m",
		Messages: []providers.Message{
			{Role: providers.RoleSystem, Content: "sys"},
			{Role: providers.RoleAssistant, Content: "thinking", ToolCalls: []providers.ToolCall{
				{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)},
			}},
			{Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "c1", Name: "read", Output: "data"}},
		},
		Tools: []providers.ToolDefinition{
			{Name: "read", Description: "read a file", Schema: json.RawMessage(`{"type":"object"}`)},
			{Name: "noschema"},
		},
		ToolChoice: "auto",
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v", got["messages"])
	}
	toolMsg := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "c1" || toolMsg["content"] != "data" {
		t.Fatalf("tool message = %v", toolMsg)
	}
	asst := msgs[1].(map[string]any)
	tcs, _ := asst["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("assistant tool calls = %v", asst["tool_calls"])
	}
	fn := tcs[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "read" || fn["arguments"] != `{"path":"a"}` {
		t.Fatalf("tool call = %v", tcs[0])
	}
	tools, _ := got["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v", got["tools"])
	}
	second := tools[1].(map[string]any)["function"].(map[string]any)
	params, _ := second["parameters"].(map[string]any)
	if len(params) != 0 {
		t.Fatalf("empty schema parameters = %v", params)
	}
	if got["tool_choice"] != "auto" {
		t.Fatalf("tool_choice = %v", got["tool_choice"])
	}
}

func TestChatClientSideValidation(t *testing.T) {
	c, err := New(Options{Provider: "test", BaseURL: "http://localhost:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := []struct {
		name string
		req  providers.ChatRequest
	}{
		{"empty model", providers.ChatRequest{Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}}}},
		{"bad role", providers.ChatRequest{Model: "m", Messages: []providers.Message{{Role: "wizard", Content: "x"}}}},
		{"bad tool args", providers.ChatRequest{Model: "m", Messages: []providers.Message{{
			Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "1", Name: "t", Arguments: json.RawMessage("{oops")}},
		}}}},
		{"tool without result", providers.ChatRequest{Model: "m", Messages: []providers.Message{{Role: providers.RoleTool}}}},
		{"tool without call id", providers.ChatRequest{Model: "m", Messages: []providers.Message{{
			Role: providers.RoleTool, ToolResult: &providers.ToolResult{Name: "t", Output: "x"},
		}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Chat(context.Background(), tc.req)
			var perr *providers.ProviderError
			if !errors.As(err, &perr) || perr.Kind != providers.KindInput {
				t.Fatalf("err = %v, want input error", err)
			}
		})
	}
	if _, err := New(Options{}); err == nil {
		t.Fatalf("New without provider/base accepted")
	}
}

func TestStreamReassemblesToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["stream"] != true {
			t.Errorf("stream = %v", got["stream"])
		}
		if got["stream_options"].(map[string]any)["include_usage"] != true {
			t.Errorf("stream_options = %v", got["stream_options"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`: keep-alive`,
			`{"model":"m","choices":[{"delta":{"role":"assistant","content":"Hi"},"finish_reason":""}]}`,
			`{"model":"m","choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"write","arguments":"{\"pa"}}]},"finish_reason":""}]}`,
			`{"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"path\":"}}]},"finish_reason":""}]}`,
			`{"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a\"}"}}]},"finish_reason":""}]}`,
			`{"model":"m","choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"th\":\"b\"}"}}]},"finish_reason":""}]}`,
			`{"model":"m","choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":50,"completion_tokens":9,"prompt_tokens_details":{"cached_tokens":40}}}`,
			`[DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := testClient(t, srv, ReasonEffort)
	var events []providers.StreamEvent
	err := c.Stream(context.Background(), providers.ChatRequest{
		Model:    "m",
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "hi"}},
	}, func(ev providers.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Delta != "Hi" {
		t.Fatalf("first delta = %+v", events[0])
	}
	// Tool calls arrive whole, ordered by index.
	tc0 := events[1].ToolCallDelta
	if tc0 == nil || tc0.Index != 0 || tc0.ID != "c1" || tc0.Name != "read" || tc0.Arguments != `{"path":"a"}` {
		t.Fatalf("tool 0 = %+v", tc0)
	}
	tc1 := events[2].ToolCallDelta
	if tc1 == nil || tc1.Index != 1 || tc1.ID != "c2" || tc1.Name != "write" || tc1.Arguments != `{"path":"b"}` {
		t.Fatalf("tool 1 = %+v", tc1)
	}
	if events[3].Usage == nil || *events[3].Usage != (providers.Usage{Input: 50, CacheRead: 40, Output: 9}) {
		t.Fatalf("usage event = %+v", events[3])
	}
	if !events[4].Done {
		t.Fatalf("last event = %+v, want done", events[4])
	}
}

func TestStreamTerminationKinds(t *testing.T) {
	cases := []struct {
		name   string
		finish string
		kind   providers.ErrorKind
	}{
		{"truncated", "length", providers.KindTruncated},
		{"content rejected", "content_filter", providers.KindContentRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"part\"},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", tc.finish)
			}))
			defer srv.Close()
			c := testClient(t, srv, ReasonEffort)
			var deltas []string
			var done bool
			err := c.Stream(context.Background(), providers.ChatRequest{
				Model:    "m",
				Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}},
			}, func(ev providers.StreamEvent) error {
				deltas = append(deltas, ev.Delta)
				done = done || ev.Done
				return nil
			})
			var perr *providers.ProviderError
			if !errors.As(err, &perr) || perr.Kind != tc.kind {
				t.Fatalf("err = %v, want %s", err, tc.kind)
			}
			if !done || len(deltas) == 0 {
				t.Fatalf("events lost on %s: done=%v deltas=%v", tc.finish, done, deltas)
			}
		})
	}
}

func TestStreamEmitAbort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "{\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n[DONE]\n\n")
	}))
	defer srv.Close()
	c := testClient(t, srv, ReasonEffort)
	sentinel := errors.New("stop")
	err := c.Stream(context.Background(), providers.ChatRequest{Model: "m"}, func(providers.StreamEvent) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want emit error", err)
	}
}

func TestErrorClasses(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		headers    map[string]string
		body       string
		kind       providers.ErrorKind
		retryAfter time.Duration
	}{
		{"transient", 500, nil, `{"error":{"message":"boom","type":"server"}}`, providers.KindTransient, 0},
		{"bad gateway", 502, nil, `upstream down`, providers.KindTransient, 0},
		{"rate limited", 429, map[string]string{"Retry-After": "3"}, `{"error":{"message":"slow","type":"rate"}}`, providers.KindRateLimited, 3 * time.Second},
		{"rate limited date", 429, nil, `{"error":{"message":"slow"}}`, providers.KindRateLimited, 0},
		{"auth", 401, nil, `{"error":{"message":"bad key","type":"auth","code":"invalid_api_key"}}`, providers.KindAuth, 0},
		{"forbidden", 403, nil, `{"error":{"message":"denied"}}`, providers.KindAuth, 0},
		{"quota", 402, nil, `{"error":{"message":"credits exhausted"}}`, providers.KindAuth, 0},
		{"input", 400, nil, `{"error":{"message":"bad arg","code":"invalid"}}`, providers.KindInput, 0},
		{"not found", 404, nil, `{"error":{"message":"no such model"}}`, providers.KindInput, 0},
		{"content error", 400, nil, `{"error":{"message":"blocked","code":"content_filter"}}`, providers.KindContentRejected, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := testClient(t, srv, ReasonEffort)
			_, err := c.Chat(context.Background(), providers.ChatRequest{
				Model:    "m",
				Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}},
			})
			var perr *providers.ProviderError
			if !errors.As(err, &perr) {
				t.Fatalf("err = %v, want provider error", err)
			}
			if perr.Kind != tc.kind {
				t.Fatalf("kind = %s, want %s (%v)", perr.Kind, tc.kind, err)
			}
			if !perr.Retryable() != (tc.kind != providers.KindTransient && tc.kind != providers.KindRateLimited) {
				t.Fatalf("retryable mismatch for %s", tc.kind)
			}
			if perr.RetryAfter != tc.retryAfter {
				t.Fatalf("retryAfter = %v, want %v", perr.RetryAfter, tc.retryAfter)
			}
			if perr.Provider != "test" {
				t.Fatalf("provider = %q", perr.Provider)
			}
		})
	}
}

func TestRetryAfterDate(t *testing.T) {
	future := time.Now().Add(90 * time.Second).UTC()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", future.Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := testClient(t, srv, ReasonEffort)
	_, err := c.Chat(context.Background(), providers.ChatRequest{Model: "m"})
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || perr.Kind != providers.KindTransient {
		t.Fatalf("err = %v", err)
	}
	if perr.RetryAfter < 60*time.Second || perr.RetryAfter > 120*time.Second {
		t.Fatalf("retryAfter = %v", perr.RetryAfter)
	}
}

func TestMalformedSuccessIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()
	c := testClient(t, srv, ReasonEffort)
	_, err := c.Chat(context.Background(), providers.ChatRequest{
		Model:    "m",
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}},
	})
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || perr.Kind != providers.KindTransient {
		t.Fatalf("err = %v, want transient", err)
	}
}

func TestEmptyChoicesIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"model": "m", "choices": []any{}})
	}))
	defer srv.Close()
	c := testClient(t, srv, ReasonEffort)
	_, err := c.Chat(context.Background(), providers.ChatRequest{
		Model:    "m",
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}},
	})
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || perr.Kind != providers.KindTransient {
		t.Fatalf("err = %v, want transient", err)
	}
}

func TestCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-release:
			writeJSON(t, w, map[string]any{"model": "m"})
		}
	}))
	defer srv.Close()
	defer close(release)
	c := testClient(t, srv, ReasonEffort)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Chat(ctx, providers.ChatRequest{Model: "m"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("chat err = %v, want context.Canceled", err)
	}
	err = c.Stream(ctx, providers.ChatRequest{Model: "m"}, func(providers.StreamEvent) error {
		t.Fatalf("emit must not run after cancel")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("stream err = %v, want context.Canceled", err)
	}
}

func TestToUsageClamps(t *testing.T) {
	u := toUsage(&wireUsage{
		PromptTokens:            10,
		PromptTokensDetails:     &promptDetails{CachedTokens: 99},
		CompletionTokens:        5,
		CompletionTokensDetails: &completionDetails{ReasoningTokens: 50},
		PromptCacheMissTokens:   40,
	})
	if u.CacheRead != 10 || u.CacheWrite != 0 || u.Reasoning != 5 {
		t.Fatalf("clamped = %+v", u)
	}
	if z := toUsage(nil); z != (providers.Usage{}) {
		t.Fatalf("nil usage = %+v", z)
	}
}

func TestCost(t *testing.T) {
	u := providers.Usage{Input: 1000, CacheRead: 600, CacheWrite: 100, Output: 200, Reasoning: 50}
	p := Price{Input: 1, CacheRead: 0.5, CacheWrite: 0.25, Output: 4, Reasoning: 2}
	// (300*1 + 600*0.5 + 100*0.25 + 150*4 + 50*2)/1e6
	want := (300.0*1 + 600.0*0.5 + 100.0*0.25 + 150.0*4 + 50.0*2) / 1e6
	if got := Cost(u, p); got != want {
		t.Fatalf("cost = %v, want %v", got, want)
	}
	// Reasoning falls back to the output rate.
	p2 := Price{Input: 1, Output: 4}
	if got, want := Cost(u, p2), (300.0*1+600*0+100*0+200*4)/1e6; got != want {
		t.Fatalf("fallback cost = %v, want %v", got, want)
	}
}

func TestEachSSEData(t *testing.T) {
	body := ": comment\n" +
		"data: {\"a\":1}\n" +
		"\n" +
		"data: {\"b\":\n" +
		"data: 2}\n" +
		"\n" +
		"data: [DONE]\n" +
		"data: ignored\n"
	var got []string
	if err := EachSSEData(strings.NewReader(body), func(data string) error {
		got = append(got, data)
		return nil
	}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != 2 || got[0] != `{"a":1}` || got[1] != "{\"b\":\n2}" {
		t.Fatalf("payloads = %q", got)
	}
}

func TestAuthHeaderAndExtras(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Custom") != "v" {
			t.Errorf("custom = %q", r.Header.Get("X-Custom"))
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
	c, err := New(Options{
		Provider:     "test",
		BaseURL:      srv.URL,
		APIKey:       "k",
		ExtraHeaders: map[string]string{"X-Custom": "v"},
		Reasoning:    ReasonEffort,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Chat(context.Background(), providers.ChatRequest{Model: "m"}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
}
