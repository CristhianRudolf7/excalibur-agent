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

## Native charts (2026-11-04)

A chart is an input-only element accepted by `add`, `set_scene` and `update`.
The tool can open a board and draw a labelled chart in one call:

```json
{"action":"add","elements":[{"id":"expenses","type":"chart","x":40,"y":80,"width":560,"height":340,"chart":{"kind":"bar","title":"Weekly expenses","unit":"S/","labels":["Food","Travel"],"values":[69.5,20]}}]}
```

Kinds are `bar` (horizontal), `column` (vertical), `line` (connected points)
and `pie` (a native bar legend with values and percentages). Charts contain
1–24 nonempty category labels and equally many finite values. Bar, column
and pie values must be non-negative; line values may be negative. Optional
`colors` supplies a repeating palette (up to 24 colors); otherwise the
default palette is used. Values use locale-neutral formatting: `S/ 69.50`, `S/ 20`. Zero-valued
categories retain their labels and values, including zero-total pie legends.

The server expands charts into editable native rectangles, text, lines and
ellipses. Every member has `group_id` equal to the chart ID. A background
rectangle keeps that ID and the chart bounds; member IDs include
`<id>-title`, `<id>-axis`, `<id>-label-0`, `<id>-value-0`, `<id>-bar-0` or
`<id>-point-0`, and `<id>-line` for connected points. Text has explicit
foreground/background contrast and 16px labels (22px titles). Dense layouts
grow to fit readable category/value labels instead of shrinking the text.
Excalidraw applies its light/dark canvas rendering to these native colors.

`{"action":"update","elements":[{"id":"expenses","chart":{"kind":"column","title":"Revised expenses","unit":"S/","labels":["Food"],"values":[72.5]}}]}`
replaces the entire chart group, preserving the existing position, bounds
and authorship. Geometry patches on the chart ID move/resize all members;
style patches merge into each member's style. Removing the chart ID removes
the entire group. Individual native members remain editable. Native group
identity survives storage restarts without additional schema fields.

Expansion and validation happen inside the atomic board mutation. Both
persisted operation history and live events contain normalized native
operations, with explicit member removals when replacing or removing a
chart. Chart expansion counts toward the existing 500-element call limit;
the existing 2 MiB scene/input limits remain in force. Invalid charts do
not create an implicit board.

For primitive elements, top-level `fill`, `stroke`, `fontSize`/`font_size`,
`strokeWidth`/`stroke_width`, `opacity`, `textAlign`, `fontFamily`,
`backgroundColor` and `strokeColor` normalize into snake_case `style`
properties before strict decoding. Explicit nested style takes precedence;
canonical top-level names win over aliases. Partial style updates preserve
unspecified style properties. Unknown fields still produce input errors
listing allowed element, style and chart fields. Opacity uses the daemon's
0–1 contract and is converted to/from Excalidraw's 0–100 scale by the adapter.
No translation keys are added: chart titles, units and labels are caller
content, and the existing whiteboard controls retain English/Spanish i18n.

Evidence: `internal/whiteboard/chart_test.go` covers all layouts, validation,
finite extremes, zero values, palettes, dense layouts, atomic failures,
restarts, group replacement/movement/resizing/style/removal, event replay
and one-call tool execution. `input_test.go` covers every style shortcut,
precedence, partial updates and helpful strict-decoding errors. The web
adapter tests cover chart groups and opacity; the whiteboard browser suite
checks native chart exports and light/dark rendering. The existing daemon
hash/parity and whiteboard admission tests verify the frozen eval/embedded
surfaces remain unchanged.

### Chart verification record (2026-11-04)

The final whiteboard package passed all Go race tests. `gofmt -l .` was
empty; touched-package vet/staticcheck, the bare-goroutine check, and
repository builds for Linux, Windows and Darwin passed. All Go test runs
used `systemd-run --user --scope -q -p MemoryMax=6G`, `nice -n 19`,
`-p 2` and `-count=1`. Existing daemon surface hashes/parity and whiteboard
admission tests in daemon, wireengine, CLI, eval and tools passed.

Web verification passed the requested TypeScript command, 29 whiteboard
Vitest cases, production builds, existing desktop/mobile/offline browser
cases and new light/dark chart browser cases. Native SVG exports contained
the category, value, title and fill; screenshots confirmed readable colors
in both themes. Web-wide typecheck, lint and formatting passed.

Broader regression checks found failures outside this change. The
repository race run was stopped after failures in command tests for classroom-daemon startup,
exec signals, PDF-worker startup and SIGTERM startup. An isolated retry
with `GOMAXPROCS=2` and `GORACE=atexit_sleep_ms=0` passed the exec-signal and
PDF-worker cases, but classroom-daemon initialization still exceeded its
deadline during SQLite setup and SIGTERM startup still timed out. The
web-wide Vitest run completed with 698 passing tests and one failing
agent-settings favorite/archive case; all six tests in that file passed
on an isolated retry. The repository-wide race run did not complete, and
neither broad run is reported as green. No protected packages or unrelated
tests were changed.
