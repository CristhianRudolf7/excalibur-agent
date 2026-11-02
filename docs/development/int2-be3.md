# INT-2 BE-3: Voice and assistant web sources

The backend exposes `voice.status`, `voice.transcribe` and `voice.speak`, plus
binary `POST /blobs` uploads. `internal/voice` implements the OpenAI audio
transport. `internal/daemon/voice.go` shares the gateway blob store, and
`cmd/excalibur/daemon.go` composes it with layered configuration and the existing
provider-registry/vault credential resolver.

Example operator configuration:

```toml
[voice]
provider = "openai"
stt_model = "gpt-4o-mini-transcribe"
tts_model = "gpt-4o-mini-tts"
voice = "alloy"
# enabled = false # omission follows OpenAI credential availability
```

Upload audio bytes to `POST /blobs` with their audio Content-Type. Pass the
returned `audio_blob` and MIME to `voice.transcribe`. `voice.speak` returns an
`audio_blob` and MIME; download with `GET /blobs/{audio_blob}?mime={mime}`.
Existing digest-addressed blob uploads continue to work. Audio is limited to
25 MiB; speech text is limited to 4096 Unicode characters per request. Clients
split longer text into sentences. Unknown transcription timing is represented
by `duration_ms: 0`; unknown detected language uses the request language or an
empty string. Speech timing is omitted when unavailable.

Web tools record trusted run attribution. Assistant delivery adds only the
returned/read pages for its run tree. Web sources include kind, URL, title,
domain, read time and tool; they do not include snippets or page content.
Unread Brave results are excluded. Library citation validation and fields remain
unchanged. Published messages and RPC history expose the same persisted sources.
The model-facing voice capability remains its existing deterministic adapter.
See decisions 0090 and 0091 for the contract and implementation boundaries.

Backend evidence:

- `internal/voice/client_test.go`: simulated OpenAI request shape, defaults and
  configured models/voices, audio/text limits, HTTP errors, redirects, response
  parsing, cancellation, deadlines, credential precedence and secret-safe errors.
- `internal/config/voice_test.go`: TOML fields and explicit disabled round trip.
- `internal/daemon/voice_test.go`: RPC availability with/without credentials,
  binary upload, speech download, transcription and invalid references/limits.
- `internal/daemon/web_sources_test.go`: engine invocation with a fake web
  backend, trusted attribution, isolation, library citations alongside web
  sources, events, RPC history and Brave retention.
- `internal/tools/web_sources_test.go`: search/source-search run attribution,
  private provider metadata and retention exclusion.
- Existing API contract parity and daemon/embedded surface and eval pins remain
  mandatory. Ordinary tests use simulated servers and do not call paid APIs.

Verification commands (from the repository root unless noted):

```sh
export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"
mkdir -p /dev/shm/excalibur-be3-tests
export TMPDIR=/dev/shm/excalibur-be3-tests GOTMPDIR=/dev/shm/excalibur-be3-tests
export GOMAXPROCS=2 OMP_THREAD_LIMIT=1
systemd-run --user --scope -p MemoryMax=3G -p MemorySwapMax=0 \
  nice -n 19 go test -race -p 2 -count=1 -timeout 30m \
  ./internal/voice ./internal/api ./internal/config ./internal/gateway \
  ./internal/daemon ./internal/engine ./internal/tools ./internal/websearch \
  ./cmd/excalibur
go vet ./internal/voice ./internal/api ./internal/config ./internal/gateway \
  ./internal/daemon ./internal/engine ./internal/tools ./internal/websearch \
  ./cmd/excalibur
staticcheck ./internal/voice ./internal/api ./internal/config ./internal/gateway \
  ./internal/daemon ./internal/engine ./internal/tools ./internal/websearch \
  ./cmd/excalibur
gofmt -l .
bash scripts/check-no-bare-go.sh
GOOS=linux go build ./...
GOOS=darwin go build ./...
GOOS=windows go build ./...
```

Tests use `TMPDIR=/dev/shm/excalibur-be3-tests` and
`GOTMPDIR=/dev/shm/excalibur-be3-tests` to avoid host disk-sync stalls in SQLite
fixtures. Go 1.27 uses `GOTMPDIR` for `t.TempDir` as well as build scratch files. The same memory, swap and priority
limits apply. The full repository regression is reserved for integration review.
Web validation uses `pnpm check --testTimeout=30000 --maxWorkers=1`, `pnpm build`
and `e2e/document-viewer.spec.ts` (three tests), with a temporary Playwright
configuration pointing at the built app on an isolated port. The initial web
unit run exceeded the default five-second timeouts under host contention; the
single-worker rerun passed all 206 tests without changing repository settings.

