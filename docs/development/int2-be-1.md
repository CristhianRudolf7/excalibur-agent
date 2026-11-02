# INT-2 BE-1: Task queries and persistent reminders

The backend exposes tasks.query/get and reminders.list/create/update/delete
through POST /rpc, with task.updated, reminder.updated and reminder.fired on
the existing event bus. Decisions 0086 and 0087 describe the exact state mapping,
pagination semantics and durable reminder wiring. Shared wire schemas are in
packages/contracts/task-reminder.json and the typed web RPC contract.

Task detail reads real planning and execution rows. Task-scoped notebook
fragments are read from identity_docs/doc_fragments. Optional task due_at has
no durable backing in the current schema and is omitted. Run times
use creation and final update timestamps. Reminder tool dates must be RFC3339;
the memory eval implementation retains its permissive date strings.

Verification includes focused race tests for internal/tasks, internal/agenda,
internal/api, internal/gateway, internal/wireengine, internal/daemon and
internal/cli, plus cmd/excalibur build/tests, frozen daemon/embedded parity,
go vet, formatting, no-bare-go checks and Linux/Darwin/Windows builds.

New behavioral tests:

- TestClientTaskMappingPaginationFiltersCounts
- TestTaskDetailAndPostCommitChanges
- TestPersistentReminderToolRestartAndFire
- TestReminderCRUDValidationSnoozeAndEvents
- TestTaskReminderRoutes
- TestEmbeddedReminderProfiles
- TestReminderToolRestartDeliversMasterNotice

## Verification results

Completed on 2026-11-02:

- Race tests covered internal/tasks, internal/agenda, internal/api,
  internal/gateway, internal/wireengine, internal/daemon, internal/cli and
  cmd/excalibur. Every test used a systemd scope with MemoryMax=3G,
  MemorySwapMax=0, nice 19 and go test -p 2 -count=1. Later runs used a
  bounded 30-minute package timeout and GOMAXPROCS=1 or 2 on the shared host.
- Both frozen daemon/embedded parity tests passed, as did the API contract
  parity tests. No tool definitions, eval profile or parity pins changed.
- All new task/reminder tests passed, including tool execution, keyed creation
  retried after restart, actual master notification and event delivery.
- go vet and staticcheck passed for every touched Go package; gofmt -l .,
  git diff --check and scripts/check-no-bare-go.sh passed.
- go build -p 2 ./... passed for Linux, Darwin and Windows.
- Web typecheck, lint, format checks and production build passed. All 206 unit
  tests passed with VITEST_MAX_WORKERS=1, --testTimeout=30000,
  --hookTimeout=30000 and --retry=1. No UI behavior changed, so no browser
  interaction specs were added.

Initial broad runs hit shared-host timing limits: agenda exceeded the default
10-minute package timeout while actively running a SQLite migration, and the
existing daemon attachment/library and CLI/binary signal tests exceeded their
short internal deadlines. Agenda passed in full with the larger package
budget. Every other failed case passed when rerun separately with GOMAXPROCS=1,
the same memory cap, race detection and low priority. The default web check
also encountered five-second UI timeouts; the full unit suite passed with the
runtime flags above. These test limits and unrelated implementations were not
modified. No live APIs, paid model calls or benchmarks were launched.

## Integration merge resolution

The integration merge preserves BE-1 task queries and persistent reminders,
BE-2 skills catalog and usage, BE-4 whiteboards, and the integrated web client.
No eval or prefix pin changed. Decision 0086 records the merged catalog pin.

| Conflicted file | Resolution |
| --- | --- |
| apps/web/src/lib/rpc/contract.ts | Keep integrated WhiteboardMethods, voice, projects, skills, usage and source fields; retain task/reminder types and clientMethods entries without duplicate declarations. |
| cmd/excalibur/daemon.go | Attach task queries and reminders together with whiteboard and usage telemetry; preserve both initialization paths and skill roots. |
| docs/development/agent-quality.md | Keep the reminder, catalog/usage and whiteboard switch sections. |
| internal/api/events.go | Keep task/reminder events followed by whiteboard and existing events. |
| internal/api/methods.go | Keep every constant and registry entry from both sides. |
| internal/cli/exec_engine.go | Pass both Reminders and Profile to the shared builder. |
| internal/daemon/routes.go | Keep task/reminder and whiteboard services/routes, plus catalog and usage routing. |
| internal/gateway/gateway.go | Route new task/reminder and catalog/usage calls through passthrough; preserve principal validation and whiteboard routes. |
| internal/wireengine/catalog.go | Keep Reminders, Whiteboard, ConversationID and Profile options, and both provider registration paths. |
| packages/contracts/events.json | Union event entries and payload definitions in the Go registry order. |
| packages/contracts/methods.json | Union all method entries in the Go registry order. |

Merge validation passed against integration commit
`182781bbd6626c8daf2cbace9b93c759ea4567fd`:

- A single capped race-test run passed all twelve packages: api, tasks, agenda,
  whiteboard, skills, wireengine, daemon, cli, gateway, telemetry,
  store/migrations and cmd/excalibur. The scope used MemoryMax=3G,
  MemorySwapMax=0, nice 19, GOMAXPROCS=2, -p 2, -count=1 and -timeout 30m.
- Both daemon/embedded parity tests passed with the incoming whiteboard name
  pin; prefix and eval pins remained unchanged. The API suite passed, and a
  separate comparison confirmed exact registry/JSON ordering and preservation
  of both parents' entries.
- Scoped go vet and staticcheck passed, as did gofmt -l .,
  scripts/check-no-bare-go.sh and git diff --check.
- Linux, Darwin and Windows go build -p 2 ./... passed.
- VITEST_MAX_WORKERS=1 pnpm --dir apps/web check passed all 256 tests in
  42 files, plus typecheck, lint and format. The production web build passed.

No isolated reruns or test-limit edits were needed for this merge validation.
