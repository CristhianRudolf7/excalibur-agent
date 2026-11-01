package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openaiwire"
)

// DefaultBaseURL is the OpenAI API root.
const DefaultBaseURL = "https://api.openai.com/v1"

// responsesPath is the Responses endpoint appended to the base URL.
const responsesPath = "/responses"

// respTextPart is one text content part of a Responses input item.
type respTextPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// respTool is one Responses function tool definition.
type respTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// respReasoning carries the fixed-per-role reasoning effort.
type respReasoning struct {
	Effort string `json:"effort"`
}

// responsesRequest is the Responses request body.
type responsesRequest struct {
	Model           string         `json:"model"`
	Input           []any          `json:"input"`
	Tools           []respTool     `json:"tools,omitempty"`
	ToolChoice      string         `json:"tool_choice,omitempty"`
	Reasoning       *respReasoning `json:"reasoning,omitempty"`
	MaxOutputTokens int            `json:"max_output_tokens,omitempty"`
	PromptCacheKey  string         `json:"prompt_cache_key,omitempty"`
	ServiceTier     string         `json:"service_tier,omitempty"`
	Stream          bool           `json:"stream"`
}

// ServiceTierEnv selects the OpenAI processing tier ("flex", "default",
// "priority"...). Unset sends no service_tier, so the account default
// applies.
const ServiceTierEnv = "EXCALIBUR_SERVICE_TIER"

// respUsage is the Responses usage payload.
type respUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	OutputTokens       int64 `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

// toUsage maps Responses usage to the internal convention via the
// shared normalizer. Responses reports no cache-write counter, so
// cache writes report zero rather than an estimate.
func toUsage(w *respUsage) providers.Usage {
	if w == nil {
		return providers.Usage{}
	}
	var cached, reasoning int64
	if w.InputTokensDetails != nil {
		cached = w.InputTokensDetails.CachedTokens
	}
	if w.OutputTokensDetails != nil {
		reasoning = w.OutputTokensDetails.ReasoningTokens
	}
	return openaiwire.NormalizeUsage(w.InputTokens, cached, 0, w.OutputTokens, reasoning)
}

// respItem is one Responses output item, decoded in two passes.
type respItem struct {
	Type string `json:"type"`
}

// respMessage is a type=message output item.
type respMessage struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// respFunctionCall is a type=function_call output item. ID carries
// the call_id so follow-up function_call_output items round-trip.
type respFunctionCall struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// respResponse is the non-streaming Responses response.
type respResponse struct {
	ID                string `json:"id"`
	Model             string `json:"model"`
	Status            string `json:"status"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
		Type    string `json:"type"`
	} `json:"error"`
	Output []json.RawMessage `json:"output"`
	Usage  *respUsage        `json:"usage"`
}

// buildInput converts internal messages to Responses input items.
// Validation mirrors the Chat Completions wire: bad tool arguments,
// missing result call IDs and unknown roles are client-side input
// errors raised before any network use.
func buildInput(msgs []providers.Message) ([]any, error) {
	out := make([]any, 0, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case providers.RoleSystem, providers.RoleUser:
			out = append(out, map[string]any{
				"role": string(m.Role),
				"content": []respTextPart{
					{Type: "input_text", Text: m.Content},
				},
			})
		case providers.RoleAssistant:
			if m.Content != "" {
				out = append(out, map[string]any{
					"role": "assistant",
					"content": []map[string]string{
						{"type": "output_text", "text": m.Content},
					},
				})
			}
			for _, tc := range m.ToolCalls {
				args := tc.Arguments
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				if !json.Valid(args) {
					return nil, &providers.ProviderError{
						Kind: providers.KindInput,
						Err:  fmt.Errorf("message %d tool call %q has invalid JSON arguments", i, tc.Name),
					}
				}
				out = append(out, map[string]any{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Name,
					"arguments": string(args),
				})
			}
		case providers.RoleTool:
			if m.ToolResult == nil {
				return nil, &providers.ProviderError{
					Kind: providers.KindInput,
					Err:  fmt.Errorf("message %d has tool role without a tool result", i),
				}
			}
			if m.ToolResult.CallID == "" {
				return nil, &providers.ProviderError{
					Kind: providers.KindInput,
					Err:  fmt.Errorf("message %d tool result misses call id", i),
				}
			}
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": m.ToolResult.CallID,
				"output":  m.ToolResult.Output,
			})
		default:
			return nil, &providers.ProviderError{
				Kind: providers.KindInput,
				Err:  fmt.Errorf("message %d has unknown role %q", i, m.Role),
			}
		}
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}

