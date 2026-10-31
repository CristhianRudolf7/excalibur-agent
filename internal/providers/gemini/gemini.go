package gemini

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

// Defaults for the Gemini API.
const (
	DefaultBaseURL = "https://generativelanguage.googleapis.com"
	// DefaultAPIPath is the API version prefix.
	DefaultAPIPath = "v1beta"
)

// Reasoning budgets in thinking tokens per effort level. Values are
// initial and calibrated later.
var thinkingBudgets = map[string]int{
	"low":    512,
	"medium": 2048,
	"high":   8192,
}

// Options configures the Gemini adapter.
type Options struct {
	// APIKey is the credential, sent via the x-goog-api-key header
	// so it never lands in URLs or access logs.
	APIKey string
	// BaseURL overrides the API endpoint (tests, proxies).
	BaseURL string
	// Model is the default model when the call sets none.
	Model string
	// HTTPClient overrides the default client (30s timeout).
	HTTPClient *http.Client
	// MaxAttempts bounds one call including the first try.
	MaxAttempts int
}

// Provider is a providers.Provider over the Gemini API.
type Provider struct {
	key      string
	base     string
	model    string
	client   *http.Client
	maxTries int
}

// New returns a Gemini provider.
func New(o Options) *Provider {
	base := o.BaseURL
	if base == "" {
		base = DefaultBaseURL
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
		model:    o.Model,
		client:   client,
		maxTries: maxTries,
	}
}

// Name implements providers.Provider.
func (p *Provider) Name() string { return "gemini" }

// Close implements providers.Provider.
func (p *Provider) Close() error { return nil }

// endpoint builds the action URL for a model.
func (p *Provider) endpoint(model, action string) string {
	return fmt.Sprintf("%s/%s/models/%s:%s", p.base, DefaultAPIPath, model, action)
}

// Chat implements providers.Provider.
func (p *Provider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return providers.ChatResponse{}, err
	}
	body, model, err := p.buildBody(req)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	var resp wireResponse
	err = providers.WithRetry(ctx, p.maxTries, providers.DefaultBaseDelay, func(ctx context.Context) error {
		r, perr := p.post(ctx, p.endpoint(model, "generateContent"), body, "application/json")
		if perr != nil {
			return perr
		}
		defer r.Body.Close()
		return decodeResponse(r, &resp, p.Name(), model)
	})
	if err != nil {
		return providers.ChatResponse{}, err
	}
	return translateResponse(resp, p.Name())
}

// Stream implements providers.Provider. Retries happen only before
// the first event is emitted; a failure mid-stream is returned so
// the gateway can fail over without duplicating output.
func (p *Provider) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	body, model, err := p.buildBody(req)
	if err != nil {
		return err
	}
	url := p.endpoint(model, "streamGenerateContent") + "?alt=sse"
	for attempt := 1; ; attempt++ {
		emitted := false
		serr := p.streamOnce(ctx, url, body, func(ev providers.StreamEvent) error {
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

// post sends one request.
func (p *Provider) post(ctx context.Context, url string, body []byte, accept string) (*http.Response, *providers.ProviderError) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", accept)
	if p.key != "" {
		httpReq.Header.Set("X-Goog-Api-Key", p.key)
	}
	resp, herr := p.client.Do(httpReq)
	if herr != nil {
		if ctx.Err() != nil {
			return nil, &providers.ProviderError{Kind: providers.KindTransient, Provider: p.Name(), Err: ctx.Err()}
		}
		// TLS verification failures are deployment configuration
		// errors (KindInput, no retry); anything else is transient.
		return nil, &providers.ProviderError{Kind: providers.TransportKind(herr), Provider: p.Name(), Err: herr}
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, mapHTTPError(resp, p.Name(), "")
	}
	return resp, nil
}

// mapHTTPError translates an HTTP failure into a normalized
// ProviderError, preferring the API status field when present.
func mapHTTPError(resp *http.Response, provider, model string) *providers.ProviderError {
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
	kind := providers.KindTransient
	switch wire.Error.Status {
	case "RESOURCE_EXHAUSTED":
		kind = providers.KindRateLimited
	case "UNAUTHENTICATED", "PERMISSION_DENIED":
		kind = providers.KindAuth
	case "INVALID_ARGUMENT", "NOT_FOUND":
		kind = providers.KindInput
	case "":
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			kind = providers.KindRateLimited
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			kind = providers.KindAuth
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			kind = providers.KindInput
		}
	}
	return &providers.ProviderError{
		Kind:       kind,
		Provider:   provider,
		Model:      model,
		RetryAfter: providers.ParseRetryAfter(resp.Header),
		Err:        fmt.Errorf("%s", msg),
	}
}

// thinkingBudget maps ReasoningEffort to a thinking budget. It
// returns (0, false) when thinking stays at the provider default.
func thinkingBudget(effort string) (int, bool, error) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "", "off", "none":
		return 0, false, nil
	}
	if b, ok := thinkingBudgets[strings.ToLower(strings.TrimSpace(effort))]; ok {
		return b, true, nil
	}
	b, err := strconv.Atoi(strings.TrimSpace(effort))
	if err != nil || b < 0 {
		return 0, false, &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: "gemini",
			Err:      fmt.Errorf("invalid reasoning_effort %q: want off, low, medium, high or >= 0 tokens", effort),
		}
	}
	return b, true, nil
}

