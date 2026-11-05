# Provider implementation coverage (H1)

Real coverage of language-model adapters against the wave-0
`internal/providers` contract. Planned rows stay marked planned: never
describe as implemented what is only planned.

## Shared helpers

| Item | Status |
|---|---|
| `internal/providers/openaiwire` Chat Completions client (request build, SSE streaming, tool-call fragment reassembly, usage mapping, `reasoning_effort` / `reasoning.effort` / omit modes, `max_tokens` vs `max_completion_tokens` rendering, normalized errors, `Retry-After` honoring, `NormalizeUsage`, `Cost`) | Implemented, unit-tested |
| `internal/providers` retry helper (`WithRetry`/`Backoff`/`ParseRetryAfter`/`SleepContext`) | Implemented, unit-tested |
| `internal/providers/registry` factory with `SecretResolver` vault hook | Implemented, unit-tested |

## Adapters

| Provider | Package | API / protocol | Status | Cache and token notes (11.6) | Reasoning effort | Prices |
|---|---|---|---|---|---|---|
| OpenAI | `internal/providers/openai` | Chat Completions (default) and Responses (`Options.UseResponses`) | Implemented, contract-tested vs simulated servers, one live check (`-tags live`, `gpt-6-luna`, low effort) | Chat: automatic cache from 1024 tokens, usage via `prompt_tokens_details.cached_tokens`. Responses: `prompt_cache_key` stable per (role, scope) via `Options.PromptCacheKey`; usage via `input_tokens_details.cached_tokens` | Sent as `reasoning_effort` (chat) or `reasoning.effort` (Responses); fixed per call so the prefix stays stable | Usage reported in the price-table shape (`openaiwire.Cost` hook); the versioned table itself lands with the gateway (H3) |
| DeepSeek | `internal/providers/deepseek` | OpenAI-compatible Chat Completions (`https://api.deepseek.com`) | Implemented, contract-tested vs simulated servers | Automatic prefix cache. `prompt_cache_hit_tokens` maps to cache read; `prompt_cache_miss_tokens` maps to cache write (tokens computed at full price that populate the cache). Newer `prompt_tokens_details.cached_tokens` also honored | Omitted on the wire: reasoning is selected by model id (`deepseek-reasoner`). `reasoning_content` deltas are drained, not surfaced as answer text | Same hook as above |
| OpenRouter | `internal/providers/openrouter` | OpenAI-compatible Chat Completions (`https://openrouter.ai/api/v1`) | Implemented, contract-tested vs simulated servers | Depends on the backing provider; usage as reported is passed through, never estimated. Attribution via `X-Title` / `HTTP-Referer` options | Sent as `reasoning.effort` | Same hook as above |
| Ollama (local) | `internal/providers/ollama` | Local OpenAI-compatible API (`OLLAMA_BASE_URL` > `OLLAMA_HOST` + `/v1` > `http://localhost:11434/v1`) | Implemented, contract-tested vs simulated servers | Server prefix cache; no credentials; missing usage yields zero usage, never an estimate. `CostUSD` reports zero | Dropped: behavior is selected by model id on the server | Zero (local inference) |
| Generic OpenAI-compatible | `internal/providers/generic` | Chat Completions at a configured base URL (vLLM, llama.cpp, Qwen, GLM, Kimi, ...) | Implemented, contract-tested vs simulated servers | As the server reports it; missing usage yields zero usage | Passed through as `reasoning_effort`; servers that do not know the field ignore it | Usage reported; rates come from the gateway table (H3) |
| Anthropic | `internal/providers/anthropic` | Messages (`POST {base}/v1/messages`), headers `X-Api-Key`, `anthropic-version: 2023-06-01` | Implemented, contract-tested vs simulated servers, one tiny live check (`-tags live`) | Explicit `cache_control` marks on the last system block, the last tool definition and the last two message blocks (max 4); TTL from `ChatRequest.CacheTTL` (`5m` default, `1h`). Usage: `input_tokens` to input, `cache_read_input_tokens` to cache read, `cache_creation_input_tokens` to cache write, `output_tokens` to output | `ReasoningEffort` `low` 1024, `medium` 4096, `high` 8192 tokens, or a numeric budget (>= 1024); budget >= max tokens is a `KindInput` error | Usage reported; rates come from the gateway table (H3) |
| Google Gemini | `internal/providers/gemini` | `POST {base}/v1beta/models/{model}:generateContent` and `:streamGenerateContent?alt=sse`; key via the `x-goog-api-key` header, never in the URL query | Implemented, contract-tested vs simulated servers, one tiny live check (`-tags live`) | Implicit prefix cache: fixed wire order (tools, system, contents), no per-call variables. Usage: `promptTokenCount` to input, `cachedContentTokenCount` to cache read, `candidatesTokenCount` to output, `thoughtsTokenCount` to reasoning; cache write is 0 | `low` 512, `medium` 2048, `high` 8192 tokens, or a numeric budget (>= 0); `generationConfig` omitted entirely when no knob is set | Usage reported; rates come from the gateway table (H3) |

See `docs/decisions/0006-provider-adapter-choices.md` (OpenAI-wire
choices, w1-providers-a) and
`docs/decisions/0007-providers-anthropic-gemini.md` (Anthropic/Gemini
choices including the additive `ChatRequest.CacheTTL`,
w1-providers-b).

## Verification evidence

- `go test ./internal/providers/... -race`: contract tests per adapter
  (text, streaming with fragmented tool-call arguments, usage
  including cache read/write, every error class, client-side
  validation, cancellation) plus `openaiwire` unit tests
  (SSE splitting, retry-after parsing, usage clamping, cost math)
  and retry-helper bounds.
- Anthropic/Gemini specifics: cache-mark count bound and TTL
  default/override; thinking mapping; tool-result round trips; retry
  then success (streams retry only before the first event);
  terminal signals (refusal, max_tokens, safety blocks,
  `MALFORMED_FUNCTION_CALL`); registry key order (vault, env,
  config) and `GOOGLE_API_KEY` fallback.
- Live: `EXCALIBUR_LIVE=1 go test -tags live
  ./internal/providers/openai/` (one short chat call, low effort;
  never runs under plain `go test`). During wave w1-providers-a the
  live call proved `gpt-6-luna` rejects `max_tokens`, so the OpenAI
  adapter renders the limit as `max_completion_tokens`.
- Credential resolution in this wave: explicit option, else the
  provider environment variable (registry: vault hook, env, config).
  Vault wiring (20.1) arrives in H10; the gateway passes the
  resolved key down.