// buildRespTools converts internal tool definitions to Responses
// function tools.
func buildRespTools(defs []providers.ToolDefinition) []respTool {
	out := make([]respTool, 0, len(defs))
	for _, d := range defs {
		schema := d.Schema
		if len(schema) == 0 {
			schema = json.RawMessage("{}")
		}
		out = append(out, respTool{
			Type:        "function",
			Name:        d.Name,
			Description: d.Description,
			Parameters:  schema,
		})
	}
	return out
}

// respClient posts to the Responses endpoint. It shares error and
// usage normalization with the Chat Completions wire.
type respClient struct {
	provider       string
	baseURL        string
	apiKey         string
	http           *http.Client
	headers        map[string]string
	promptCacheKey string
}

// buildBody renders the Responses request body.
func (c *respClient) buildBody(req providers.ChatRequest, stream bool) (*responsesRequest, error) {
	if req.Model == "" {
		return nil, &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: c.provider,
			Err:      fmt.Errorf("responses: model is required"),
		}
	}
	input, err := buildInput(req.Messages)
	if err != nil {
		if perr, ok := err.(*providers.ProviderError); ok {
			perr.Provider = c.provider
			perr.Model = req.Model
			return nil, perr
		}
		return nil, err
	}
	out := &responsesRequest{
		Model:  req.Model,
		Input:  input,
		Tools:  buildRespTools(req.Tools),
		Stream: stream,
	}
	if req.ToolChoice != "" {
		out.ToolChoice = req.ToolChoice
	}
	if req.MaxTokens > 0 {
		out.MaxOutputTokens = req.MaxTokens
	}
	if req.ReasoningEffort != "" {
		out.Reasoning = &respReasoning{Effort: req.ReasoningEffort}
	}
	if c.promptCacheKey != "" {
		out.PromptCacheKey = c.promptCacheKey
	}
	out.ServiceTier = strings.TrimSpace(os.Getenv(ServiceTierEnv))
	return out, nil
}

// post sends the body and returns the response for status handling.
func (c *respClient) post(ctx context.Context, body *responsesRequest) (*http.Response, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, &providers.ProviderError{
			Kind:     providers.KindInput,
			Provider: c.provider,
			Model:    body.Model,
			Err:      fmt.Errorf("responses: encode request: %w", err),
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+responsesPath, &buf)
	if err != nil {
		return nil, openaiwire.TransportError(c.provider, body.Model, err, ctx.Err())
	}
	req.Header.Set("Content-Type", "application/json")
	if body.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, openaiwire.TransportError(c.provider, body.Model, err, ctx.Err())
	}
	return resp, nil
}

// respMessageOut assembles the assistant message from output items.
func respMessageOut(output []json.RawMessage) (providers.Message, error) {
	msg := providers.Message{Role: providers.RoleAssistant}
	for _, raw := range output {
		var item respItem
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		switch item.Type {
		case "message":
			var m respMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			for _, part := range m.Content {
				if part.Type == "output_text" {
					msg.Content += part.Text
				}
			}
		case "function_call":
			var fc respFunctionCall
			if err := json.Unmarshal(raw, &fc); err != nil {
				continue
			}
			args := fc.Arguments
			if args == "" {
				args = "{}"
			}
			msg.ToolCalls = append(msg.ToolCalls, providers.ToolCall{
				ID:        fc.CallID,
				Name:      fc.Name,
				Arguments: json.RawMessage(args),
			})
		case "reasoning":
			// Chain-of-thought items have no slot in the
			// internal format; reasoning tokens are still
			// counted through usage.
		}
	}
	return msg, nil
}

