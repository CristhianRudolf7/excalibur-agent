package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// Defaults for the Anthropic Messages API.
const (
	DefaultBaseURL = "https://api.anthropic.com"
	DefaultVersion = "2023-06-01"
	// DefaultMaxTokens applies when the request sets none. Anthropic
	// requires max_tokens on every call.
	DefaultMaxTokens = 4096
	// MaxCacheMarks is the provider cap on cache_control breakpoints.
	MaxCacheMarks = 4
)

// Reasoning budgets in thinking tokens per effort level. Values are
// initial and calibrated later.
var thinkingBudgets = map[string]int{
	"low":    1024,
	"medium": 4096,
	"high":   8192,
}

// Options configures the Anthropic adapter.
type Options struct {
	// APIKey is the credential. Empty means unauthenticated calls,
	// which the API rejects with KindAuth.
	APIKey string
	// BaseURL overrides the API endpoint (tests, proxies).
	BaseURL string
	// Version overrides the anthropic-version header.
	Version string
	// Model is the default model when the call sets none.
	Model string
	// HTTPClient overrides the default client (30s timeout).
	HTTPClient *http.Client
	// MaxAttempts bounds one call including the first try.
	MaxAttempts int
}

// Provider is a providers.Provider over the Anthropic Messages API.
type Provider struct {
	key      string
	base     string
	version  string
	model    string
	client   *http.Client
	maxTries int
}

// New returns an Anthropic provider.
func New(o Options) *Provider {
	base := o.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	version := o.Version
	if version == "" {
		version = DefaultVersion
	}
	client := o.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	maxTries := o.MaxAttempts
	if maxTries < 1 {
		maxTries = providers.DefaultMaxAttempts
	}
	return &Provider{
		key:      o.APIKey,
		base:     strings.TrimSuffix(base, "/"),
		version:  version,
		model:    o.Model,
		client:   client,
		maxTries: maxTries,
	}
}

// Name implements providers.Provider.
func (p *Provider) Name() string { return "anthropic" }

// Close implements providers.Provider.
func (p *Provider) Close() error { return nil }

// Chat implements providers.Provider.
func (p *Provider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return providers.ChatResponse{}, err
	}
	body, model, err := p.buildBody(req, false)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	var resp wireResponse
	err = providers.WithRetry(ctx, p.maxTries, providers.DefaultBaseDelay, func(ctx context.Context) error {
		r, perr := p.post(ctx, body)
		if perr != nil {
			return perr
		}
		defer r.Body.Close()
		return decodeResponse(r, &resp, p.Name(), model)
	})
	if err != nil {
		return providers.ChatResponse{}, err
	}
	return translateResponse(resp)
}

// Stream implements providers.Provider. Retries happen only before
// the first event is emitted; a failure mid-stream is returned so
// the gateway can fail over without duplicating output.
func (p *Provider) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	body, _, err := p.buildBody(req, true)
	if err != nil {
		return err
	}
	for attempt := 1; ; attempt++ {
		emitted := false
		serr := p.streamOnce(ctx, body, func(ev providers.StreamEvent) error {
			emitted = true
			return emit(ev)
		})
		if serr == nil {
			return nil
		}
		var abort *emitAbortError
		if errors.As(serr, &abort) {
			return abort.err
		}
		if emitted {
			return serr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		perr, ok := serr.(*providers.ProviderError)
		if !ok || !perr.Retryable() || attempt >= p.maxTries {
			return serr
		}
		wait := perr.RetryAfter
		if wait <= 0 {
			wait = providers.Backoff(providers.DefaultBaseDelay, attempt)
		}
		if werr := providers.SleepContext(ctx, wait); werr != nil {
			return werr
		}
	}
}

// post sends one Messages request.
func (p *Provider) post(ctx context.Context, body []byte) (*http.Response, *providers.ProviderError) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Anthropic-Version", p.version)
	if p.key != "" {
		httpReq.Header.Set("X-Api-Key", p.key)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &providers.ProviderError{Kind: providers.KindTransient, Provider: p.Name(), Err: ctx.Err()}
		}
		return nil, &providers.ProviderError{Kind: providers.KindTransient, Provider: p.Name(), Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, p.mapError(resp)
	}
	return resp, nil
}

// mapError translates an HTTP failure into a normalized ProviderError.
func (p *Provider) mapError(resp *http.Response) *providers.ProviderError {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var wire wireError
	_ = json.Unmarshal(raw, &wire)
	msg := wire.Error.Message
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
	}
	if msg == "" {
		msg = "status " + resp.Status
	}
	err := fmt.Errorf("%s", msg)
	kind := providers.KindTransient
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		kind = providers.KindRateLimited
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		kind = providers.KindAuth
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		kind = providers.KindInput
	}
	return &providers.ProviderError{
		Kind:       kind,
		Provider:   p.Name(),
		RetryAfter: providers.ParseRetryAfter(resp.Header),
		Err:        err,
	}
}

