# 0007: Anthropic and Gemini adapters, retry policy and registry

Date: 2026-10-31. Milestone H1, agent w1-providers-b (Sections 19.2,
19.5, 11.6 and Appendix E row H1).

## What

`internal/providers/anthropic`, `internal/providers/gemini` and
`internal/providers/registry`, plus a shared retry helper in
`internal/providers`.

## Decisions

1. **Additive contract field `ChatRequest.CacheTTL`** (`""`, `"5m"`,
   `"1h"`; default `"5m"`). Section 11.6 requires explicit cache
   breakpoints with a 5-minute TTL for machine-paced runs and a
   1-hour TTL for the master and human-paced conversations. The
   wave-0 contract had no way to express this, so one optional
   string field was added; no existing literal breaks (all use
   field names). Alternatives rejected: a rich breakpoint list
   (the flat message list carries no layer boundaries; the H6
   compiler owns layers, the adapter only places marks).
2. **Adapter-placed cache marks (Anthropic, max 4).** Without layer
   information the adapter marks, in order: the last system block,
   the last tool definition, and the last two message blocks. This
   approximates the Section 11.6 boundaries (L0/L1/L2-L3 stable
   prefix, mobile mark at the end of L4) within what the adapter
   can see. Gemini needs no marks (implicit prefix cache); the
   adapter only keeps the wire order tools, system, messages stable
   and never injects per-call variables.
3. **Retry inside the adapter.** Transient and rate-limited failures
   retry up to 3 attempts total with exponential backoff (500 ms
   base, x2) and jitter, honoring `Retry-After` (seconds or
   HTTP-date, capped at 60 s). Rationale: Section 19.5 puts the
   retry policy on the provider path; the gateway (H3) owns the
   circuit breaker and failover, not per-attempt backoff.
   Streaming retries only before the first event is emitted; after
   that a failure is returned so the gateway can fail over without
   duplicating a partial stream.
4. **Reasoning-effort mapping.** `ReasoningEffort` accepts `"off"` /
   `""` (disabled), `"low"`, `"medium"`, `"high"`, or a plain
   integer budget. Anthropic: low 1024, medium 4096, high 8192
   thinking tokens (1024 is the provider minimum); Gemini:
   low 512, medium 2048, high 8192. Values are initial and
   calibrated later. Conflicts (Anthropic budget >= max tokens)
   are `KindInput` errors, not silent bumps.
5. **Terminal content signals are errors.** Anthropic
   `stop_reason` `refusal` and Gemini safety/recitation blocks map
   to `KindContentRejected`; length truncation maps to
   `KindTruncated`. The wave-0 `ChatResponse` has no partial-output
   field, so per Section 19.5 the gateway retries with more
   concrete instructions or a larger limit and reports if it
   persists.
6. **Context cancellation wins.** When `ctx` is done the adapters
   return the context error unwrapped (not a `ProviderError`), so
   the gateway never mistakes a cancellation for a retryable
   failure.
7. **Gemini key transport.** API key via the `x-goog-api-key`
   header (not the `?key=` query), so keys never land in access
   logs or shared URLs.
8. **Registry with constructor map.** `registry.Register` lets
   other H1 adapters (OpenAI family, sibling agent) plug in
   without editing the registry file. Key resolution order:
   `SecretResolver` (vault hook, H10) first, then the named
   environment variable, then the explicit config value, per
   Sections 19.2 and 20.1. No new module dependencies: stdlib
   `net/http` only.
