package openaiwire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// ReasonMode selects how ChatRequest.ReasoningEffort reaches the wire.
// Reasoning effort is fixed per role because changing it breaks the
// input cache on some providers (Section 19.2).
type ReasonMode int

const (
	// ReasonOmit drops the effort: the model is reasoning-capable by
	// id (DeepSeek) or the server has no such field (Ollama).
	ReasonOmit ReasonMode = iota
	// ReasonEffort sends {"reasoning_effort": effort} (OpenAI,
	// generic OpenAI-compatible servers).
	ReasonEffort
	// ReasonObject sends {"reasoning": {"effort": effort}}
	// (OpenRouter).
	ReasonObject
)

// Options configures the shared Chat Completions client. Provider
// packages fill the defaults (base URL, key environment, headers,
// reasoning style); callers override per deployment.
type Options struct {
	// Provider names the adapter in normalized errors.
	Provider string
	// BaseURL is the API root; "/chat/completions" is appended.
	BaseURL string
	// APIKey is sent as Bearer auth when non-empty.
	APIKey string
	// HTTPClient performs requests; nil means a client without a
	// timeout, so callers must bound calls with context deadlines.
	HTTPClient *http.Client
	// ExtraHeaders are sent on every request (e.g. OpenRouter
	// referral headers, organization scoping).
	ExtraHeaders map[string]string
	// Reasoning selects the reasoning-effort wire style.
	Reasoning ReasonMode
	// UseMaxCompletionTokens renders ChatRequest.MaxTokens as
	// max_completion_tokens instead of max_tokens. Newer OpenAI
	// reasoning models reject max_tokens; the compatible long tail
	// (DeepSeek, OpenRouter passthrough, Ollama, generic servers)
	// expects max_tokens.
	UseMaxCompletionTokens bool
}

// Client is a shared OpenAI-compatible Chat Completions client. The
// zero value is invalid; use New. A Client is safe for concurrent use.
type Client struct {
	provider            string
	baseURL             string
	apiKey              string
	http                *http.Client
	headers             map[string]string
	reason              ReasonMode
	maxCompletionTokens bool
}

// New validates options and returns a client.
func New(o Options) (*Client, error) {
	if o.Provider == "" {
		return nil, &providers.ProviderError{
			Kind: providers.KindInput,
			Err:  fmt.Errorf("openaiwire: provider name is required"),
		}
	}
	if o.BaseURL == "" {
		return nil, &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: o.Provider,
			Err:      fmt.Errorf("openaiwire: base URL is required"),
		}
	}
	httpClient := o.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	headers := make(map[string]string, len(o.ExtraHeaders))
	for k, v := range o.ExtraHeaders {
		headers[k] = v
	}
	return &Client{
		provider:            o.Provider,
		baseURL:             strings.TrimRight(o.BaseURL, "/"),
		apiKey:              o.APIKey,
		http:                httpClient,
		headers:             headers,
		reason:              o.Reasoning,
		maxCompletionTokens: o.UseMaxCompletionTokens,
	}, nil
}

// endpoint returns the chat completions URL.
func (c *Client) endpoint() string {
	return c.baseURL + "/chat/completions"
}

// applyHeaders sets auth and extra headers on req.
func (c *Client) applyHeaders(req *http.Request, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
}

// buildRequest renders the wire body, validating client-side input
// errors (empty model, bad tool schemas) before any network use.
func (c *Client) buildRequest(req providers.ChatRequest, stream bool) (*chatRequest, error) {
	if req.Model == "" {
		return nil, &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: c.provider,
			Err:      fmt.Errorf("openaiwire: model is required"),
		}
	}
	msgs, err := buildMessages(req.Messages)
	if err != nil {
		if perr, ok := err.(*providers.ProviderError); ok {
			perr.Provider = c.provider
			perr.Model = req.Model
			return nil, perr
		}
		return nil, err
	}
	out := &chatRequest{
		Model:    req.Model,
		Messages: msgs,
		Tools:    buildTools(req.Tools),
		Stream:   stream,
	}
	if req.ToolChoice != "" {
		out.ToolChoice = req.ToolChoice
	}
	if req.MaxTokens > 0 {
		if c.maxCompletionTokens {
			out.MaxCompletionTokens = req.MaxTokens
		} else {
			out.MaxTokens = req.MaxTokens
		}
	}
	switch c.reason {
	case ReasonEffort:
		if req.ReasoningEffort != "" {
			out.ReasoningEffort = req.ReasoningEffort
		}
	case ReasonObject:
		if req.ReasoningEffort != "" {
			out.Reasoning = &wireReasoning{Effort: req.ReasoningEffort}
		}
	case ReasonOmit:
	}
	if stream {
		out.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	return out, nil
}