// statusError maps a terminal Responses status to its error class.
func statusError(provider, model, status, reason string) *providers.ProviderError {
	switch status {
	case "completed":
		return nil
	case "incomplete":
		switch reason {
		case "max_output_tokens":
			return &providers.ProviderError{
				Kind:     providers.KindTruncated,
				Provider: provider,
				Model:    model,
				Err:      fmt.Errorf("responses: incomplete: %s", reason),
			}
		case "content_filter":
			return &providers.ProviderError{
				Kind:     providers.KindContentRejected,
				Provider: provider,
				Model:    model,
				Err:      fmt.Errorf("responses: incomplete: %s", reason),
			}
		default:
			return &providers.ProviderError{
				Kind:     providers.KindTransient,
				Provider: provider,
				Model:    model,
				Err:      fmt.Errorf("responses: incomplete: %s", reason),
			}
		}
	default:
		return &providers.ProviderError{
			Kind:     providers.KindTransient,
			Provider: provider,
			Model:    model,
			Err:      fmt.Errorf("responses: status %q", status),
		}
	}
}

// chat performs one non-streaming Responses call.
func (c *respClient) chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	body, err := c.buildBody(req, false)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	if ctx.Err() != nil {
		return providers.ChatResponse{}, ctx.Err()
	}
	resp, err := c.post(ctx, body)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		failure, _ := io.ReadAll(io.LimitReader(resp.Body, 2304))
		return providers.ChatResponse{}, openaiwire.HTTPError(c.provider, req.Model, resp.StatusCode, resp.Header, failure)
	}
	defer resp.Body.Close()
	var decoded respResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		if ctx.Err() != nil {
			return providers.ChatResponse{}, ctx.Err()
		}
		return providers.ChatResponse{}, &providers.ProviderError{
			Kind:     providers.KindTransient,
			Provider: c.provider,
			Model:    req.Model,
			Err:      fmt.Errorf("responses: decode response: %w", err),
		}
	}
	if decoded.Error != nil {
		kind := providers.KindTransient
		if openaiwire.IsContentRefusal(decoded.Error.Code, decoded.Error.Message) {
			kind = providers.KindContentRejected
		}
		return providers.ChatResponse{}, &providers.ProviderError{
			Kind:     kind,
			Provider: c.provider,
			Model:    req.Model,
			Err:      fmt.Errorf("responses: %s", decoded.Error.Message),
		}
	}
	msg, err := respMessageOut(decoded.Output)
	if err != nil {
		return providers.ChatResponse{}, err
	}
	model := decoded.Model
	if model == "" {
		model = req.Model
	}
	out := providers.ChatResponse{Message: msg, Usage: toUsage(decoded.Usage), Model: model}
	if serr := statusError(c.provider, model, decoded.Status, incompleteReason(decoded.IncompleteDetails)); serr != nil {
		return out, serr
	}
	return out, nil
}

// incompleteReason extracts the incomplete reason, if any.
func incompleteReason(d *struct {
	Reason string `json:"reason"`
}) string {
	if d == nil {
		return ""
	}
	return d.Reason
}

// respFrag accumulates one streamed function call by output index.
type respFrag struct {
	index   int
	id      string
	name    string
	args    strings.Builder
	full    string
	hasFull bool
}

// respTextDelta is a response.output_text.delta payload.
type respTextDelta struct {
	Delta string `json:"delta"`
}

