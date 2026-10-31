// Package anthropic adapts the Anthropic Messages API to the
// providers.Provider contract.
//
// Wire mapping (Section 19.2): the internal message list becomes the
// Messages `system` blocks plus `messages`; tool definitions become
// Anthropic tools; tool calls and results become `tool_use` and
// `tool_result` blocks, merged so roles strictly alternate.
// Streaming reassembles `input_json` fragments per content index and
// backfills each fragment's id and name from its `content_block_start`.
// Usage maps `input_tokens`, `output_tokens`,
// `cache_read_input_tokens` and `cache_creation_input_tokens` onto
// Usage (Section 19.6). Explicit `cache_control` marks (Section 11.6,
// at most 4) go on the last system block, the last tool definition
// and the last two message blocks, with the ChatRequest.CacheTTL.
// Reasoning effort maps to the `thinking` budget. Failures map to
// normalized ProviderError kinds (Section 19.5) and honor
// Retry-After; context cancellation returns the context error itself.
package anthropic
