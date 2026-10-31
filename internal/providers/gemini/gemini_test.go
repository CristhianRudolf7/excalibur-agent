package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func testRequest() providers.ChatRequest {
	return providers.ChatRequest{
		Model: "gemini-test",
		Messages: []providers.Message{
			{Role: providers.RoleSystem, Content: "be brief"},
			{Role: providers.RoleUser, Content: "hi"},
		},
		Tools: []providers.ToolDefinition{{
			Name:        "read_file",
			Description: "read a file",
			Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
		MaxTokens:       128,
		ReasoningEffort: "low",
		ToolChoice:      "auto",
	}
}

func okBody() string {
	return `{"candidates":[{"content":{"role":"model","parts":[` +
		`{"text":"hello"},` +
		`{"functionCall":{"name":"read_file","args":{"path":"a.go"}}}]},` +
		`"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":120,` +
		`"cachedContentTokenCount":40,"candidatesTokenCount":30,` +
		`"thoughtsTokenCount":7,"totalTokenCount":150}}`
}

func TestChatSuccess(t *testing.T) {
	var got wireRequest
	var gotKey, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Goog-Api-Key")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		fmt.Fprint(w, okBody())
	}))
	defer srv.Close()

	p := New(Options{APIKey: "k", BaseURL: srv.URL})
	resp, err := p.Chat(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if gotKey != "k" {
		t.Fatalf("key header = %q", gotKey)
	}
	if gotPath != "/v1beta/models/gemini-test:generateContent" {
		t.Fatalf("path = %s", gotPath)
	}
	if strings.Contains(gotQuery, "key=") {
		t.Fatalf("key must not travel in the URL query: %s", gotQuery)
	}
	if got.SystemInstruction == nil || len(got.SystemInstruction.Parts) != 1 ||
		got.SystemInstruction.Parts[0].Text != "be brief" {
		t.Fatalf("system: %+v", got.SystemInstruction)
	}
	if len(got.Contents) != 1 || got.Contents[0].Role != "user" {
		t.Fatalf("contents: %+v", got.Contents)
	}
	if len(got.Tools) != 1 || len(got.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("tools: %+v", got.Tools)
	}
	if got.Tools[0].FunctionDeclarations[0].Name != "read_file" {
		t.Fatalf("decl: %+v", got.Tools[0].FunctionDeclarations[0])
	}
	if got.GenerationConfig == nil || got.GenerationConfig.MaxOutputTokens != 128 ||
		got.GenerationConfig.ThinkingConfig == nil || got.GenerationConfig.ThinkingConfig.ThinkingBudget != 512 {
		t.Fatalf("generationConfig: %+v", got.GenerationConfig)
	}
	if resp.Message.Content != "hello" {
		t.Fatalf("content = %q", resp.Message.Content)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool calls: %+v", resp.Message.ToolCalls)
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "call_0" || tc.Name != "read_file" || string(tc.Arguments) != `{"path":"a.go"}` {
		t.Fatalf("tool call: %+v", tc)
	}
	want := providers.Usage{Input: 120, CacheRead: 40, CacheWrite: 0, Output: 30, Reasoning: 7}
	if resp.Usage != want {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestChatStableOrder(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		raw = string(body)
		fmt.Fprint(w, okBody())
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	if _, err := p.Chat(context.Background(), testRequest()); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	// Fixed key order keeps the implicit prefix cache hot: tools and
	// system come before the growing contents.
	order := []string{"systemInstruction", "contents", "tools", "generationConfig"}
	last := -1
	for _, k := range order {
		i := strings.Index(raw, `"`+k+`"`)
		if i < 0 {
			t.Fatalf("wire body missing %q", k)
		}
		if i < last {
			t.Fatalf("wire key %q out of order", k)
		}
		last = i
	}
}

func TestChatFunctionResponseRoundTrip(t *testing.T) {
	var got wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, okBody())
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	req := providers.ChatRequest{
		Model: "m",
		Messages: []providers.Message{
			{Role: providers.RoleUser, Content: "read it"},
			{Role: providers.RoleAssistant, Content: "on it", ToolCalls: []providers.ToolCall{{
				ID: "c0", Name: "read_file", Arguments: json.RawMessage(`{"path":"x"}`),
			}}},
			{Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "c0", Name: "read_file", Output: "data"}},
		},
	}
	if _, err := p.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(got.Contents) != 3 {
		t.Fatalf("contents: %+v", got.Contents)
	}
	last := got.Contents[2]
	if last.Role != "user" || len(last.Parts) != 1 || last.Parts[0].FunctionResponse == nil ||
		last.Parts[0].FunctionResponse.Name != "read_file" {
		t.Fatalf("functionResponse: %+v", last)
	}
}

func TestChatRetryThenSuccess(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":{"code":503,"message":"overloaded","status":"UNAVAILABLE"}}`)
			return
		}
		fmt.Fprint(w, okBody())
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

func TestChatErrorKinds(t *testing.T) {
	cases := []struct {
		status string
		kind   providers.ErrorKind
		calls  int
	}{
		{`{"error":{"code":401,"message":"bad key","status":"UNAUTHENTICATED"}}`, providers.KindAuth, 1},
		{`{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`, providers.KindInput, 1},
		{`{"error":{"code":429,"message":"slow","status":"RESOURCE_EXHAUSTED"}}`, providers.KindRateLimited, 3},
	}
	for i, c := range cases {
		calls := 0
		code := http.StatusInternalServerError
		switch c.kind {
		case providers.KindAuth:
			code = http.StatusUnauthorized
		case providers.KindInput:
			code = http.StatusBadRequest
		case providers.KindRateLimited:
			code = http.StatusTooManyRequests
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(code)
			fmt.Fprint(w, c.status)
		}))
		p := New(Options{BaseURL: srv.URL, MaxAttempts: 3})
		_, err := p.Chat(context.Background(), testRequest())
		srv.Close()
		var perr *providers.ProviderError
		if !errors.As(err, &perr) || perr.Kind != c.kind {
			t.Fatalf("case %d: kind = %v", i, err)
		}
		if calls != c.calls {
			t.Fatalf("case %d: calls = %d, want %d", i, calls, c.calls)
		}
	}
}