// cacheTTL resolves the requested TTL, defaulting to 5 minutes.
func cacheTTL(req providers.ChatRequest) (string, error) {
	switch req.CacheTTL {
	case "", providers.CacheTTL5m:
		return providers.CacheTTL5m, nil
	case providers.CacheTTL1h:
		return providers.CacheTTL1h, nil
	default:
		return "", &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: "anthropic",
			Err:      fmt.Errorf("invalid cache_ttl %q: want %q or %q", req.CacheTTL, providers.CacheTTL5m, providers.CacheTTL1h),
		}
	}
}

// thinkingBudget maps ReasoningEffort to a thinking budget in tokens.
// It returns 0 when thinking is disabled.
func thinkingBudget(effort string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "", "off", "none":
		return 0, nil
	}
	if b, ok := thinkingBudgets[strings.ToLower(strings.TrimSpace(effort))]; ok {
		return b, nil
	}
	b, err := strconv.Atoi(strings.TrimSpace(effort))
	if err != nil || b < 1024 {
		return 0, &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: "anthropic",
			Err:      fmt.Errorf("invalid reasoning_effort %q: want off, low, medium, high or >= 1024 tokens", effort),
		}
	}
	return b, nil
}

// buildBody translates one ChatRequest to the Messages wire format.
func (p *Provider) buildBody(req providers.ChatRequest, stream bool) ([]byte, string, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("no model set")}
	}
	ttl, err := cacheTTL(req)
	if err != nil {
		return nil, "", err
	}
	budget, err := thinkingBudget(req.ReasoningEffort)
	if err != nil {
		return nil, "", err
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	if budget > 0 && budget >= maxTokens {
		return nil, "", &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: p.Name(),
			Err:      fmt.Errorf("thinking budget %d needs max_tokens above it, got %d", budget, maxTokens),
		}
	}

	var system []wireBlock
	var msgs []wireMessage
	for _, m := range req.Messages {
		switch m.Role {
		case providers.RoleSystem:
			if m.Content == "" {
				continue
			}
			system = append(system, wireBlock{Type: "text", Text: m.Content})
		default:
			block, role, ok, berr := translateMessage(m)
			if berr != nil {
				return nil, "", berr
			}
			if !ok {
				continue
			}
			if n := len(msgs); n > 0 && msgs[n-1].Role == role {
				msgs[n-1].Content = append(msgs[n-1].Content, block...)
			} else {
				msgs = append(msgs, wireMessage{Role: role, Content: block})
			}
		}
	}
	if len(msgs) == 0 {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("no messages")}
	}
	if msgs[0].Role != "user" {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("first message must be user")}
	}

	placeCacheMarks(system, msgs, len(req.Tools) > 0, ttl)

	wire := wireRequest{
		Model:     model,
		System:    system,
		Messages:  msgs,
		MaxTokens: maxTokens,
		Stream:    stream,
	}
	if len(req.Tools) > 0 {
		wire.Tools = make([]wireTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Schema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			var probe any
			if jerr := json.Unmarshal(schema, &probe); jerr != nil {
				return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("tool %q: invalid schema: %w", t.Name, jerr)}
			}
			wire.Tools = append(wire.Tools, wireTool{Name: t.Name, Description: t.Description, InputSchema: schema})
		}
		wire.Tools[len(wire.Tools)-1].CacheControl = &wireCacheControl{Type: "ephemeral", TTL: ttl}
		if tc, terr := translateToolChoice(req.ToolChoice); terr != nil {
			return nil, "", terr
		} else if tc != nil {
			wire.ToolChoice = tc
		}
	} else if strings.TrimSpace(req.ToolChoice) != "" {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("tool_choice without tools")}
	}
	if budget > 0 {
		wire.Thinking = &wireThinking{Type: "enabled", BudgetTokens: budget}
	}
	out, merr := json.Marshal(wire)
	if merr != nil {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: merr}
	}
	return out, model, nil
}

