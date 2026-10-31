# 0006 — Provider adapter wire choices (H1, OpenAI-wire wave)

Date: 2026-10-31. Status: accepted.

## Context

Wave `w1-providers-a` implements the OpenAI, DeepSeek, OpenRouter,
Ollama and generic OpenAI-compatible adapters over a shared Chat
Completions helper (`internal/providers/openaiwire`), plus Responses
support for OpenAI. The architecture document fixes the what
(Sections 19.2, 19.5, 19.6, 11.6); several wire details below that
level needed a choice.

## Decisions

1. **`max_completion_tokens` for OpenAI, `max_tokens` elsewhere.**
   The first live call against `gpt-6-luna` failed with HTTP 400:
   `max_tokens` is not supported with this model. The OpenAI
   adapter renders `ChatRequest.MaxTokens` as
   `max_completion_tokens`; all other adapters keep `max_tokens`,
   the field the compatible long tail expects. Alternative
   (send both, or always `max_tokens`): rejected, strict servers
   refuse unknown or deprecated fields.
2. **DeepSeek `prompt_cache_miss_tokens` maps to cache write.**
   Miss tokens are computed at full price and populate the cache,
   so they are the honest write counter. Wires without an explicit
   write counter report zero, never an estimate.
3. **DeepSeek `reasoning_content` is drained, not surfaced.**
   The internal message format has no reasoning slot; leaking
   chain-of-thought into answer text would corrupt tool arguments
   downstream. Reasoning tokens still count through usage.
4. **Reasoning effort omitted for DeepSeek and Ollama.**
   DeepSeek reasons by model id; Ollama by server-side model
   selection. Sending an unknown field risks strict-server
   rejections; dropping is documented per adapter.
5. **Truncation and content refusal return the partial response
   plus a typed error.** Callers keep the assembled events and can
   still attribute usage; the gateway decides retry vs report.
6. **Malformed 200 payloads and empty choice lists are
   transient**, not input: the request was well-formed, the server
   misbehaved, so gateway failover (H3) is the right response.
7. **HTTP 402 (quota exhausted, OpenRouter) maps to auth.**
   Semantically "pause and notify", like revoked credentials — not
   something a blind retry fixes.
8. **No hardcoded per-model prices in adapters.** Rate snapshots
   would rot and could not be verified in this wave. Adapters
   report usage in the price-table shape plus the `Cost` helper;
   the versioned table itself belongs to the gateway (H3).
9. **Generic adapter takes its base URL as a required option**,
   with no invented environment variable: no standard exists, and
   the value comes from user configuration via the gateway.
10. **`prompt_cache_key` is Responses-only.** It is the only wire
    that accepts the field; Chat Completions keeps prefix
    stability through fixed parameter order and per-role effort.

## Consequences

- `openaiwire.Options.UseMaxCompletionTokens` exists so future
  wires can opt in without touching call sites.
- Live check (`-tags live`, `TestLiveChat`) stays chat-mode with
  low effort; it already paid for itself once (decision 1).
