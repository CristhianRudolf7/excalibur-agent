# Account connection relay

The daemon supports `provider: "codex"` in the operator credentials file,
`[models].provider = "codex"` in layered configuration, or
`EXCALIBUR_PROVIDER=codex`. Set `model` to a model available to the signed-in
account and `reasoning_effort` to the desired effort. Requests count against
the account's plan allowance. `EXCALIBUR_SERVICE_TIER=fast` selects the upstream `priority` tier, matching
the CLI. Other tier values pass through unchanged.

This implementation supplies the credential store and daemon relay. Login RPCs
and the settings interface are a separate integration. Other command entry
points retain their existing provider resolution.

## Credential storage

The store resolves `EXCALIBUR_CODEX_AUTH_FILE`, then
`<data-dir>/codexlink/auth.json`, then `~/.codex/auth.json`. Missing files fall
through; an unreadable or malformed selected file fails without exposing its
contents. The public status reports connection source, optional email and plan,
and access expiry. Token claims supply display metadata only.

`Save` writes the application's own sign-in file atomically with mode `0600`.
`Delete` removes only that file. An external sign-in can therefore remain active
after deleting the application's file. Refresh updates the selected file in
place through an atomic replacement, preserving unknown JSON fields. Concurrent
requests share one store and serialize refresh; requests rejected with an old
token reuse the already-refreshed token. Expired access tokens refresh before
use, and a 401 permits one refresh and one replay. Refresh failures leave the
previous file intact. Tokens and the private relay key are excluded from public
status and error messages.

## Request and response handling

The daemon creates a private loopback listener and random bearer key on each
start. Both loopback source address and bearer key are required. The listener
closes with the daemon. Account tokens are attached only to upstream requests;
redirects are rejected.

The relay moves system and developer messages into `instructions`, keeps tool
calls and results in `input`, sets `store=false` and `stream=true`, and removes
unsupported request parameters through an allowlist. Reasoning, service tier,
function schemas, tool choice, text options and prompt-cache keys survive.

Streaming callers receive SSE frames as they arrive. JSON callers receive the
terminal response with its output items and usage, including reasoning and
function-call items. A missing terminal event fails explicitly. Upstream errors use safe messages, with redacted input-rejection details. Usage-limit responses map to HTTP 429
with the supplied reset time or retry interval, and the relay never retries
429. A limit event arriving after SSE headers have been sent necessarily stays
an SSE event; the HTTP status can no longer change.

The existing model gateway still routes daemon agent calls. The local provider
uses the per-start key directly, preventing unrelated environment or vault keys
from overriding relay authentication. Account-mode decisions use the configured
chat model. Remote embeddings are disabled for account mode; local memory
retrieval remains available. Audio operations need their existing separate
credentials and are outside this relay.

## Verification

Ordinary tests use simulated servers only. They cover credential precedence and
permissions, metadata, refresh and rewrite, concurrent refresh, local caller
authentication, request headers and instruction mapping, SSE aggregation and
passthrough, reasoning/function-call/usage preservation, 401 replay, usage limits,
redaction, listener shutdown, provider selection and a daemon gateway tool call.

Run the requested backend verification from the repository root:

```sh
GOMAXPROCS=2 systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 \
  /usr/local/go/bin/go test -race -p 2 \
  ./internal/codexlink ./internal/config ./internal/daemon \
  ./internal/gateway ./cmd/excalibur
GOMAXPROCS=2 systemd-run --user --scope -q -p MemoryMax=6G nice -n 19 \
  /usr/local/go/bin/go vet -p 2 \
  ./internal/codexlink ./internal/config ./internal/daemon \
  ./internal/gateway ./cmd/excalibur
```

On shared machines, `GOMAXPROCS=2` also keeps daemon startup tests within their
existing deadlines. A scratch `HOME` with the normal Go cache paths retained
keeps unrelated machine sign-ins out of the ordinary tests. Preserve the normal
`XDG_CACHE_HOME` as well: a cold renderer compilation cache can exceed existing
PDF/OCR test deadlines. The failed cold-cache cases passed when the renderer
cache was preserved, without source changes or longer per-document deadlines.

An opt-in `live` test requires `EXCALIBUR_LIVE=1` and
`EXCALIBUR_CODEX_SMOKE_MODEL` set explicitly. It makes exactly three requests:
a short JSON response, the same short streaming response, and a function call.
It uses low reasoning and the fast service tier and logs only outcome and usage.

Protocol research used the installed CLI release's source: the Responses
endpoint/session headers and JSON refresh grant, client identifier and token
endpoint. Literal wire identifiers follow that protocol. This work does not
change the evaluation or benchmark path.

The 2026-11-04 live smoke passed through a throwaway loopback relay using the
machine sign-in: JSON and streaming requests both returned `ok`, and a function
call preserved its name, call ID and JSON arguments. The three successful
requests used low reasoning and fast routing (upstream `priority`). Input/output
usage was 10/5, 10/5 and 56/19 tokens respectively. Two initial input rejections
produced no completed responses and established that literal `fast` must use the
CLI's `priority` wire value. No credentials were printed or recorded.

Final verification on 2026-11-04 passed the complete five-package race suite,
vet and static analysis, plus Windows and macOS builds of the store, config,
daemon and command packages. The complete race run used a scratch home while
retaining the normal Go and renderer caches; all new connection tests and the
existing document-processing tests passed together.
