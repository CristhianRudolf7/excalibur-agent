// Package ollama adapts the local Ollama OpenAI-compatible API to the
// providers.Provider contract: streaming chat, tool calls, usage,
// normalized errors and cancellation via context.Context.
//
// The server prefix cache is fed by the same stable prompt prefix the
// compiler builds for every provider (Section 11.6); no cache fields
// are sent. Reasoning effort is dropped: model behavior is selected
// by model id on the server, and the OpenAI-compatible endpoint
// accepts no effort field. Costs are zero: inference is local.
// Ollama omits usage details on some paths; missing usage yields zero
// usage, never an estimate. No credential is needed.
//
// The base URL resolves from Options.BaseURL, else OLLAMA_BASE_URL,
// else OLLAMA_HOST with "/v1" appended, else the local default.
//
// Architecture reference: Sections 19.2, 19.5, 19.6 and 11.6.
package ollama
