package anthropic

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

func testRequest() providers.ChatRequest {
	return providers.ChatRequest{
		Model: "claude-test",
		Messages: []providers.Message{
			{Role: providers.RoleSystem, Content: "be brief"},
			{Role: providers.RoleUser, Content: "hi"},
		},
		Tools: []providers.ToolDefinition{{
			Name:        "read_file",
			Description: "read a file",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
		MaxTokens:  128,
		CacheTTL:   providers.CacheTTL1h,
		ToolChoice: "auto",
	}
}

func okBody(model string) string {
	return fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","model":%q,`+
		`"content":[{"type":"text","text":"hello"},{"type":"tool_use","id":"tu_1","name":"read_file","input":{"path":"a.go"}}],`+
		`"stop_reason":"tool_use","usage":{"input_tokens":120,"output_tokens":30,`+
		`"cache_creation_input_tokens":100,"cache_read_input_tokens":10}}`, model)
}

func TestChatSuccess(t *testing.T) {
	var got wireRequest
	var gotVersion, gotKey string
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotVersion = r.Header.Get("Anthropic-Version")
		gotKey = r.Header.Get("X-Api-Key")
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, okBody("claude-test"))
	}))
	defer srv.Close()

	p := New(Options{APIKey: "k", BaseURL: srv.URL, Model: "fallback"})
	resp, err := p.Chat(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if gotVersion != DefaultVersion || gotKey != "k" {
		t.Fatalf("headers version=%q key set=%v", gotVersion, gotKey != "")
	}
	if got.Model != "claude-test" || got.MaxTokens != 128 || got.Stream {
		t.Fatalf("wire request: %+v", got)
	}
	if len(got.System) != 1 || got.System[0].Text != "be brief" {
		t.Fatalf("system: %+v", got.System)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" {
		t.Fatalf("messages: %+v", got.Messages)
	}
	if len(got.Tools) != 1 || string(got.Tools[0].InputSchema) == "" {
		t.Fatalf("tools: %+v", got.Tools)
	}
	if resp.Message.Content != "hello" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool calls: %+v", resp.Message.ToolCalls)
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "tu_1" || tc.Name != "read_file" || string(tc.Arguments) != `{"path":"a.go"}` {
		t.Fatalf("tool call: %+v", tc)
	}
	want := providers.Usage{Input: 120, CacheRead: 10, CacheWrite: 100, Output: 30}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestChatCacheMarksBounded(t *testing.T) {
	var got wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, okBody("m"))
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	req := providers.ChatRequest{
		Model: "m",
		Messages: []providers.Message{
			{Role: providers.RoleSystem, Content: "sys"},
			{Role: providers.RoleUser, Content: "one"},
			{Role: providers.RoleAssistant, Content: "two"},
			{Role: providers.RoleUser, Content: "three"},
		},
		Tools: []providers.ToolDefinition{{Name: "t", Schema: json.RawMessage(`{"type":"object"}`)}},
	}
	if _, err := p.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	marks := 0
	for _, b := range got.System {
		if b.CacheControl != nil {
			marks++
			if b.CacheControl.TTL != providers.CacheTTL5m {
				t.Fatalf("default TTL = %q", b.CacheControl.TTL)
			}
		}
	}
	for _, tl := range got.Tools {
		if tl.CacheControl != nil {
			marks++
		}
	}
	for _, m := range got.Messages {
		for _, b := range m.Content {
			if b.CacheControl != nil {
				marks++
			}
		}
	}
	if marks == 0 || marks > MaxCacheMarks {
		t.Fatalf("marks = %d, want 1..%d", marks, MaxCacheMarks)
	}
}