func TestChatTerminalSignals(t *testing.T) {
	cases := []struct {
		body string
		kind providers.ErrorKind
	}{
		{`{"candidates":[],"promptFeedback":{"blockReason":"SAFETY"}}`, providers.KindContentRejected},
		{`{"candidates":[{"content":{"role":"model","parts":[{"text":"x"}]},"finishReason":"MAX_TOKENS"}]}`, providers.KindTruncated},
		{`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"RECITATION"}]}`, providers.KindContentRejected},
		{`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"MALFORMED_FUNCTION_CALL"}]}`, providers.KindInput},
	}
	for i, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, c.body)
		}))
		p := New(Options{BaseURL: srv.URL})
		_, err := p.Chat(context.Background(), testRequest())
		srv.Close()
		var perr *providers.ProviderError
		if !errors.As(err, &perr) || perr.Kind != c.kind {
			t.Fatalf("case %d: err = %v", i, err)
		}
	}
}

func TestChatThinkingMapping(t *testing.T) {
	var got wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, okBody())
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	for effort, budget := range map[string]int{"low": 512, "medium": 2048, "high": 8192, "1024": 1024} {
		req := testRequest()
		req.ReasoningEffort = effort
		if _, err := p.Chat(context.Background(), req); err != nil {
			t.Fatalf("%s: %v", effort, err)
		}
		if got.GenerationConfig == nil || got.GenerationConfig.ThinkingConfig.ThinkingBudget != budget {
			t.Fatalf("%s: %+v", effort, got.GenerationConfig)
		}
	}
	req := testRequest()
	req.ReasoningEffort = "bogus"
	if _, err := p.Chat(context.Background(), req); err == nil {
		t.Fatal("bogus effort must fail")
	}
	plain := providers.ChatRequest{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}}}
	var gotPlain wireRequest
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotPlain)
		fmt.Fprint(w, okBody())
	}))
	defer srv2.Close()
	p2 := New(Options{BaseURL: srv2.URL})
	if _, err := p2.Chat(context.Background(), plain); err != nil {
		t.Fatalf("plain: %v", err)
	}
	if gotPlain.GenerationConfig != nil {
		t.Fatalf("no knobs set: generationConfig must be omitted, got %+v", gotPlain.GenerationConfig)
	}
}

func TestChatCancellation(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, okBody())
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
		t.Fatalf("calls = %d", calls)
	}
}

