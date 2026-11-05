# H1 — Model providers

Contract and backend tests for the implemented model adapters (Sections 19.2,
19.5, 19.6 and 11.6). The shared contract below applies to all adapters.
Production configuration and wiring are described in [wire-engine.md](wire-engine.md).

## Contract

- **Packages**: `internal/providers/openaiwire` (shared Chat
  Completions client) and one subpackage per provider:
  `internal/providers/{openai,deepseek,openrouter,ollama,generic}`
; `internal/providers/anthropic`,
  `internal/providers/gemini`, `internal/providers/registry`, plus
  the shared `internal/providers/retry.go`.
  Each adapter implements `providers.Provider`: `Name`, `Chat`,
  `Stream`, `Close`.
- **Single internal format**: every adapter translates the
  `providers.Message` / `ToolDefinition` / `ChatRequest` format to
  its wire and back. Assistant tool arguments must be valid JSON;
  tool results require a call id; unknown roles are client-side
  input errors raised before any network use. Additive H1 change,
  recorded in
  `docs/decisions/0007-providers-anthropic-gemini.md`:
  `ChatRequest.CacheTTL` (`""`, `"5m"`, `"1h"`, default `"5m"`)
  for Anthropic explicit cache breakpoints (Section 11.6);
  providers with implicit caching ignore it.
- **Streaming**: text deltas emit as they arrive. Streamed tool-call
  fragments (split `arguments` across chunks, out-of-order indices)
  are reassembled by the adapter and emitted once per call with
  complete id, name and arguments, ordered by index, so only whole
  calls leave the package. A usage event (when the server reports
  usage) precedes the final done event. Anthropic backfills id/name
  from `content_block_start`; Gemini assigns stable per-stream
  `call_<n>` ids.
- **Usage** follows the ATIF/OpenTelemetry GenAI convention: input
  includes cached tokens; cache read and cache write are subsets of
  input; reasoning is a subset of output. Sources: Chat Completions
  `prompt_tokens_details.cached_tokens`,
  `completion_tokens_details.reasoning_tokens`; DeepSeek legacy
  `prompt_cache_hit_tokens` (read) and `prompt_cache_miss_tokens`
  (write); Responses `input_tokens_details.cached_tokens` and
  `output_tokens_details.reasoning_tokens`; Anthropic
  `cache_read_input_tokens` / `cache_creation_input_tokens`; Gemini
  `usageMetadata` incl. `thoughtsTokenCount` reasoning. Missing
  counters yield zero, never estimates; inconsistent counters clamp
  to the invariant. `openaiwire.Cost` turns usage plus a
  gateway-owned rate row into USD: the price-table hook of Section
  19.6.
- **Reasoning effort** is fixed per call (changing it breaks the
  input cache): `reasoning_effort` (OpenAI chat, generic
  passthrough), `reasoning.effort` (OpenRouter, OpenAI Responses),
  omitted (DeepSeek, Ollama). DeepSeek `reasoning_content` deltas
  are drained, not surfaced as answer text. Anthropic maps effort
  to a `thinking` budget (`low` 1024, `medium` 4096, `high` 8192,
  or numeric >= 1024); Gemini to `thinkingBudget` (`low` 512,
  `medium` 2048, `high` 8192, or numeric >= 0).
- **Limits**: `ChatRequest.MaxTokens` renders as
  `max_completion_tokens` on the OpenAI adapter (newer reasoning
  models reject `max_tokens`; verified live) and as `max_tokens`
  elsewhere; Responses uses `max_output_tokens`. Anthropic rejects
  a thinking budget >= max tokens as `KindInput`.
- **Errors** are normalized to `providers.ErrorKind`: 429 is rate
  limited, 5xx/408 transient, 401/403/402 auth, other 4xx input;
  `finish_reason`/`incomplete_details` `length` is truncated and
  `content_filter` (or matching error codes) is content rejected,
  returned together with the partial response. Anthropic maps
  `refusal` to content-rejected and `max_tokens` to truncated;
  Gemini prefers the API `status` (`RESOURCE_EXHAUSTED` to
  rate-limited, `UNAUTHENTICATED`/`PERMISSION_DENIED` to auth,
  `INVALID_ARGUMENT`/`NOT_FOUND` to input; safety/recitation/
  prohibited/blocklist to content-rejected; `MAX_TOKENS` to
  truncated; `MALFORMED_FUNCTION_CALL` to input). `Retry-After`
  (seconds or HTTP date) is honored on retryable errors. Transport
  failures are transient unless the context ended first, in which
  case the context error itself returns so `errors.Is` keeps
  working. Missing credentials fail locally as auth errors without
  touching the network. Malformed 200 payloads are transient.
- **Retry**: adapters perform one attempt and report `RetryAfter`
  for the gateway (H3), except Anthropic/Gemini which retry inside
  the adapter (up to 3 attempts, exponential backoff with jitter,
  `Retry-After` honored; streams retry only before the first
  event).
- **Cancellation** via `context.Context` on every call; context
  errors return unwrapped; no credentials are logged.
- **Credentials**: explicit option first, then the provider
  environment variable (`OPENAI_API_KEY`, `DEEPSEEK_API_KEY`,
  `OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY` then
  `GOOGLE_API_KEY`); Ollama needs none; generic takes its base
  URL (required option) and optional key from configuration via the
  gateway. The registry (`internal/providers/registry`) resolves
  vault hook, env, config in that order (Sections 19.2, 20.1) and
  accepts extra kinds via `Register`. Vault wiring (20.1) arrives
  with H10.
- **Responses extras (OpenAI only)**: `Options.UseResponses`
  switches wires; `Options.PromptCacheKey` sets the stable per
  (role, scope) key; `Options.Organization` sets
  `OpenAI-Organization`; OpenRouter attribution via
  `Options.AppURL`/`AppName`; Ollama base via `OLLAMA_BASE_URL`,
  `OLLAMA_HOST` or the local default.