The existing assistant restart test retains all restart and attachment assertions
but allows ten seconds for the first model request. Its previous one-second
startup wait expired during race-instrumented attachment reads on the capped
runner. Daemon reruns use `GOMAXPROCS=2`, `OMP_THREAD_LIMIT=1` and a 30 minute
package timeout; these settings do not disable race detection or skip tests.

Final verification: all nine scoped Go packages passed with race detection
(daemon and command packages were rerun separately with the settings above).
The complete daemon run passed in 229.554 seconds; the complete command run
passed in 36.474 seconds. API contract parity and both daemon/embedded surface
and eval-profile pin tests passed. Vet, staticcheck, repository formatting,
no-bare-go checks, and Linux/Darwin/Windows builds passed. Web validation passed
all 206 unit tests, the production build, and all three document-viewer browser
tests. No live API calls or external benchmark runs were performed.

## Integration merge (BE-1, BE-2 and BE-4)

The merge retains the complete client surface from both branches: 75 methods
and 25 events, without duplicates. JSON registry arrays follow `api.Methods`
and `api.EventTypes` order.

Conflict resolutions:

- `apps/web/src/lib/rpc/contract.ts`: retain the whiteboard method extension,
  question/plan methods, task/reminder and catalog/usage types, and voice methods.
  Keep optional citation numbering and the shared web-source fields once.
- `cmd/excalibur/daemon.go`: combine voice credentials and the shared audio blob
  store/source recorder with task queries, persistent reminders, whiteboard and
  the SQL usage store; preserve all service construction and event callbacks.
- `internal/api/methods.go`: retain every constant and registry row, including
  voice, tasks, reminders, whiteboard, catalog management and usage.
- `internal/daemon/routes.go`: retain both imports and every service field;
  compose voice dispatch with persistent task/reminder and whiteboard dispatch,
  plus the expanded catalog/usage handlers. Keep one library service field.
- `internal/daemon/services.go`: retain the source recorder and whiteboard
  service, plus their assembly and shared engine bindings.
- `internal/gateway/gateway.go`: combine the voice and task/reminder/catalog/usage
  passthrough cases; preserve whiteboard routing and both blob upload routes.
- `packages/contracts/methods.json`: retain every method and follow the merged
  Go registry order.

Automatically merged files were reviewed as well:

- `docs/development/agent-quality.md`: preserve the persistent reminder,
  catalog/usage control and whiteboard sections.
- `internal/api/events.go` and `packages/contracts/events.json`: keep task,
  reminder and whiteboard events, alongside message sources. Align the JSON
  event array with the Go registry while preserving all schema definitions.
- `internal/cli/exec_engine.go`: retain persistent reminder opening/cleanup and
  embedded profile selection, including eval isolation.
- `internal/wireengine/catalog.go`: retain reminder wiring, whiteboard options,
  profile admission and the eval skill-index exclusion.
- `internal/daemon/engine_wire.go`: combine the source recorder with whiteboard
  composition at the shared embedded builder.

Decision 0093's whiteboard catalog names pin is retained. This merge adds no
further catalog-name change; prefix and eval-profile pins remain unchanged.

Merge verification uses the same 3 GiB/no-swap/nice-19 Go test scopes, with
`GOMAXPROCS=2`, `OMP_THREAD_LIMIT=1` and `-race -p 2 -count=1`. Database-heavy
packages use tmpfs for `TMPDIR` and `GOTMPDIR`; the engine package is run with
both set to `/tmp` because its existing named-artifact path fixture recognizes
that root. No path-detection logic or test assertions were changed for the merge.
The merge's web check uses `pnpm check --testTimeout=30000 --maxWorkers=1`.

Merge validation passed: race tests for API, tasks, agenda, whiteboard, skills,
wireengine, daemon, CLI, voice, tools and engine, plus touched gateway, telemetry,
migrations and command packages. The complete daemon suite, including both
catalog/prefix/eval parity tests, passed in 139.923 seconds. The engine rerun
with `/tmp` fixtures passed in 6.324 seconds. All three OS builds, touched-package
vet/staticcheck, repository gofmt and no-bare-go checks passed. Web `pnpm check`
passed all 256 tests across 42 files; the production build also passed.