// buildBody translates one ChatRequest to the Gemini wire format.
// The output order is fixed (tools, system, contents) and carries no
// per-call variables, keeping the implicit prefix cache hot.
func (p *Provider) buildBody(req providers.ChatRequest) ([]byte, string, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	if model == "" {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("no model set")}
	}
	wire := wireRequest{}
	var system []string
	for _, m := range req.Messages {
		switch m.Role {
		case providers.RoleSystem:
			if m.Content != "" {
				system = append(system, m.Content)
			}
		default:
			content, ok, err := translateMessage(m)
			if err != nil {
				return nil, "", err
			}
			if !ok {
				continue
			}
			if n := len(wire.Contents); n > 0 && wire.Contents[n-1].Role == content.Role {
				wire.Contents[n-1].Parts = append(wire.Contents[n-1].Parts, content.Parts...)
			} else {
				wire.Contents = append(wire.Contents, content)
			}
		}
	}
	if len(wire.Contents) == 0 {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("no messages")}
	}
	if wire.Contents[0].Role != "user" {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("first message must be user")}
	}
	if len(system) > 0 {
		wire.SystemInstruction = &wireContent{Role: "system", Parts: []wirePart{{Text: strings.Join(system, "\n\n")}}}
	}
	if len(req.Tools) > 0 {
		decls := make([]wireFunctionDecl, 0, len(req.Tools))
		for _, t := range req.Tools {
			params := t.Schema
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object"}`)
			}
			var probe any
			if jerr := json.Unmarshal(params, &probe); jerr != nil {
				return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("tool %q: invalid schema: %w", t.Name, jerr)}
			}
			decls = append(decls, wireFunctionDecl{Name: t.Name, Description: t.Description, Parameters: params})
		}
		wire.Tools = []wireTool{{FunctionDeclarations: decls}}
		if tc, terr := translateToolChoice(req.ToolChoice); terr != nil {
			return nil, "", terr
		} else if tc != nil {
			wire.ToolConfig = &wireToolConfig{FunctionCallingConfig: *tc}
		}
	} else if strings.TrimSpace(req.ToolChoice) != "" {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: fmt.Errorf("tool_choice without tools")}
	}
	var gen wireGenerationConfig
	hasGen := false
	if req.MaxTokens > 0 {
		gen.MaxOutputTokens = req.MaxTokens
		hasGen = true
	}
	if budget, enabled, berr := thinkingBudget(req.ReasoningEffort); berr != nil {
		return nil, "", berr
	} else if enabled {
		gen.ThinkingConfig = &wireThinking{ThinkingBudget: budget}
		hasGen = true
	}
	if hasGen {
		wire.GenerationConfig = &gen
	}
	out, merr := json.Marshal(wire)
	if merr != nil {
		return nil, "", &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: merr}
	}
	return out, model, nil
}