// post sends body and returns the response for status handling. HTTP
// transport failures classify as transient unless the context ended
// first, in which case the context error wins.
func (c *Client) post(ctx context.Context, body *chatRequest, stream bool) (*http.Response, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: c.provider,
			Model:    body.Model,
			Err:      fmt.Errorf("openaiwire: encode request: %w", err),
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), &buf)
	if err != nil {
		return nil, netError(c.provider, body.Model, err, ctx.Err())
	}
	c.applyHeaders(req, stream)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, netError(c.provider, body.Model, err, ctx.Err())
	}
	return resp, nil
}

// readFailure drains a failure body for the normalized error.
func readFailure(resp *http.Response) []byte {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody+256))
	return body
}

// Chat performs one non-streaming call.
func (c *Client) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	body, err := c.buildRequest(req, false)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	if ctx.Err() != nil {
		return providers.ChatResponse{}, ctx.Err()
	}
	resp, err := c.post(ctx, body, false)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return providers.ChatResponse{}, normalizeHTTPError(c.provider, req.Model, resp.StatusCode, resp.Header, readFailure(resp))
	}
	defer resp.Body.Close()
	var decoded chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		if ctx.Err() != nil {
			return providers.ChatResponse{}, ctx.Err()
		}
		return providers.ChatResponse{}, &providers.ProviderError{
			Kind:     providers.KindTransient,
			Provider: c.provider,
			Model:    req.Model,
			Err:      fmt.Errorf("openaiwire: decode response: %w", err),
		}
	}
	if len(decoded.Choices) == 0 {
		return providers.ChatResponse{}, &providers.ProviderError{
			Kind:     providers.KindTransient,
			Provider: c.provider,
			Model:    req.Model,
			Err:      fmt.Errorf("openaiwire: response carries no choices"),
		}
	}
	choice := decoded.Choices[0]
	msg := providers.Message{Role: providers.RoleAssistant, Content: choice.Message.Content}
	for _, tc := range choice.Message.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, providers.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		})
	}
	model := decoded.Model
	if model == "" {
		model = req.Model
	}
	out := providers.ChatResponse{
		Message: msg,
		Usage:   toUsage(decoded.Usage),
		Model:   model,
	}
	if kind, bad := finishKind(choice.FinishReason); bad {
		return out, &providers.ProviderError{
			Kind:     kind,
			Provider: c.provider,
			Model:    model,
			Err:      fmt.Errorf("openaiwire: finish reason %q", choice.FinishReason),
		}
	}
	if choice.Message.Refusal != "" {
		return out, &providers.ProviderError{
			Kind:     providers.KindContentRejected,
			Provider: c.provider,
			Model:    model,
			Err:      fmt.Errorf("openaiwire: model refused: %s", choice.Message.Refusal),
		}
	}
	return out, nil
}

// toolFrag accumulates one streamed tool call by index.
type toolFrag struct {
	index int
	id    string
	name  string
	args  strings.Builder
}

