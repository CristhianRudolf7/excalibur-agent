# Live coding activity in chat

The interactive daemon publishes tool activity for conversational turns and all
nested chat delegations. The chat displays a live timeline inside each
conversational turn, between the user's message and the final assistant reply,
alongside the existing final Changes card. This implementation is confined to
daemon runners and the web client; evaluation tool definitions, prefixes,
prompts, and runners are unchanged. See decision 0111 for the additive event
contract.

## Behavior

- File reads show the tool and path. Recorded writes show a collapsed diff with
  added/removed counts; expanding an edit displays that individual version,
  including edits that undo an earlier change.
- Commands show their text, optional exit code, and last two output lines.
  Expanding shows the bounded output. Session polls update the original command
  row and retain the command that started the process.
- Test commands recognize Go, pytest, Vitest, Jest, npm/pnpm/yarn test scripts,
  and Cargo. Counts come from observed summaries. Go output without verbose test
  results is labeled as packages. Unknown or incomplete output displays no
  invented counts. A bounded preview may contain only part of a verbose run.
- All runs sharing a root turn render in one inline activity block. Nested
  delegations retain parent markers and indentation; events from concurrent runs
  appear chronologically, with event sequence breaking timestamp ties. Headers
  display state, elapsed time, and expandable completion steps. Criteria are
  marked done together when the run succeeds.
- Completed turns collapse automatically to a single clickable line showing
  action and subagent counts. Expanding restores the fixed-height timeline. The
  summary stays between the originating user message and final reply as later
  turns arrive. Final Changes cards and message actions remain available.
- Intermediate assistant notices render inside the same bounded timeline while
  work is live. After completion, earlier notices remain in the expandable feed
  and the final reply appears below the summary. Notices cannot push the live
  block away from the composer.
- English and Spanish, light/dark tokens, keyboard expansion, reduced motion,
  and a 390 px viewport are supported. Icons use the existing Lucide set.

The timeline has a fixed 320 px scroll viewport and virtual rows. It retains 12
recent runs and 200 entries per run, showing an omission count for earlier
entries. Counts include omitted actions. The feed renders at most 200 recent
assistant notices in addition to those retained tool rows. Completion events
update existing rows. Readers at the end follow new activity and delayed row
measurements; scrolling upward preserves their position until they return to the
bottom. The outer chat follows the active block above the composer while
respecting readers who scroll upward. Chat messages and activity blocks share
one virtualized timeline, so completed summaries retain their turn position.
Live events capture the user-message anchor; persisted message timestamps also
place replayed activity correctly when history loads after the subscription.
Ordinary chat answers without tools or delegations do not create an empty
activity panel.

## Publication and limits

`daemonToolRunner` emits `tool.called` before execution and `tool.completed`
after the normalized executor result, followed by `file.edited` for recorded
versions created by that call's run. Resource keys resolve actual tool paths;
pre-execution version IDs exclude earlier changes. Per-call file events are
ordered by path. Partial successful edits remain visible even if a tool fails.
Unrelated runs cannot contribute versions. Shell edits without file-tool history
are represented by their command result; final Changes behavior is unchanged.

Payloads carry conversation, root turn, run, parent run, and call identity. Raw
argument objects are never published. The existing vault redactor processes
inputs/output and both diff images before clipping or adding diff prefixes.
Limits are 8 KiB per output/diff, 2 KiB per command, 512 bytes per path, 128
bytes per call/session/name, 50 observed files per call, and 20 completion
steps. Clipping preserves UTF-8 and is explicitly indicated. Binary diffs show
an unavailable-preview notice. Events reuse the chat's reconnect/replay client;
activity can be recovered from daemon bus replay, not from a new history API.

## Verification

All fixtures use local fake responses or recorded daemon-shaped events, without
paid calls. Backend tests cover start-before-invocation ordering, failure and
cancellation, literal/pattern/multiline-secret redaction, UTF-8 bounds, command
tails, per-edit diffs, multi-file order/limits, unrelated-run isolation, and a
real fake-endpoint delegated conversation through the daemon.

