// Package openai adapts the OpenAI language-model API to the
// providers.Provider contract: streaming chat, tool calls, usage
// including cached input, reasoning effort, normalized errors and
// cancellation via context.Context.
//
// Two wires are supported (Section 19.2): Chat Completions (default,
// shared with the other OpenAI-compatible adapters through
// internal/providers/openaiwire) and Responses (opt-in via
// Options.UseResponses), which additionally accepts a stable
// prompt_cache_key per (role, scope) for the automatic prefix cache
// (Section 11.6). Reasoning effort is fixed per call because changing
// it breaks the cache.
//
// Credentials resolve from Options.APIKey, else the OPENAI_API_KEY
// environment variable. The secrets vault (Section 20.1, H10) plugs
// in at the gateway, which passes the resolved key here.
//
// Architecture reference: Sections 19.2, 19.5, 19.6 and 11.6.
package openai
