// Package openaiwire implements the shared OpenAI Chat Completions
// wire client used by the OpenAI-compatible providers of milestone H1
// (OpenAI chat mode, DeepSeek, OpenRouter, Ollama and generic).
//
// It covers request building from the single internal format of
// internal/providers, server-sent-events streaming with reconstruction
// of fragmented tool-call arguments, usage mapping that keeps the
// ATIF/OpenTelemetry GenAI convention (input includes cached tokens;
// cache read and cache write are subsets of input), reasoning effort
// passthrough styles, normalized errors with retry classes and
// cancellation via context.Context.
//
// Retry execution and provider failover live in the model gateway
// (internal/models, H3): this client performs a single attempt per
// call and reports RetryAfter so the gateway can honor Retry-After.
//
// Architecture reference: Sections 19.2, 19.5, 19.6 and 11.6.
package openaiwire