Frontend tests cover state reconciliation, replay deduplication, session polls,
retention, runner summaries, rendering/expansion, both themes, and Spanish.
`e2e/coding-activity.spec.ts` drives the actual WebSocket client at 1440 and 390
px. It asserts user → activity → final reply DOM order, live changes, completion
collapse and expansion, stable composer position, nesting and conversation
isolation, and the existing final Changes card. Two long-build cases stream more
than 600 events across four runs with three levels of nested subagents and
repeated assistant notices. They check automatic following, pause/resume after
scrolling, retention eviction, bounded rendered rows, fixed feed height, and no
growth of the document viewport.

Screenshots are generated by that fixture under
`/home/cristhian/excalibur-imp/s4-shots/coding-view/`: `desktop-light.png`,
`desktop-dark.png`, `mobile-390-light.png`, and `mobile-390-dark.png`. These
four show the completed turn's inline summary. `long-build.png` shows the live
desktop feed after 323 actions across four runs, including 60 evicted earlier
actions and twelve intermediate assistant notices.

Original implementation verified on 2026-11-04:

- `go vet -p 2 ./...` and `go build -p 2 ./...` passed.
- `go test -race -p 2 ./internal/daemon ./internal/api` passed. A final focused
  race run covering coding activity, assistant turns, delegation modes, and
  intent routing also passed after redaction/order refinements.
- Prefix/hash/frozen checks passed in `internal/engine`, `internal/wireengine`,
  `internal/eval`, and `tests/agentquality`. Frozen paths have no source
  changes.
- Web typecheck, lint, and formatting passed; lint reports existing warnings in
  unrelated files. All 144 Vitest files / 833 tests passed. The 17 new focused
  component/store/parser cases also passed after the final scroll refinement.
- The fixture Playwright suite passed both cases, at 1440 and 390 px. Its
  production web build passed, and all four screenshots were visually reviewed.

Go commands use `nice -n 19` and `-p 2`; test processes run under
`systemd-run --user --scope -q -p MemoryMax=6G`. The e2e preview uses port 5314.
No paid model calls were made.

## A2 placement and live-follow verification

The follow-up is web-only. No daemon/API contract changes or frozen evaluation
path edits were needed. Commits 4278b52f and 6d871379 implement the inline turn,
completion summary, notice handling and measured-height following. Commit
74767768 adds the browser order/follow regressions and regenerates screenshot
evidence.

Verified on 2026-11-04 with the shared-machine resource limits:

- Web typecheck, lint and formatting passed. Lint still reports warnings in
  unrelated files. The production build passed.
- The final full Vitest run passed all 145 files / 840 tests. All 32 focused
  Vitest cases also passed across the component, store, chat and timeline
  suites. They include turn placement across later messages, replay placement,
  collapsed summaries, notices, measured-height following, pause/resume, and
  interleaved events with equal timestamps.
- All four Playwright cases passed at 1440 and 390 px. The five screenshots were
  refreshed by the fixture and visually reviewed.
- `go vet -p 2 ./...`, native `go build -p 2 ./...`, macOS amd64 and Windows
  amd64 builds passed, as did formatting and the no-bare-goroutine check.
- The full `go test -race -p 2 ./internal/daemon ./internal/api` suite passed.
  Separate race checks also passed for coding activity and embedded/evaluation
  parity, assistant-turn timing, delegated runs, and intent routing.
  Prefix/hash/frozen race checks passed in `internal/engine`,
  `internal/wireengine`, `internal/eval`, and `tests/agentquality`.
- Staticcheck reports existing ST1005 errors in `internal/googleapi/tools.go`
  and U1000 errors in `internal/robotlab/requirements_nondarwin.go`. Neither
  file changed in this follow-up.