// translateMessage converts one internal message to Anthropic blocks.
// It reports ok=false for messages with no wire content.
func translateMessage(m providers.Message) ([]wireBlock, string, bool, error) {
	switch m.Role {
	case providers.RoleUser:
		if m.ToolResult != nil {
			return []wireBlock{{
				Type:      "tool_result",
				ToolUseID: m.ToolResult.CallID,
				Text:      m.ToolResult.Output,
				IsError:   boolPtr(m.ToolResult.IsError),
			}}, "user", true, nil
		}
		if m.Content == "" {
			return nil, "", false, nil
		}
		return []wireBlock{{Type: "text", Text: m.Content}}, "user", true, nil
	case providers.RoleTool:
		if m.ToolResult == nil {
			if m.Content == "" {
				return nil, "", false, nil
			}
			return []wireBlock{{Type: "text", Text: m.Content}}, "user", true, nil
		}
		return []wireBlock{{
			Type:      "tool_result",
			ToolUseID: m.ToolResult.CallID,
			Text:      m.ToolResult.Output,
			IsError:   boolPtr(m.ToolResult.IsError),
		}}, "user", true, nil
	case providers.RoleAssistant:
		var blocks []wireBlock
		if m.Content != "" {
			blocks = append(blocks, wireBlock{Type: "text", Text: m.Content})
		}
		for _, c := range m.ToolCalls {
			input := c.Arguments
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			var probe any
			if jerr := json.Unmarshal(input, &probe); jerr != nil {
				return nil, "", false, &providers.ProviderError{
					Kind:     providers.KindInput,
					Provider: "anthropic",
					Err:      fmt.Errorf("tool call %q: invalid arguments JSON: %w", c.ID, jerr),
				}
			}
			blocks = append(blocks, wireBlock{Type: "tool_use", ID: c.ID, Name: c.Name, Input: input})
		}
		if len(blocks) == 0 {
			return nil, "", false, nil
		}
		return blocks, "assistant", true, nil
	default:
		return nil, "", false, &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: "anthropic",
			Err:      fmt.Errorf("unknown role %q", m.Role),
		}
	}
}

// placeCacheMarks sets explicit cache_control breakpoints (Section
// 11.6): the last system block, the last tool definition mark is set
// by the caller, and the last two message blocks, capped at
// MaxCacheMarks total.
func placeCacheMarks(system []wireBlock, msgs []wireMessage, hasTools bool, ttl string) {
	marks := 0
	mark := func() *wireCacheControl {
		if marks >= MaxCacheMarks {
			return nil
		}
		marks++
		return &wireCacheControl{Type: "ephemeral", TTL: ttl}
	}
	if len(system) > 0 {
		if cc := mark(); cc != nil {
			system[len(system)-1].CacheControl = cc
		}
	}
	if hasTools {
		mark() // consumed by the last tool definition
	}
	for i := len(msgs) - 1; i >= 0 && marks < MaxCacheMarks; i-- {
		blocks := msgs[i].Content
		if len(blocks) == 0 {
			continue
		}
		// Only the last two messages carry mobile marks.
		if i < len(msgs)-2 {
			break
		}
		last := len(blocks) - 1
		if blocks[last].CacheControl == nil {
			if cc := mark(); cc != nil {
				blocks[last].CacheControl = cc
			}
		}
	}
}

func boolPtr(b bool) *bool {
	if !b {
		return nil
	}
	return &b
}

// translateToolChoice maps the contract ToolChoice to the wire form.
func translateToolChoice(choice string) (*wireToolChoice, error) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "", "auto":
		return nil, nil
	case "any", "required":
		return &wireToolChoice{Type: "any"}, nil
	case "none":
		return &wireToolChoice{Type: "none"}, nil
	default:
		return &wireToolChoice{Type: "tool", Name: strings.TrimSpace(choice)}, nil
	}
}

// decodeResponse reads one non-streaming response or maps its failure.
func decodeResponse(resp *http.Response, out *wireResponse, provider, model string) error {
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return &providers.ProviderError{Kind: providers.KindTransient, Provider: provider, Model: model, Err: err}
	}
	if resp.StatusCode != http.StatusOK {
		var wire wireError
		_ = json.Unmarshal(raw, &wire)
		msg := wire.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		kind := providers.KindTransient
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			kind = providers.KindRateLimited
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			kind = providers.KindAuth
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			kind = providers.KindInput
		}
		return &providers.ProviderError{
			Kind:       kind,
			Provider:   provider,
			Model:      model,
			RetryAfter: providers.ParseRetryAfter(resp.Header),
			Err:        fmt.Errorf("%s", msg),
		}
	}
	if jerr := json.Unmarshal(raw, out); jerr != nil {
		return &providers.ProviderError{Kind: providers.KindTransient, Provider: provider, Model: model, Err: jerr}
	}
	return nil
}