func TestChatThinkingMapping(t *testing.T) {
	var got wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, okBody("m"))
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	req := testRequest()
	req.ReasoningEffort = "medium"
	req.MaxTokens = 8192
	if _, err := p.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got.Thinking == nil || got.Thinking.Type != "enabled" || got.Thinking.BudgetTokens != 4096 {
		t.Fatalf("thinking: %+v", got.Thinking)
	}

	req.ReasoningEffort = "999"
	req.MaxTokens = 512
	if _, err := p.Chat(context.Background(), req); err == nil {
		t.Fatal("budget below provider minimum must fail")
	} else {
		var perr *providers.ProviderError
		if !errors.As(err, &perr) || perr.Kind != providers.KindInput {
			t.Fatalf("kind = %v", err)
		}
	}

	req.ReasoningEffort = "high"
	req.MaxTokens = 1024
	if _, err := p.Chat(context.Background(), req); err == nil {
		t.Fatal("budget >= max_tokens must fail")
	}
}

func TestChatToolResultRoundTrip(t *testing.T) {
	var got wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, okBody("m"))
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	req := providers.ChatRequest{
		Model: "m",
		Messages: []providers.Message{
			{Role: providers.RoleUser, Content: "read it"},
			{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{
				ID: "tu_9", Name: "read_file", Arguments: json.RawMessage(`{"path":"x"}`),
			}}},
			{Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "tu_9", Name: "read_file", Output: "data", IsError: true}},
		},
	}
	if _, err := p.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("messages: %+v", got.Messages)
	}
	last := got.Messages[2].Content[0]
	if last.Type != "tool_result" || last.ToolUseID != "tu_9" || last.Text != "data" || last.IsError == nil {
		t.Fatalf("tool_result: %+v", last)
	}
}

func TestChatRetryThenSuccess(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, `{"error":{"type":"overloaded","message":"busy"}}`)
			return
		}
		fmt.Fprint(w, okBody("m"))
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL, MaxAttempts: 3})
	if _, err := p.Chat(context.Background(), testRequest()); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestChatRetryAfterHonored(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"type":"rate_limit","message":"slow"}}`)
			return
		}
		fmt.Fprint(w, okBody("m"))
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	start := time.Now()
	if _, err := p.Chat(context.Background(), testRequest()); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("Retry-After not honored: %v", elapsed)
	}
}

func TestChatErrorKinds(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   providers.ErrorKind
		calls  int
	}{
		{401, `{"error":{"type":"auth","message":"bad key"}}`, providers.KindAuth, 1},
		{400, `{"error":{"type":"invalid","message":"bad"}}`, providers.KindInput, 1},
		{503, `{"error":{"type":"overloaded","message":"busy"}}`, providers.KindTransient, 3},
	}
	for _, c := range cases {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(c.status)
			fmt.Fprint(w, c.body)
		}))
		p := New(Options{BaseURL: srv.URL, MaxAttempts: 3})
		_, err := p.Chat(context.Background(), testRequest())
		srv.Close()
		var perr *providers.ProviderError
		if !errors.As(err, &perr) || perr.Kind != c.kind {
			t.Fatalf("status %d: kind = %v", c.status, err)
		}
		if calls != c.calls {
			t.Fatalf("status %d: calls = %d, want %d", c.status, calls, c.calls)
		}
	}
}

func TestChatTerminalSignals(t *testing.T) {
	for _, stop := range []string{"refusal", "max_tokens"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"model":"m","stop_reason":%q,"content":[],"usage":{"input_tokens":1,"output_tokens":2}}`, stop)
		}))
		p := New(Options{BaseURL: srv.URL})
		_, err := p.Chat(context.Background(), testRequest())
		srv.Close()
		var perr *providers.ProviderError
		want := providers.KindContentRejected
		if stop == "max_tokens" {
			want = providers.KindTruncated
		}
		if !errors.As(err, &perr) || perr.Kind != want {
			t.Fatalf("stop %s: err = %v", stop, err)
		}
	}
}

func TestChatCancellation(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, okBody("m"))
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Chat(ctx, testRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	var perr *providers.ProviderError
	if errors.As(err, &perr) {
		t.Fatal("cancellation must not be a ProviderError")
	}
	if calls != 0 {
		t.Fatalf("canceled call must not reach the server, calls = %d", calls)
	}
}