// Stream performs one streaming call. Text deltas emit as they arrive;
// tool-call fragments are buffered and emitted once per call with
// complete id, name and arguments, so consumers only ever see whole
// calls. A usage event (when the server reports it) precedes the final
// done event. A truncating or refusing finish returns the assembled
// events plus a typed error.
func (c *Client) Stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	body, err := c.buildRequest(req, true)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	resp, err := c.post(ctx, body, true)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return normalizeHTTPError(c.provider, req.Model, resp.StatusCode, resp.Header, readFailure(resp))
	}
	defer resp.Body.Close()

	var text strings.Builder
	frags := map[int]*toolFrag{}
	finish := ""
	var usage *wireUsage
	refused := false

	flushErr := EachSSEData(resp.Body, func(data string) error {
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// Tolerate heartbeat or vendor-extension payloads.
			return nil
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
			d := choice.Delta
			if d.Refusal != "" {
				refused = true
			}
			// reasoning_content carries chain-of-thought on
			// reasoning models; it is not answer text and the
			// internal format has no slot for it, so it is
			// skipped while still draining the stream.
			if d.Content != "" {
				text.WriteString(d.Content)
				if err := emit(providers.StreamEvent{Delta: d.Content}); err != nil {
					return err
				}
			}
			for _, tc := range d.ToolCalls {
				f, ok := frags[tc.Index]
				if !ok {
					f = &toolFrag{index: tc.Index}
					frags[tc.Index] = f
				}
				if tc.ID != "" {
					f.id = tc.ID
				}
				if tc.Function != nil {
					if tc.Function.Name != "" {
						f.name = tc.Function.Name
					}
					f.args.WriteString(tc.Function.Arguments)
				}
			}
		}
		return nil
	})
	if flushErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return flushErr
	}

	// Emit whole tool calls ordered by index for determinism.
	indices := make([]int, 0, len(frags))
	for i := range frags {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		f := frags[i]
		if err := emit(providers.StreamEvent{ToolCallDelta: &providers.ToolCallDelta{
			Index:     f.index,
			ID:        f.id,
			Name:      f.name,
			Arguments: f.args.String(),
		}}); err != nil {
			return err
		}
	}
	if usage != nil {
		u := toUsage(usage)
		if err := emit(providers.StreamEvent{Usage: &u}); err != nil {
			return err
		}
	}
	if err := emit(providers.StreamEvent{Done: true}); err != nil {
		return err
	}
	if kind, bad := finishKind(finish); bad {
		return &providers.ProviderError{
			Kind:     kind,
			Provider: c.provider,
			Model:    req.Model,
			Err:      fmt.Errorf("openaiwire: finish reason %q", finish),
		}
	}
	if refused {
		return &providers.ProviderError{
			Kind:     providers.KindContentRejected,
			Provider: c.provider,
			Model:    req.Model,
			Err:      fmt.Errorf("openaiwire: model refused"),
		}
	}
	return nil
}

// EachSSEData splits a server-sent-events body into data payloads,
// joining multi-line data fields per the SSE rule, skipping comments
// and dispatching on blank lines. The "[DONE]" terminator ends the
// scan without invoking fn. Malformed lines are ignored so keep-alive
// comments never fail a stream.
func EachSSEData(body io.Reader, fn func(data string) error) error {
	return EachSSEEvent(body, func(_ string, data string) error {
		return fn(data)
	})
}

// EachSSEEvent is EachSSEData with the event name attached. The event
// name is the last "event:" line before the dispatching blank line, or
// empty for anonymous data lines. The "[DONE]" data terminator ends
// the scan without invoking fn.
func EachSSEEvent(body io.Reader, fn func(event, data string) error) error {
	reader := bufio.NewReader(body)
	var dataLines []string
	event := ""
	flush := func() error {
		if len(dataLines) == 0 {
			event = ""
			return nil
		}
		data := strings.Join(dataLines, "\n")
		ev := event
		dataLines = nil
		event = ""
		if data == "[DONE]" {
			return errStreamDone{}
		}
		return fn(ev, data)
	}
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if err := flush(); err != nil {
				if _, done := err.(errStreamDone); done {
					return nil
				}
				return err
			}
		} else if strings.HasPrefix(line, ":") {
			// Comment/heartbeat: ignore.
		} else if rest, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimLeft(rest, " ")
		} else if rest, ok := strings.CutPrefix(line, "data:"); ok {
			payload := strings.TrimLeft(rest, " ")
			if payload == "[DONE]" {
				return nil
			}
			dataLines = append(dataLines, payload)
		}
		if err != nil {
			if err == io.EOF {
				if ferr := flush(); ferr != nil {
					if _, done := ferr.(errStreamDone); done {
						return nil
					}
					return ferr
				}
				return nil
			}
			return fmt.Errorf("openaiwire: read stream: %w", err)
		}
	}
}

// errStreamDone ends an SSE scan at the terminator.
type errStreamDone struct{}

func (errStreamDone) Error() string { return "openaiwire: stream done" }