func TestChatValidation(t *testing.T) {
	p := New(Options{})
	bad := []providers.ChatRequest{
		{Model: "m"},
		{Model: "m", Messages: []providers.Message{{Role: providers.RoleAssistant, Content: "x"}}},
		{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}}, ToolChoice: "any"},
		{Model: "m", Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}}, ReasoningEffort: "bogus"},
		{Model: "m", Messages: []providers.Message{{Role: "alien", Content: "x"}}},
		{Model: "m", Messages: []providers.Message{
			{Role: providers.RoleAssistant, Content: "x"},
			{Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "c", Output: "o"}},
		}},
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
}

func sseChunk(text, fname, args string, usage string) string {
	parts := ""
	if text != "" {
		parts += fmt.Sprintf(`{"text":%q}`, text)
	}
	if fname != "" {
		if parts != "" {
			parts += ","
		}
		parts += fmt.Sprintf(`{"functionCall":{"name":%q,"args":%s}}`, fname, args)
	}
	return "data: " + fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[%s]},"finishReason":"STOP"}]%s}`, parts, usage) + "\n\n"
}

func TestStreamSuccess(t *testing.T) {
	usage := `,"usageMetadata":{"promptTokenCount":60,"cachedContentTokenCount":20,"candidatesTokenCount":9,"totalTokenCount":69}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":streamGenerateContent") || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("url = %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk("hel", "", "", ""))
		fmt.Fprint(w, sseChunk("lo", "read_file", `{"path":"a.go"}`, ""))
		fmt.Fprint(w, sseChunk("", "exec", `{"cmd":"ls"}`, usage))
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
	var text string
	var calls []providers.ToolCallDelta
	var usageEv *providers.Usage
	done := false
	for _, ev := range events {
		text += ev.Delta
		if ev.ToolCallDelta != nil {
			calls = append(calls, *ev.ToolCallDelta)
		}
		if ev.Usage != nil {
			usageEv = ev.Usage
		}
		if ev.Done {
			done = true
		}
	}
	if text != "hello" {
		t.Fatalf("text = %q", text)
	}
	if len(calls) != 2 {
		t.Fatalf("calls: %+v", calls)
	}
	if calls[0].Index != 0 || calls[0].ID != "call_0" || calls[0].Name != "read_file" ||
		calls[0].Arguments != `{"path":"a.go"}` {
		t.Fatalf("call 0: %+v", calls[0])
	}
	if calls[1].Index != 1 || calls[1].ID != "call_1" || calls[1].Name != "exec" {
		t.Fatalf("call 1: %+v", calls[1])
	}
	if usageEv == nil || *usageEv != (providers.Usage{Input: 60, CacheRead: 20, Output: 9}) {
		t.Fatalf("usage = %+v", usageEv)
	}
	if !done {
		t.Fatal("stream must end with Done")
	}
}

func TestStreamBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+`{"candidates":[],"promptFeedback":{"blockReason":"SAFETY"}}`+"\n\n")
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL})
	var n int
	err := p.Stream(context.Background(), testRequest(), func(providers.StreamEvent) error {
		n++
		return nil
	})
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || perr.Kind != providers.KindContentRejected {
		t.Fatalf("err = %v", err)
	}
	if n != 1 { // the usage event is still delivered before the error
		t.Fatalf("events = %d", n)
	}
}

func TestStreamEmitAbort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk("x", "", "", ""))
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
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, `{"error":{"code":503,"message":"busy","status":"UNAVAILABLE"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseChunk("x", "", "", ""))
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
		fmt.Fprint(w, sseChunk("x", "", "", ""))
		fmt.Fprint(w, "data: {invalid-json\n\n")
	}))
	defer srv.Close()

	p := New(Options{BaseURL: srv.URL, MaxAttempts: 3})
	var n int
	err := p.Stream(context.Background(), testRequest(), func(providers.StreamEvent) error {
		n++
		return nil
	})
	if err == nil || calls != 1 {
		t.Fatalf("mid-stream failure must not retry: err=%v calls=%d", err, calls)
	}
	var perr *providers.ProviderError
	if !errors.As(err, &perr) || !perr.Retryable() {
		t.Fatalf("transport failure must stay retryable-classified: %v", err)
	}
	if n != 1 {
		t.Fatalf("events = %d, want 1 (text only, garbage kills the stream)", n)
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