// respArgsDelta is a response.function_call_arguments.delta payload.
type respArgsDelta struct {
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// respItemEvent is a response.output_item.added/done payload.
type respItemEvent struct {
	OutputIndex int             `json:"output_index"`
	Item        json.RawMessage `json:"item"`
}

// respTerminal is the response object of completed/failed events.
type respTerminal struct {
	Response respResponse `json:"response"`
}

// stream performs one streaming Responses call. Text deltas emit as
// they arrive; function calls are buffered and emitted whole, ordered
// by output index, before the usage and done events.
func (c *respClient) stream(ctx context.Context, req providers.ChatRequest, emit func(providers.StreamEvent) error) error {
	body, err := c.buildBody(req, true)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	resp, err := c.post(ctx, body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		failure, _ := io.ReadAll(io.LimitReader(resp.Body, 2304))
		return openaiwire.HTTPError(c.provider, req.Model, resp.StatusCode, resp.Header, failure)
	}
	defer resp.Body.Close()

	frags := map[int]*respFrag{}
	status := ""
	reason := ""
	var usage *respUsage
	var failed *respResponse

	get := func(i int) *respFrag {
		f, ok := frags[i]
		if !ok {
			f = &respFrag{index: i}
			frags[i] = f
		}
		return f
	}

	scanErr := openaiwire.EachSSEEvent(resp.Body, func(event, data string) error {
		switch event {
		case "response.output_text.delta":
			var d respTextDelta
			if err := json.Unmarshal([]byte(data), &d); err != nil || d.Delta == "" {
				return nil
			}
			return emit(providers.StreamEvent{Delta: d.Delta})
		case "response.function_call_arguments.delta":
			var d respArgsDelta
			if err := json.Unmarshal([]byte(data), &d); err != nil {
				return nil
			}
			get(d.OutputIndex).args.WriteString(d.Delta)
			return nil
		case "response.output_item.added":
			var e respItemEvent
			if err := json.Unmarshal([]byte(data), &e); err != nil {
				return nil
			}
			var fc respFunctionCall
			var item respItem
			if json.Unmarshal(e.Item, &item) == nil && item.Type == "function_call" &&
				json.Unmarshal(e.Item, &fc) == nil {
				f := get(e.OutputIndex)
				if fc.CallID != "" {
					f.id = fc.CallID
				}
				if fc.Name != "" {
					f.name = fc.Name
				}
			}
			return nil
		case "response.output_item.done":
			var e respItemEvent
			if err := json.Unmarshal([]byte(data), &e); err != nil {
				return nil
			}
			var item respItem
			if json.Unmarshal(e.Item, &item) != nil || item.Type != "function_call" {
				return nil
			}
			var fc respFunctionCall
			if err := json.Unmarshal(e.Item, &fc); err != nil {
				return nil
			}
			f := get(e.OutputIndex)
			if fc.CallID != "" {
				f.id = fc.CallID
			}
			if fc.Name != "" {
				f.name = fc.Name
			}
			if fc.Arguments != "" {
				f.full = fc.Arguments
				f.hasFull = true
			}
			return nil
		case "response.completed":
			var t respTerminal
			if err := json.Unmarshal([]byte(data), &t); err != nil {
				return nil
			}
			status = t.Response.Status
			reason = incompleteReason(t.Response.IncompleteDetails)
			usage = t.Response.Usage
			return nil
		case "response.failed":
			var t respTerminal
			if err := json.Unmarshal([]byte(data), &t); err != nil {
				return nil
			}
			failed = &t.Response
			status = t.Response.Status
			return nil
		default:
			return nil
		}
	})
	if scanErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return scanErr
	}

	indices := make([]int, 0, len(frags))
	for i := range frags {
		indices = append(indices, i)
	}
	sort.Ints(indices)
	for _, i := range indices {
		f := frags[i]
		args := f.args.String()
		if f.hasFull {
			args = f.full
		}
		if err := emit(providers.StreamEvent{ToolCallDelta: &providers.ToolCallDelta{
			Index:     f.index,
			ID:        f.id,
			Name:      f.name,
			Arguments: args,
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
	if failed != nil && failed.Error != nil {
		kind := providers.KindTransient
		if openaiwire.IsContentRefusal(failed.Error.Code, failed.Error.Message) {
			kind = providers.KindContentRejected
		}
		return &providers.ProviderError{
			Kind:     kind,
			Provider: c.provider,
			Model:    req.Model,
			Err:      fmt.Errorf("responses: %s", failed.Error.Message),
		}
	}
	if status == "" {
		status = "completed"
	}
	if serr := statusError(c.provider, req.Model, status, reason); serr != nil {
		return serr
	}
	return nil
}
