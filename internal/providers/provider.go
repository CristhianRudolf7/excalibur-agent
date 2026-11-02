package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Role is a message role in the single internal format.
type Role string

// Message roles.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is one model-requested tool invocation.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is the executed outcome of one tool call.
type ToolResult struct {
	CallID  string `json:"call_id"`
	Name    string `json:"name"`
	Output  string `json:"output"`
	IsError bool   `json:"is_error"`
}

// ImageInput is an image URL or base64 data URL for a multimodal turn.
// Currently supported by the OpenAI Responses adapter.
type ImageInput struct {
	URL string `json:"url"`
}

// Message is one turn message in the single internal format that every
// adapter translates to and from its wire protocol (Section 19.2).
type Message struct {
	Images     []ImageInput `json:"images,omitempty"`
	Role       Role         `json:"role"`
	Content    string       `json:"content"`
	ToolCalls  []ToolCall   `json:"tool_calls,omitempty"`
	ToolResult *ToolResult  `json:"tool_result,omitempty"`
}

// ToolDefinition describes one callable tool for the model.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
}

// ChatRequest is one model call: messages, tools and call-scoped
// settings. Reasoning effort is fixed per role because changing it
// breaks the cache on some providers (Section 19.2).
type ChatRequest struct {
	Model           string           `json:"model"`
	Messages        []Message        `json:"messages"`
	Tools           []ToolDefinition `json:"tools,omitempty"`
	MaxTokens       int              `json:"max_tokens,omitempty"`
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
	ToolChoice      string           `json:"tool_choice,omitempty"`
	// CacheTTL selects the prompt-cache time-to-live on providers
	// with explicit cache control (Anthropic, Section 11.6): either
	// CacheTTL5m or CacheTTL1h. Empty means the adapter default
	// (CacheTTL5m). Providers with implicit caching ignore it.
	CacheTTL string `json:"cache_ttl,omitempty"`
}

// Supported CacheTTL values for ChatRequest (Section 11.6): a 5-minute
// TTL for machine-paced runs and a 1-hour TTL for the master and
// human-paced conversations.
const (
	CacheTTL5m = "5m"
	CacheTTL1h = "1h"
)

// Usage counts tokens per call. Input includes cached tokens; cache
// read/write are subsets of input, following the ATIF and OpenTelemetry
// GenAI conventions (Section 19.6).
type Usage struct {
	Input      int64 `json:"input"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Output     int64 `json:"output"`
	Reasoning  int64 `json:"reasoning"`
}

// Add accumulates usage.
func (u *Usage) Add(o Usage) {
	u.Input += o.Input
	u.CacheRead += o.CacheRead
	u.CacheWrite += o.CacheWrite
	u.Output += o.Output
	u.Reasoning += o.Reasoning
}

// ChatResponse is one completed model call.
type ChatResponse struct {
	Message Message `json:"message"`
	Usage   Usage   `json:"usage"`
	Model   string  `json:"model"`
}

// ToolCallDelta is a streaming fragment of a tool call: providers that
// stream arguments in pieces (e.g. Responses, Anthropic) are reassembled
// by the adapter into whole ToolCalls before leaving the package.
type ToolCallDelta struct {
	Index     int    `json:"index"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// StreamEvent is one streaming fragment. Usage, when present, carries
// the final totals; Done marks the end of the stream.
type StreamEvent struct {
	Delta         string         `json:"delta,omitempty"`
	ToolCallDelta *ToolCallDelta `json:"tool_call_delta,omitempty"`
	Usage         *Usage         `json:"usage,omitempty"`
	Done          bool           `json:"done"`
}

// ErrorKind classifies provider failures for retry and failover
// (Section 19.5).
type ErrorKind string

// Provider error kinds.
const (
	// KindTransient: 5xx, network failures, timeouts; retry with
	// exponential backoff honoring Retry-After.
	KindTransient ErrorKind = "transient"
	// KindRateLimited: 429; retry honoring Retry-After.
	KindRateLimited ErrorKind = "rate_limited"
	// KindAuth: revoked credentials, denied access; pause and notify.
	KindAuth ErrorKind = "auth"
	// KindInput: invalid arguments or unknown resource; no blind retry.
	KindInput ErrorKind = "input"
	// KindContentRejected: content refusal; retry with more concrete
	// instructions or a larger output limit, then report.
	KindContentRejected ErrorKind = "content_rejected"
	// KindTruncated: output cut by length limits.
	KindTruncated ErrorKind = "truncated"
)

// ProviderError is a normalized provider failure.
type ProviderError struct {
	Kind       ErrorKind
	Provider   string
	Model      string
	RetryAfter time.Duration
	Err        error
}

// Error implements the error interface.
func (e *ProviderError) Error() string {
	return fmt.Sprintf("providers: %s/%s %s: %v", e.Provider, e.Model, e.Kind, e.Err)
}

// Unwrap returns the underlying error.
func (e *ProviderError) Unwrap() error { return e.Err }

// Retryable reports whether the gateway should retry or fail over.
func (e *ProviderError) Retryable() bool {
	return e.Kind == KindTransient || e.Kind == KindRateLimited
}

// Provider is the adapter contract every model provider implements:
// streaming chat, tool calls, usage including cache read/write,
// normalized errors and cancellation via context.Context (H1).
type Provider interface {
	// Name identifies the provider (openai, anthropic, gemini,
	// deepseek, openrouter, ollama, generic).
	Name() string
	// Chat performs one non-streaming call.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	// Stream performs one streaming call, invoking emit per fragment.
	// Returning an error from emit aborts the stream. Adapters reassemble
	// streamed tool-call fragments into whole calls.
	Stream(ctx context.Context, req ChatRequest, emit func(StreamEvent) error) error
	// Close releases provider resources.
	Close() error
}
