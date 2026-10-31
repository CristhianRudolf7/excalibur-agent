// Package openrouter adapts the OpenRouter Chat Completions API to the
// providers.Provider contract: streaming chat, tool calls, usage
// including cached input, reasoning effort, normalized errors and
// cancellation via context.Context.
//
// The API is OpenAI-compatible and shares the wire through
// internal/providers/openaiwire with the OpenRouter reasoning style:
// effort travels as {"reasoning": {"effort": effort}}. Usage follows
// the OpenAI shape with cached tokens in prompt_tokens_details. Cache
// behavior depends on the backing provider (Section 19.2); usage as
// reported is passed through, never estimated.
//
// Credentials resolve from Options.APIKey, else OPENROUTER_API_KEY.
// Optional AppURL/AppName set the HTTP-Referer/X-Title headers
// OpenRouter uses for attribution.
//
// Architecture reference: Sections 19.2, 19.5, 19.6 and 11.6.
package openrouter