// translateMessage converts one internal message to Gemini content.
// It reports ok=false for messages with no wire content.
func translateMessage(m providers.Message) (wireContent, bool, error) {
	fail := func(err error) (wireContent, bool, error) {
		return wireContent{}, false, &providers.ProviderError{Kind: providers.KindInput, Provider: "gemini", Err: err}
	}
	switch m.Role {
	case providers.RoleUser:
		if m.ToolResult != nil {
			if m.ToolResult.Name == "" {
				return fail(fmt.Errorf("tool result needs a tool name"))
			}
			return wireContent{Role: "user", Parts: []wirePart{{
				FunctionResponse: &wireFunctionResponse{
					Name:     m.ToolResult.Name,
					Response: map[string]any{"output": m.ToolResult.Output},
				},
			}}}, true, nil
		}
		if m.Content == "" {
			return wireContent{}, false, nil
		}
		return wireContent{Role: "user", Parts: []wirePart{{Text: m.Content}}}, true, nil
	case providers.RoleTool:
		if m.ToolResult == nil {
			if m.Content == "" {
				return wireContent{}, false, nil
			}
			return wireContent{Role: "user", Parts: []wirePart{{Text: m.Content}}}, true, nil
		}
		if m.ToolResult.Name == "" {
			return fail(fmt.Errorf("tool result needs a tool name"))
		}
		return wireContent{Role: "user", Parts: []wirePart{{
			FunctionResponse: &wireFunctionResponse{
				Name:     m.ToolResult.Name,
				Response: map[string]any{"output": m.ToolResult.Output},
			},
		}}}, true, nil
	case providers.RoleAssistant:
		var parts []wirePart
		if m.Content != "" {
			parts = append(parts, wirePart{Text: m.Content})
		}
		for _, c := range m.ToolCalls {
			args := c.Arguments
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			var probe any
			if jerr := json.Unmarshal(args, &probe); jerr != nil {
				return fail(fmt.Errorf("tool call %q: invalid arguments JSON: %w", c.ID, jerr))
			}
			parts = append(parts, wirePart{FunctionCall: &wireFunctionCall{Name: c.Name, Args: args}})
		}
		if len(parts) == 0 {
			return wireContent{}, false, nil
		}
		return wireContent{Role: "model", Parts: parts}, true, nil
	default:
		return fail(fmt.Errorf("unknown role %q", m.Role))
	}
}

// translateToolChoice maps the contract ToolChoice to the
// function_calling_config form.
func translateToolChoice(choice string) (*wireCallingConfig, error) {
	switch trimmed := strings.ToLower(strings.TrimSpace(choice)); trimmed {
	case "", "auto":
		return nil, nil
	case "any", "required":
		return &wireCallingConfig{Mode: "ANY"}, nil
	case "none":
		return &wireCallingConfig{Mode: "NONE"}, nil
	default:
		return &wireCallingConfig{Mode: "ANY", AllowedFunctionNames: []string{strings.TrimSpace(choice)}}, nil
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
		return mapHTTPError(&http.Response{
			Status:     resp.Status,
			StatusCode: resp.StatusCode,
			Header:     resp.Header,
			Body:       io.NopCloser(bytes.NewReader(raw)),
		}, provider, model)
	}
	if jerr := json.Unmarshal(raw, out); jerr != nil {
		return &providers.ProviderError{Kind: providers.KindTransient, Provider: provider, Model: model, Err: jerr}
	}
	return nil
}

// finishKind maps terminal finish reasons to error kinds. Empty and
// STOP-like reasons are success.
func finishKind(reason string) providers.ErrorKind {
	switch reason {
	case "SAFETY", "RECITATION", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII", "LANGUAGE", "OTHER":
		return providers.KindContentRejected
	case "MAX_TOKENS":
		return providers.KindTruncated
	case "MALFORMED_FUNCTION_CALL":
		return providers.KindInput
	default:
		return ""
	}
}

