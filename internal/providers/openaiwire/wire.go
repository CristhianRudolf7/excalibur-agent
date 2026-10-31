package openaiwire

import (
	"encoding/json"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// wireMessage is one Chat Completions message on the wire.
type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// wireToolCall is one Chat Completions tool call on the wire.
type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// wireToolDef is one Chat Completions tool definition on the wire.
type wireToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// chatRequest is the Chat Completions request body. ReasoningStyle
// selects which reasoning field is rendered (see ReasonMode). Only
// one of MaxTokens and MaxCompletionTokens is ever set: newer OpenAI
// reasoning models reject max_tokens outright.
type chatRequest struct {
	Model               string         `json:"model"`
	Messages            []wireMessage  `json:"messages"`
	Tools               []wireToolDef  `json:"tools,omitempty"`
	ToolChoice          string         `json:"tool_choice,omitempty"`
	MaxTokens           int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string         `json:"reasoning_effort,omitempty"`
	Reasoning           *wireReasoning `json:"reasoning,omitempty"`
	Stream              bool           `json:"stream"`
	StreamOptions       *streamOptions `json:"stream_options,omitempty"`
}

// wireReasoning is the OpenRouter-style reasoning object.
type wireReasoning struct {
	Effort string `json:"effort"`
}

// streamOptions asks the server to append usage to the stream.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// promptDetails carries cached-input counters.
type promptDetails struct {
	CachedTokens int64 `json:"cached_tokens"`
}

// completionDetails carries reasoning-output counters.
type completionDetails struct {
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

// wireUsage is the union of the usage shapes served by
// OpenAI-compatible endpoints.
type wireUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	// Newer OpenAI/DeepSeek/OpenRouter shape.
	PromptTokensDetails     *promptDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails *completionDetails `json:"completion_tokens_details"`
	// Legacy DeepSeek shape.
	PromptCacheHitTokens  int64 `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int64 `json:"prompt_cache_miss_tokens"`
}

// chatChoice is one non-streaming response choice.
type chatChoice struct {
	Message struct {
		Role      string         `json:"role"`
		Content   string         `json:"content"`
		Refusal   string         `json:"refusal,omitempty"`
		ToolCalls []wireToolCall `json:"tool_calls"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

// chatResponse is the non-streaming Chat Completions response.
type chatResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *wireUsage   `json:"usage"`
}

// streamDelta is the per-chunk delta of a streaming choice.
type streamDelta struct {
	Content          string `json:"content"`
	Refusal          string `json:"refusal,omitempty"`
	Role             string `json:"role,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
	ToolCalls        []struct {
		Index    int    `json:"index"`
		ID       string `json:"id,omitempty"`
		Function *struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		} `json:"function,omitempty"`
	} `json:"tool_calls,omitempty"`
}

// streamChunk is one decoded SSE data payload.
type streamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta        streamDelta `json:"delta"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
}

// wireError is the OpenAI-style error envelope shared by compatible
// servers: {"error": {"message": ..., "type": ..., "code": ...}}.
type wireError struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// buildMessages converts internal messages to wire messages. Tool
// result messages require a call ID; assistant tool-call arguments
// must be valid JSON. Violations are client-side input errors so a
// malformed request never reaches the network.
func buildMessages(msgs []providers.Message) ([]wireMessage, error) {
	out := make([]wireMessage, 0, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case providers.RoleSystem, providers.RoleUser:
			out = append(out, wireMessage{Role: string(m.Role), Content: m.Content})
		case providers.RoleAssistant:
			wm := wireMessage{Role: "assistant", Content: m.Content}
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
				var wtc wireToolCall
				wtc.ID = tc.ID
				wtc.Type = "function"
				wtc.Function.Name = tc.Name
				wtc.Function.Arguments = string(args)
				wm.ToolCalls = append(wm.ToolCalls, wtc)
			}
			out = append(out, wm)
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
			out = append(out, wireMessage{
				Role:       "tool",
				Content:    m.ToolResult.Output,
				ToolCallID: m.ToolResult.CallID,
			})
		default:
			return nil, &providers.ProviderError{
				Kind: providers.KindInput,
				Err:  fmt.Errorf("message %d has unknown role %q", i, m.Role),
			}
		}
	}
	return out, nil
}

// buildTools converts internal tool definitions to wire definitions.
// A nil or empty schema becomes an empty JSON object so the server
// always receives a parameters object.
func buildTools(defs []providers.ToolDefinition) []wireToolDef {
	out := make([]wireToolDef, 0, len(defs))
	for _, d := range defs {
		schema := d.Schema
		if len(schema) == 0 {
			schema = json.RawMessage("{}")
		}
		var wt wireToolDef
		wt.Type = "function"
		wt.Function.Name = d.Name
		wt.Function.Description = d.Description
		wt.Function.Parameters = schema
		out = append(out, wt)
	}
	return out
}
