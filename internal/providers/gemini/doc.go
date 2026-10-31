// Package gemini adapts the Google Gemini API (AI Studio) to the
// providers.Provider contract.
//
// Wire mapping (Section 19.2): system messages become
// `system_instruction`; user messages stay `user`, assistant messages
// become `model`; tool calls and results become `functionCall` and
// `functionResponse` parts. Tool definitions become
// `functionDeclarations`; ToolChoice maps to `function_calling_config`
// modes. Streaming over `streamGenerateContent` reassembles one
// ToolCallDelta per `functionCall` part with a stable per-stream
// index, id and name. Usage maps `usageMetadata` onto Usage
// (Section 19.6), with `thoughtsTokenCount` as reasoning tokens;
// Gemini caches implicitly by stable prefix (Section 11.6), so the
// adapter sends no cache marks and never injects per-call variables.
// Reasoning effort maps to `thinkingConfig.thinkingBudget`. Failures
// map to normalized ProviderError kinds (Section 19.5) preferring
// the API `status` field and honoring Retry-After; context
// cancellation returns the context error itself.
package gemini