// translateResponse converts a wire response to the internal format.
func translateResponse(resp wireResponse) (providers.ChatResponse, error) {
	switch resp.StopReason {
	case "refusal":
		return providers.ChatResponse{}, &providers.ProviderError{
			Kind:     providers.KindContentRejected,
			Provider: "anthropic",
			Model:    resp.Model,
			Err:      fmt.Errorf("model refused to answer"),
		}
	case "max_tokens":
		return providers.ChatResponse{}, &providers.ProviderError{
			Kind:     providers.KindTruncated,
			Provider: "anthropic",
			Model:    resp.Model,
			Err:      fmt.Errorf("output cut by max_tokens limit"),
		}
	}
	var msg providers.Message
	msg.Role = providers.RoleAssistant
	for _, b := range resp.Content {
		switch b.Type {
		case "text":
			msg.Content += b.Text
		case "tool_use":
			args := b.Input
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			var probe any
			if jerr := json.Unmarshal(args, &probe); jerr != nil {
				return providers.ChatResponse{}, &providers.ProviderError{
					Kind:     providers.KindInput,
					Provider: "anthropic",
					Model:    resp.Model,
					Err:      fmt.Errorf("tool_use %q: invalid input JSON: %w", b.ID, jerr),
				}
			}
			msg.ToolCalls = append(msg.ToolCalls, providers.ToolCall{ID: b.ID, Name: b.Name, Arguments: args})
		default:
			// thinking, redacted_thinking and unknown blocks carry
			// no caller-visible content; their cost stays inside
			// output_tokens.
		}
	}
	return providers.ChatResponse{
		Message: msg,
		Model:   resp.Model,
		Usage: providers.Usage{
			Input:      resp.Usage.InputTokens,
			CacheRead:  resp.Usage.CacheReadInputTokens,
			CacheWrite: resp.Usage.CacheCreationInputTokens,
			Output:     resp.Usage.OutputTokens,
		},
	}, nil
}

// streamOnce runs one SSE attempt, reassembling tool-call fragments.
func (p *Provider) streamOnce(ctx context.Context, body []byte, emit func(providers.StreamEvent) error) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Anthropic-Version", p.version)
	httpReq.Header.Set("Cache-Control", "no-store")
	if p.key != "" {
		httpReq.Header.Set("X-Api-Key", p.key)
	}
	resp, herr := p.client.Do(httpReq)
	if herr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &providers.ProviderError{Kind: providers.KindTransient, Provider: p.Name(), Err: herr}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return p.mapError(resp)
	}
	state := streamState{tools: map[int]*toolCursor{}}
	terminal := ""
	finalUsage := providers.Usage{}
	scanErr := scanSSE(resp.Body, func(event string, data []byte) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		switch event {
		case "message_start":
			var e wireMessageStart
			if jerr := json.Unmarshal(data, &e); jerr != nil {
				return streamErr(jerr)
			}
			finalUsage.Input = e.Message.Usage.InputTokens
			finalUsage.CacheRead = e.Message.Usage.CacheReadInputTokens
			finalUsage.CacheWrite = e.Message.Usage.CacheCreationInputTokens
		case "content_block_start":
			var e wireBlockStart
			if jerr := json.Unmarshal(data, &e); jerr != nil {
				return streamErr(jerr)
			}
			if e.Block.Type == "tool_use" {
				state.tools[e.Index] = &toolCursor{ID: e.Block.ID, Name: e.Block.Name}
			}
		case "content_block_delta":
			var e wireBlockDelta
			if jerr := json.Unmarshal(data, &e); jerr != nil {
				return streamErr(jerr)
			}
			switch e.Delta.Type {
			case "text_delta":
				if e.Delta.Text != "" {
					if serr := emit(providers.StreamEvent{Delta: e.Delta.Text}); serr != nil {
						return emitAbort(serr)
					}
				}
			case "input_json_delta":
				cur, ok := state.tools[e.Index]
				if !ok {
					cur = &toolCursor{}
					state.tools[e.Index] = cur
				}
				cur.JSON += e.Delta.PartialJSON
				if serr := emit(providers.StreamEvent{ToolCallDelta: &providers.ToolCallDelta{
					Index:     e.Index,
					ID:        cur.ID,
					Name:      cur.Name,
					Arguments: e.Delta.PartialJSON,
				}}); serr != nil {
					return emitAbort(serr)
				}
			default:
				// thinking_delta, signature_delta and unknown
				// deltas are reasoning internals, not caller text.
			}
		case "message_delta":
			var e wireMessageDelta
			if jerr := json.Unmarshal(data, &e); jerr != nil {
				return streamErr(jerr)
			}
			finalUsage.Output = e.Usage.OutputTokens
			terminal = e.Delta.StopReason
		case "message_stop", "done":
			terminal = firstNonEmpty(terminal, "end_turn")
		}
		return nil
	})
	if scanErr != nil {
		var abort *emitAbortError
		if errors.As(scanErr, &abort) {
			return scanErr
		}
		var dec *streamDecodeError
		if errors.As(scanErr, &dec) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &providers.ProviderError{Kind: providers.KindTransient, Provider: p.Name(), Err: dec.err}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &providers.ProviderError{Kind: providers.KindTransient, Provider: p.Name(), Err: scanErr}
	}
	if uerr := emit(providers.StreamEvent{Usage: &finalUsage}); uerr != nil {
		return uerr
	}
	switch terminal {
	case "refusal":
		return &providers.ProviderError{Kind: providers.KindContentRejected, Provider: p.Name(), Err: fmt.Errorf("model refused to answer")}
	case "max_tokens":
		return &providers.ProviderError{Kind: providers.KindTruncated, Provider: p.Name(), Err: fmt.Errorf("output cut by max_tokens limit")}
	}
	if derr := emit(providers.StreamEvent{Done: true}); derr != nil {
		return derr
	}
	return nil
}