func TestChatValidation(t *testing.T) {
	p := New(Options{})
	bad := []providers.ChatRequest{
		{Model: "m"},
		{Model: "m", Messages: []providers.Message{{Role: providers.RoleAssistant, Content: "x"}}},
		{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}}, CacheTTL: "2h"},
		{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}}, ReasoningEffort: "bogus"},
		{Model: "m", Messages: []providers.Message{{Role: "alien", Content: "x"}}},
	}
	for i, req := range bad {
		if _, err := p.Chat(context.Background(), req); err == nil {
			t.Fatalf("case %d must fail", i)
		} else {
			var perr *providers.ProviderError
			if !errors.As(err, &perr) || perr.Kind != providers.KindInput {
				t.Fatalf("case %d: kind = %v", i, err)
			}
		}
	}
	if _, err := p.Chat(context.Background(), providers.ChatRequest{
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}},
	}); err == nil {
		t.Fatal("missing model must fail")
	}
}

func sseStream() string {
	return "event: message_start\n" +
		`data: {"message":{"usage":{"input_tokens":50,"cache_read_input_tokens":40,"cache_creation_input_tokens":5}}}` + "\n\n" +
		"event: content_block_start\n" +
		`data: {"index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"read_file"}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"index":1,"delta":{"type":"input_json_delta","partial_json":"\"a.go\"}"}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"index":0,"delta":{"type":"text_delta","text":"working"}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}` + "\n\n" +
		"event: message_stop\n" +
		"data: {}\n\n"
}

func TestStreamSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept = %s", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseStream())
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	var events []providers.StreamEvent
	err := p.Stream(context.Background(), testRequest(), func(ev providers.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var deltas []string
	var toolArgs []string
	var usage *providers.Usage
	done := false
	for _, ev := range events {
		deltas = append(deltas, ev.Delta)
		if ev.ToolCallDelta != nil {
			toolArgs = append(toolArgs, ev.ToolCallDelta.Arguments)
			if ev.ToolCallDelta.ID != "tu_1" || ev.ToolCallDelta.Name != "read_file" || ev.ToolCallDelta.Index != 1 {
				t.Fatalf("fragment lost identity: %+v", ev.ToolCallDelta)
			}
		}
		if ev.Usage != nil {
			usage = ev.Usage
		}
		if ev.Done {
			done = true
		}
	}
	if strings.Join(deltas, "") != "working" {
		t.Fatalf("text = %q", strings.Join(deltas, ""))
	}
	if strings.Join(toolArgs, "") != `{"path":"a.go"}` {
		t.Fatalf("args = %q", strings.Join(toolArgs, ""))
	}
	if usage == nil || *usage != (providers.Usage{Input: 50, CacheRead: 40, CacheWrite: 5, Output: 12}) {
		t.Fatalf("usage = %+v", usage)
	}
	if !done {
		t.Fatal("stream must end with Done")
	}
}

func TestStreamEmitAbort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseStream())
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	boom := errors.New("stop")
	err := p.Stream(context.Background(), testRequest(), func(providers.StreamEvent) error {
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamRetryBeforeFirstEvent(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"type":"overloaded","message":"busy"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseStream())
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	var n int
	err := p.Stream(context.Background(), testRequest(), func(providers.StreamEvent) error {
		n++
		return nil
	})
	if err != nil || calls != 2 || n == 0 {
		t.Fatalf("err=%v calls=%d events=%d", err, calls, n)
	}
}

func TestStreamNoRetryAfterFirstEvent(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		// Truncated mid-stream: valid event then garbage.
		fmt.Fprint(w, "event: content_block_delta\n"+`data: {"index":0,"delta":{"type":"text_delta","text":"x"}}`+"\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {invalid-json\n\n")
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL, MaxAttempts: 3})
	var n int
	err := p.Stream(context.Background(), testRequest(), func(providers.StreamEvent) error {
		n++
		return nil
	})
	if err == nil || calls != 1 || n != 1 {
		t.Fatalf("mid-stream failure must not retry: err=%v calls=%d events=%d", err, calls, n)
	}
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || !perr.Retryable() {
		t.Fatalf("transport failure must stay retryable-classified: %v", err)
	}
}

func TestStreamCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		fmt.Fprint(w, ": ping\n\n")
		fl.Flush()
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := p.Stream(ctx, testRequest(), func(providers.StreamEvent) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}