// translateResponse converts a wire response to the internal format.
func translateResponse(resp wireResponse, provider string) (providers.ChatResponse, error) {
	if len(resp.Candidates) == 0 {
		reason := ""
		if resp.PromptFeedback != nil {
			reason = resp.PromptFeedback.BlockReason
		}
		if reason == "" {
			reason = "empty response"
		}
		return providers.ChatResponse{}, &providers.ProviderError{
			Kind:     providers.KindContentRejected,
			Provider: provider,
			Err:      fmt.Errorf("blocked: %s", reason),
		}
	}
	cand := resp.Candidates[0]
	if kind := finishKind(cand.FinishReason); kind != "" {
		return providers.ChatResponse{}, &providers.ProviderError{
			Kind:     kind,
			Provider: provider,
			Err:      fmt.Errorf("finish reason %s", cand.FinishReason),
		}
	}
	var msg providers.Message
	msg.Role = providers.RoleAssistant
	for _, part := range cand.Content.Parts {
		switch {
		case part.FunctionCall != nil:
			args := part.FunctionCall.Args
			if len(args) == 0 {
				args = json.RawMessage(`{}`)
			}
			var probe any
			if jerr := json.Unmarshal(args, &probe); jerr != nil {
				return providers.ChatResponse{}, &providers.ProviderError{
					Kind:     providers.KindInput,
					Provider: provider,
					Err:      fmt.Errorf("function call %q: invalid args JSON: %w", part.FunctionCall.Name, jerr),
				}
			}
			msg.ToolCalls = append(msg.ToolCalls, providers.ToolCall{
				Name:      part.FunctionCall.Name,
				Arguments: args,
			})
		case part.Text != "":
			msg.Content += part.Text
		}
	}
	// ToolCall IDs are stream-scoped in Gemini; non-streaming calls
	// get stable per-response IDs so results can reference them.
	for i := range msg.ToolCalls {
		msg.ToolCalls[i].ID = fmt.Sprintf("call_%d", i)
	}
	u := resp.UsageMetadata
	return providers.ChatResponse{
		Message: msg,
		Usage: providers.Usage{
			Input:      u.PromptTokenCount,
			CacheRead:  u.CachedContentTokenCount,
			CacheWrite: 0, // implicit cache reports no write tokens
			Output:     u.CandidatesTokenCount,
			Reasoning:  u.ThoughtsTokenCount,
		},
	}, nil
}