type streamState struct {
	tools map[int]*toolCursor
}

type toolCursor struct {
	ID   string
	Name string
	JSON string
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// scanSSE reads a Server-Sent Events stream, calling fn per event
// with its type and payload. Blank lines dispatch; comment lines
// (": ...") are skipped.
func scanSSE(r io.Reader, fn func(event string, data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	event := ""
	var payload []string
	flush := func() error {
		defer func() {
			event = ""
			payload = nil
		}()
		if len(payload) == 0 {
			return nil
		}
		raw := strings.Join(payload, "\n")
		if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "[DONE]" {
			return nil
		}
		if event == "" {
			event = "message"
		}
		return fn(event, []byte(raw))
	}
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, ":") {
			continue
		}
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if name, value, found := strings.Cut(line, ":"); found {
			field := strings.TrimSpace(name)
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				event = value
			case "data":
				payload = append(payload, value)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

// emitAbortError marks an emit-callback failure so the stream loop
// returns it unwrapped instead of retrying or normalizing it.
type emitAbortError struct{ err error }

func (e *emitAbortError) Error() string { return e.err.Error() }
func (e *emitAbortError) Unwrap() error { return e.err }

func emitAbort(err error) error { return &emitAbortError{err: err} }

// streamDecodeError marks a malformed SSE payload as a non-retryable
// input-class failure of the transport, not of the request.
type streamDecodeError struct{ err error }

func (e *streamDecodeError) Error() string { return e.err.Error() }
func (e *streamDecodeError) Unwrap() error { return e.err }

func streamErr(err error) error { return &streamDecodeError{err: err} }

// Wire types for the Messages API. Only the fields Excalibur uses
// are modeled.

type wireCacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type wireBlock struct {
	Type         string            `json:"type"`
	Text         string            `json:"text,omitempty"`
	ID           string            `json:"id,omitempty"`
	Name         string            `json:"name,omitempty"`
	Input        json.RawMessage   `json:"input,omitempty"`
	ToolUseID    string            `json:"tool_use_id,omitempty"`
	IsError      *bool             `json:"is_error,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireMessage struct {
	Role    string      `json:"role"`
	Content []wireBlock `json:"content"`
}

type wireTool struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	InputSchema  json.RawMessage   `json:"input_schema"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type wireThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type wireRequest struct {
	Model      string          `json:"model"`
	System     []wireBlock     `json:"system,omitempty"`
	Messages   []wireMessage   `json:"messages"`
	MaxTokens  int             `json:"max_tokens"`
	Stream     bool            `json:"stream,omitempty"`
	Tools      []wireTool      `json:"tools,omitempty"`
	ToolChoice *wireToolChoice `json:"tool_choice,omitempty"`
	Thinking   *wireThinking   `json:"thinking,omitempty"`
}

type wireUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type wireResponse struct {
	Model      string      `json:"model"`
	StopReason string      `json:"stop_reason"`
	Content    []wireBlock `json:"content"`
	Usage      wireUsage   `json:"usage"`
}

type wireError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type wireMessageStart struct {
	Message struct {
		Usage wireUsage `json:"usage"`
	} `json:"message"`
}

type wireBlockStart struct {
	Index int       `json:"index"`
	Block wireBlock `json:"content_block"`
}

type wireBlockDelta struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

type wireMessageDelta struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage wireUsage `json:"usage"`
}
