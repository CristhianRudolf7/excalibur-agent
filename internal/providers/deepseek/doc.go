// Package deepseek adapts the DeepSeek Chat Completions API to the
// providers.Provider contract: streaming chat, tool calls, usage
// including cached input and cache writes, normalized errors and
// cancellation via context.Context.
//
// The API is OpenAI-compatible and shares the wire through
// internal/providers/openaiwire with ReasonMode omitted: reasoning is
// selected by model id (deepseek-reasoner), not by an effort field.
// Reasoning models stream chain-of-thought in reasoning_content; the
// internal message format has no slot for it, so it is drained and
// skipped while reasoning tokens still count through usage. Usage
// reports prompt_cache_hit_tokens (cache read) and
// prompt_cache_miss_tokens (cache write: tokens computed at full
// price that populate the cache).
//
// Credentials resolve from Options.APIKey, else DEEPSEEK_API_KEY.
//
// Architecture reference: Sections 19.2, 19.5, 19.6 and 11.6.
package deepseek