// streamOnce runs one SSE attempt. Each SSE data payload is a whole
// GenerateContentResponse; text accumulates as deltas and each
// functionCall part becomes one ToolCallDelta with a stable index.
func (p *Provider) streamOnce(ctx context.Context, url string, body []byte, emit func(providers.StreamEvent) error) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return &providers.ProviderError{Kind: providers.KindInput, Provider: p.Name(), Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.key != "" {
		httpReq.Header.Set("X-Goog-Api-Key", p.key)
	}
	resp, herr := p.client.Do(httpReq)
	if herr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &providers.ProviderError{Kind: providers.TransportKind(herr), Provider: p.Name(), Err: herr}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return mapHTTPError(resp, p.Name(), "")
	}
	var usage providers.Usage
	var terminal *providers.ProviderError
	callIndex := 0
	scanErr := scanSSE(resp.Body, func(data []byte) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var chunk wireResponse
		if jerr := json.Unmarshal(data, &chunk); jerr != nil {
			return streamErr(jerr)
		}
		if chunk.UsageMetadata.TotalTokenCount > 0 || chunk.UsageMetadata.PromptTokenCount > 0 {
			u := chunk.UsageMetadata
			usage = providers.Usage{
				Input:     u.PromptTokenCount,
				CacheRead: u.CachedContentTokenCount,
				Output:    u.CandidatesTokenCount,
				Reasoning: u.ThoughtsTokenCount,
			}
		}
		if len(chunk.Candidates) == 0 {
			if chunk.PromptFeedback != nil && chunk.PromptFeedback.BlockReason != "" {
				terminal = &providers.ProviderError{
					Kind:     providers.KindContentRejected,
					Provider: p.Name(),
					Err:      fmt.Errorf("blocked: %s", chunk.PromptFeedback.BlockReason),
				}
			}
			return nil
		}
		for _, cand := range chunk.Candidates[:1] {
			if kind := finishKind(cand.FinishReason); kind != "" {
				terminal = &providers.ProviderError{
					Kind:     kind,
					Provider: p.Name(),
					Err:      fmt.Errorf("finish reason %s", cand.FinishReason),
				}
			}
			for _, part := range cand.Content.Parts {
				switch {
				case part.FunctionCall != nil:
					args := part.FunctionCall.Args
					if len(args) == 0 {
						args = json.RawMessage(`{}`)
					}
					ev := providers.StreamEvent{ToolCallDelta: &providers.ToolCallDelta{
						Index:     callIndex,
						ID:        fmt.Sprintf("call_%d", callIndex),
						Name:      part.FunctionCall.Name,
						Arguments: string(args),
					}}
					callIndex++
					if serr := emit(ev); serr != nil {
						return emitAbort(serr)
					}
				case part.Text != "":
					if serr := emit(providers.StreamEvent{Delta: part.Text}); serr != nil {
						return emitAbort(serr)
					}
				}
			}
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
	if uerr := emit(providers.StreamEvent{Usage: &usage}); uerr != nil {
		return uerr
	}
	if terminal != nil {
		return terminal
	}
	if derr := emit(providers.StreamEvent{Done: true}); derr != nil {
		return derr
	}
	return nil
}

// scanSSE reads a Server-Sent Events stream, calling fn per data
// payload. Blank lines dispatch; comment lines are skipped and a
// "[DONE]" sentinel ends the stream.
func scanSSE(r io.Reader, fn func(data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8<<20)
	var payload []string
	flush := func() error {
		defer func() { payload = nil }()
		if len(payload) == 0 {
			return nil
		}
		raw := strings.Join(payload, "\n")
		if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "[DONE]" {
			return nil
		}
		return fn([]byte(raw))
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
			if strings.TrimSpace(name) == "data" {
				payload = append(payload, strings.TrimPrefix(value, " "))
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

// streamDecodeError marks a malformed SSE payload as a transport
// failure, retryable only before the first event.
type streamDecodeError struct{ err error }

func (e *streamDecodeError) Error() string { return e.err.Error() }
func (e *streamDecodeError) Unwrap() error { return e.err }

func streamErr(err error) error { return &streamDecodeError{err: err} }

// Wire types for the Gemini API. Only the fields Excalibur uses are
// modeled.

type wirePart struct {
	Text             string                `json:"text,omitempty"`
	FunctionCall     *wireFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *wireFunctionResponse `json:"functionResponse,omitempty"`
}

type wireFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type wireFunctionResponse struct {
	Name     string `json:"name"`
	Response any    `json:"response,omitempty"`
}

type wireContent struct {
	Role  string     `json:"role"`
	Parts []wirePart `json:"parts"`
}

type wireFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireTool struct {
	FunctionDeclarations []wireFunctionDecl `json:"functionDeclarations"`
}

type wireCallingConfig struct {
	Mode                 string   `json:"mode"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type wireToolConfig struct {
	FunctionCallingConfig wireCallingConfig `json:"functionCallingConfig"`
}

type wireThinking struct {
	ThinkingBudget int `json:"thinkingBudget"`
}

type wireGenerationConfig struct {
	MaxOutputTokens int           `json:"maxOutputTokens,omitempty"`
	ThinkingConfig  *wireThinking `json:"thinkingConfig,omitempty"`
}

type wireRequest struct {
	SystemInstruction *wireContent          `json:"systemInstruction,omitempty"`
	Contents          []wireContent         `json:"contents"`
	Tools             []wireTool            `json:"tools,omitempty"`
	ToolConfig        *wireToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *wireGenerationConfig `json:"generationConfig,omitempty"`
}

type wireUsage struct {
	PromptTokenCount        int64 `json:"promptTokenCount"`
	CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
	CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
	ThoughtsTokenCount      int64 `json:"thoughtsTokenCount"`
	TotalTokenCount         int64 `json:"totalTokenCount"`
}

type wireCandidate struct {
	Content      wireContent `json:"content"`
	FinishReason string      `json:"finishReason,omitempty"`
}

type wirePromptFeedback struct {
	BlockReason string `json:"blockReason,omitempty"`
}

type wireResponse struct {
	Candidates     []wireCandidate     `json:"candidates,omitempty"`
	PromptFeedback *wirePromptFeedback `json:"promptFeedback,omitempty"`
	UsageMetadata  wireUsage           `json:"usageMetadata"`
}

type wireError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}
