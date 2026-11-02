# INT-2 whiteboard backend

The backend implements the interactive conversation whiteboard contract in
`internal/whiteboard`, with shared drawing and event types in
`internal/api/whiteboard.go`. Migration 8 persists boards, versions and op
history through the existing SQLite porter. Production daemon startup shares
one service between the API, model tool and telemetry bus. The gateway uses
its existing registered-method passthrough; no transport interface changes
are needed.

The deferred `whiteboard` tool supports open, close, add, update, remove,
clear, set_scene, focus, highlight, annotate, step, read, export, list, rename
and delete. Inputs are capped, authorship comes from the execution boundary,
and step delays and snapshot waits honor cancellation. Presentation and scene
mutations advance versions; read/list observations do not. Client apply
retries are durable and idempotent; conflicts report the current version.
The UI rebases its intended element patches and resubmits with a fresh ID.

Every event carries the trusted `conversation_id`. An implicit board follows
the conversation's active board; a supplied title creates another one.
Snapshots use `whiteboard.snapshot_requested` with `request_id`, the existing
blob upload, then `whiteboard.snapshot` with `board_id`, `image_blob` and
optional `request_id`. Concurrent requests require that correlation ID.
Eight-second timeouts return the current element payload without an image
and an explicit timeout notice. PNG exports request a UI snapshot; JSON
exports preserve the scene. SVG exports represent native primitives and
include the source text of math, Mermaid and code widgets; use PNG when the
exact UI rendering is needed.

The builtin `whiteboard-teaching` skill is excluded from eval. The tool is
available in default/delegate and conversational turns, and absent from the
eval registry, discovery and dispatch. Decision 0093 records the sole catalog
name pin change; the fixed prefix and eval profile pins remain unchanged.
The feature switch is documented in `agent-quality.md`.

Backend evidence:

- `internal/whiteboard/whiteboard_test.go`: migration from version 7, disk
  restart, versions/op log, retries, conflict atomicity, rebasing, authorship,
  invalid/oversized edits, all primitive types and tool actions, animation,
  scripted steps, interruption, snapshots, timeout and cancellation, API
  validation, and conversation-scoped events.
- `internal/daemon/whiteboard_test.go`: HTTP JSON-RPC routing, retries,
  conflicts and invalid inputs, plus a scripted assistant model turn that
  opens, draws, highlights and reads a board through the real executor.
- `internal/wireengine/whiteboard_test.go`: deferred discovery/dispatch,
  enabled/disabled behavior, skill indexing and eval exclusion.
- `internal/cli/whiteboard_test.go`: default/delegate admission and eval
  exclusion, including the disabled path.
- Existing API export parity and daemon/embedded surface tests check the
  additive method/event exports and frozen surfaces.

## Verification record (2026-11-02)

Race tests covered `internal/whiteboard`, `internal/api`,
`internal/store/migrations`, `internal/skills`, `internal/wireengine`,
`internal/daemon`, `internal/cli` and `cmd/excalibur`. Every test invocation
used a user systemd scope with `MemoryMax=3G`, `MemorySwapMax=0`, `nice -n 19`,
`go test -p 2 -count=1 -race`, and a bounded package timeout. The final
whiteboard run passed, as did API parity, migration, skill and wireengine
suites and the daemon's whiteboard and surface parity cases.

On the busy shared host, broad daemon/CLI/command runs hit existing
wall-clock deadlines in these tests:

- `TestEvalTimeOnlySurvivesPast25Steps`
- `TestExecSignalRecords`
- `TestSIGTERMContext`
- `TestEngineAssistantRestartsAndKeepsAttachments`

All four passed when rerun in isolation, one package at a time, under the
same memory cap and priority with `GOMAXPROCS=1` and
`GORACE=atexit_sleep_ms=0`. No test deadlines or assertions were changed.
The remaining cases passed in the broad runs.

The following checks also passed on the final source:

- `gofmt -l .` (empty output) and `git diff --check`.
- `go vet -p 2` and `staticcheck` on the eight packages above.
- `bash scripts/check-no-bare-go.sh`.
- `go build -p 2 ./...` for Linux, Darwin and Windows.

The full repository test regression, live APIs and benchmarks were not run.
