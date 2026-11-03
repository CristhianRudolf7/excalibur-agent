# User time zone

Settings → General → Time zone offers a searchable IANA picker, a local clock
with the current UTC offset, and automatic browser detection. Every visible
label has English and Spanish translations. The IANA list comes from
`Intl.supportedValuesOf('timeZone')`; older browsers use a fallback list that
also includes UTC, the browser zone, and the stored zone. The picker supports
search, arrow keys, Home/End, Enter, and Escape. A failed save keeps the persisted
selection and offers an error message; the user can retry the same action.

The existing `principals` row is the only source of profile time zones. Memory's
principal service reads and updates it; the daemon keeps its existing profile
seeding, feature switch, and RPC error boundary. Migration 12 adds
`timezone_source`, with `default` for existing rows, preserving their time zones.
New profiles use UTC and source `default`, including on non-UTC hosts. Headless
clients never detect a browser zone.

`principal.get` includes `timezone_source`. `principal.update` accepts
`timezone` and `timezone_source` (`default`, `auto`, `manual`) and returns the
effective profile. The daemon validates zones with `time.LoadLocation`, rejecting
empty names and the host-dependent `Local` pseudo-zone. Invalid input returns
JSON-RPC invalid params. Other profile fields are preserved.

The browser reports its zone once per application startup, including React
StrictMode and route remounts. Auto updates are atomically ignored when the
stored source is manual, including concurrent reports. Selecting a zone or
switching detection off saves source `manual`. Explicitly switching detection on
sends source `auto` with `enable_auto: true` and the current browser zone, making
the transition atomic. Startup reports never include that flag. The shared
profile query cache refreshes after detection and after successful Settings
updates. See decision 0104 for the additive contract.

The daemon installs a principal-zone resolver on the master. Each receipt reads
it once and captures that zone for message persistence and temporal preanalysis,
including deferred and mixed-control intake. New durable messages write
`user_tz` and preserve the snapshot in their normalized data. The next assistant
turn reads the current profile through the existing profile line. A missing or
disabled profile retains the master's previous configured/UTC fallback.

Automation schedules retain their own stored zones and UTC instants. Profile
updates never edit them or create new versions. Tool results add `at_local`,
`next_run_at_local`, and `now_local` alongside existing UTC fields, using the
schedule's zone, date, weekday, and offset at each instant. For full get/list/
restore results, `at_local` lives beside `at` inside `schedule`; compact write
confirmations put both at the top level. Recurring schedules expose
`next_run_at_local` even when there is no `at`. The presentation guidance is
added only by `WireDaemonTool` at the daemon command's composition boundary.
The embedded builder continues using `WireTool`, preserving its catalog
definition and pinned prefix hash even when supplied a service. Rewiring the
daemon adds the guidance only once.

## Regression coverage

| Behavior | Evidence |
| --- | --- |
| UTC/default, validation, persisted sources, manual protection, explicit auto re-enable | `internal/memory/principal_test.go`, daemon `TestPrincipalUpdateRPC` |
| Existing profiles preserved on migration; host zone does not change defaults | `internal/store/migrations/v12_test.go`, daemon principal seeding test |
| Immediate zone changes for sync/deferred temporal intake and message snapshots | `internal/master/timezone_test.go` |
| Durable `user_tz` and next assistant profile see updates | daemon `TestProfileZoneFlowsToMessagesAndNextAssistantTurn` |
| Profile changes leave automation zone, instant, next run, and version intact | daemon `TestProfileTimezoneNeverMovesExistingAutomation` |
| Local date boundary, DST, fractional offsets, all schedule-returning tool actions | `internal/automations/timezone_test.go` |
| Picker, failures, switch, shared cache, startup deduplication and manual protection | Settings `TimeZone.test.tsx` |
| IANA fallback and local clock/date/offset formatting | Settings `time-zone.test.ts` |
| English/Spanish keyboard selection, reload persistence, auto re-enable, mobile fit | `apps/web/e2e/time-zone.spec.ts` |
| Embedded surface/prefix and eval profile unchanged | daemon `TestEmbeddedSurfaceAndEvalProfileDoNotMove`, automation `TestDaemonGuidancePreservesEmbeddedAutomationDefinition` |

## Verification

Tests use isolated SQLite stores, simulated RPC responses, and the existing
system Chrome setup; no paid APIs are called.

- Touched Go files: `gofmt -l` clean; `git diff --check` clean.
- `scripts/check-no-bare-go.sh`: passed.
- `go vet -p 2` on api, automations, daemon, gateway, master, memory,
  store/migrations and cmd/excalibur: passed.
- `staticcheck` on the same touched packages, with compilation and analysis
  parallelism limited to two: passed.
- `go build -p 2 ./...` for Linux, Darwin and Windows: passed.
- Web `npx tsc --noEmit -p apps/web` (with npm prefix `apps/web`) and
  `pnpm typecheck`: passed.
- Web `pnpm lint` and `pnpm format:check`: passed. Lint retains pre-existing
  warnings in unrelated files; no new time-zone warnings.
- Web `npx vitest run`: 118 files, 627 tests passed.
- `EXCALIBUR_WEB_TEST_PORT=5196 npx playwright test e2e/time-zone.spec.ts
  --workers=1`: both English and Spanish flows passed. The test setup also runs
  `pnpm build`, which passed.

`go test -race -p 2 -count=1` passed for master, memory, api, store/migrations,
gateway, automations, daemon and cmd/excalibur. The final daemon/command run
used `-timeout 20m` to accommodate shared-machine load.
The existing gateway `TestWebSocketRPCAndSubscribe` hit its 10-second deadline
on the first race run; a complete gateway package rerun passed without changes.
The initial compatibility check also caught shared automation wiring changing
the embedded prefix; the explicit daemon-only wiring above fixes that, and its
focused regression and the existing embedded/eval pins pass.

An exploratory run of the untouched parent `internal/store` package encountered
a `TestExternalLockBusy` statistics assertion followed by a cleanup hang. The
stack showed cleanup waiting for the still-held test lock. That run was stopped;
final storage checks cover the touched `internal/store/migrations` package. No
storage implementation or unrelated tests were changed.

All Go builds and vet runs use `nice -n 19` and `-p 2`. Tests additionally run inside a user systemd scope with
`MemoryMax=6G` and use `-count=1`.