- **Gemini wire rules**: key via the `x-goog-api-key` header, never
  in the URL query; fixed wire order (tools, system, contents)
  keeps the implicit prefix cache hot; `generationConfig` omitted
  entirely when no knob is set.

## Backend tests

All against `httptest` servers; no test calls a paid API.

- `openaiwire`: chat text + usage mapping (incl. cache read and
  reasoning tokens); reasoning styles (effort/object/omit);
  `max_tokens` vs `max_completion_tokens` rendering; full request
  shape (system/user/assistant+tools/tool-result mapping, empty
  schema default, `tool_choice`); client-side validation matrix
  (empty model, bad role, bad tool args, tool message without
  result/call id); streaming reassembly of two interleaved,
  fragmented tool calls with usage and done ordering; termination
  kinds (`length`, `content_filter` keep emitted events and return
  the typed error); emit-abort propagation; error class table
  (500/502/429+`Retry-After`/401/403/402/400/404/content-filter
  body) with retryability and provider tagging;
  `Retry-After` HTTP-date parsing; malformed-200 and empty-choices
  as transient; cancellation (chat and stream return
  `context.Canceled`); usage clamping; `Cost` math incl. reasoning
  fallback; SSE splitting (comments, multi-line data, `[DONE]`);
  auth and extra headers.
- Per OpenAI-wire adapter (`openai` incl. `responses_test.go`,
  `deepseek`, `openrouter`, `ollama`, `generic`): adapter name;
  chat text with provider usage shape; streaming with a fragmented
  tool call; provider error classes (chat and, where simulated,
  stream); provider reasoning-field assertion (present with the
  right shape or absent); missing-key auth without network traffic;
  cancellation. Plus per-provider specifics: OpenAI Responses
  wire (input/tool mapping, `prompt_cache_key`, reasoning object,
  `max_output_tokens`, delta+added+done reassembly, completed with
  usage, HTTP/incomplete/validation errors, organization header);
  DeepSeek cache hit/miss mapping and `reasoning_content`
  skipping; OpenRouter attribution headers and quota-402;
  Ollama keyless access, base resolution, absent-usage handling,
  zero `CostUSD`; generic required base URL and keyless servers.
- Retry helper bounds (`Backoff`), `Retry-After` parsing (seconds,
  HTTP-date, cap, garbage), success after transients, no retry on
  auth/input, attempt exhaustion, `Retry-After` honored, context
  cancel wins unwrapped.
- `internal/providers/anthropic`: chat success (headers, path,
  system/tools extraction, tool-call and usage mapping);
  cache-mark count bound and TTL default/override; thinking
  mapping incl. invalid budget and budget-vs-max-tokens;
  tool-result round trip incl. `is_error`; retry-then-success;
  `Retry-After` honored; error kinds and attempt counts
  (auth 1, input 1, transient 3); terminal signals
  (refusal, max_tokens); cancellation (no server hit, no
  `ProviderError`); request validation matrix; streaming
  success (text join, fragment identity backfill, usage,
  `Done`); emit abort passthrough; stream retry before first
  event; no retry after first event (still retryable-classified);
  stream cancellation.
- `internal/providers/gemini`: chat success (key header, model
  path, no key in query, system/tools/generationConfig mapping,
  `call_<n>` ids, usage incl. reasoning); wire key order;
  `functionResponse` round trip; retry-then-success; error
  kinds via API `status`; terminal signals (block, `MAX_TOKENS`,
  `RECITATION`, `MALFORMED_FUNCTION_CALL`); thinking mapping
  incl. omitted `generationConfig` when unset; cancellation;
  validation matrix; streaming success (multi-chunk text join,
  stable call indexes, usage, `Done`); blocked stream (usage
  delivered before the error); emit abort; retry rules;
  cancellation.
- `internal/providers/registry`: built-in kinds, register
  validation, custom-kind plug-in (incl. case-insensitivity),
  key order vault, env, config, custom env var, vault error
  naming, Gemini `GOOGLE_API_KEY` fallback, adapter builds,
  unknown/blank kind failures, batch build and batch abort.
- Live (`-tags live`, `EXCALIBUR_LIVE=1`): one short chat call
  each in `internal/providers/openai/live_test.go`,
  `anthropic_live_test.go`, `gemini_live_test.go` using the
  operator credential file. Never runs under plain
  `go test ./...`.

## W6 production wiring (registry + credentials)

W6 registers the OpenAI-family kinds in the same registry
(`openai`, `deepseek`, `openrouter`, `ollama`, `generic`) and adds
the operator layers: `OperatorCredentials` file loader, the
`EXCALIBUR_API_KEY` alias after kind-specific variables, and
fallback-credential resolution with a decorator that retries
quota/rate-limit failures exactly once on the fallback key (never
for auth/input/content; streams only before the first fragment).
The Responses wire is the registry default for `openai` (Chat
Completions rejects effort+tools on reasoning models). The
`[models]` layered config section and the top-level-alias skip in
the env loader live in `internal/config`. Backend tests:
`TestBuiltinKindsAll`, `TestOperatorKeyAlias`,
`TestFallback{KeyResolution,UsedOnlyOnQuotaLike,StreamNoDuplicate}`,
`TestNewWrapsFallback`, `TestLoadOperatorCredentials`. See
`docs/decisions/0035-w6-live-wiring.md`.

## Deferred (not in this wave)

The model gateway itself — roles, routing, escalation, breaker,
versioned price table — (H3/H4/H12); vault-backed credential
resolution (H10); image/video/voice/embeddings/GPU providers
(post-H1).
