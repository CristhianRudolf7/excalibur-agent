# 0033 Wave 4 integration: daemon, engine and SQL wiring

Merge of `feat/w4-wire-daemon`, `feat/w4-wire-engine` and
`feat/w4-wire-sql` (in that order) into `main`. All three branches
claimed DONE in their status files; the merge keeps both sides'
functionality wherever they overlapped.

## Conflict resolution

- `internal/agenda/sqlstore.go` (add/add): the daemon branch stored
  schedules/reminders/triggers in standalone `agenda_*` tables behind
  `EnsureSQLSchema` + `NewSQL*` constructors; the SQL branch stores
  the same interfaces in the migration-aligned v4/v5 tables
  (`schedules`, `reminders`, `triggers`, `trigger_events`,
  `agenda_fires`, `workflow_versions`, `workflows`, `workflow_runs`,
  `sessions`) behind `Open*` constructors. The v4/v5 schema won: it
  is the Appendix A source of truth and the migration registry is
  authoritative. Nothing observable was dropped: `EnsureSQLSchema`
  now guarantees the v4/v5 agenda tables (no-op on migrated
  databases), `NewSQLAgendaStore`/`NewSQLReminderStore`/
  `NewSQLTriggerStore` are aliases of the `Open*` constructors, and
  `SQLTriggerStore` keeps the daemon spellings `Save`/`Load`/`Delete`
  over `SaveRule`/`Rules`/`DeleteRule`. No production database with
  the `agenda_*` layout ever shipped, so no migration was needed.
- `internal/agenda/sqlstore_test.go` (add/add): kept the full SQL
  suite (round trips + crash/reopen for every store) and the daemon
  suite re-homed as compat tests (`TestSQLAgendaStoreCompatAliases`,
  `TestSQLReminderStoreRoundTrip`, `TestSQLTriggerStoreRoundTrip`)
  exercising the alias constructors and `EnsureSQLSchema`.
- Decision records: all three branches filed `0030-*.md`. Kept
  `0030-daemon-wiring.md`, renumbered the others to
  `0031-wire-engine-seams.md` and `0032-sql-wiring.md` (titles
  updated), following the wave 3 precedent.

## Verification

`gofmt -l .` empty, `go vet ./...` clean, `staticcheck ./...` clean,
`scripts/check-no-bare-go.sh` OK, `go test ./... -race -p 2` all
green, `GOOS=darwin`/`GOOS=windows` builds pass. No `.es.md` files,
no architecture copies.

## Known gaps (owned by earlier waves, not introduced here)

- Daemon boot assembly of the SQL stores is left to the H9 owner
  (recorded in 0032, not done silently); `cmd/excalibur/daemon.go`
  still builds the trigger engine from `Load()` instead of
  `AttachTriggerStore`/`RestoreTriggers`.
- Live backends (search/vision/TTS/model/channel adapters), plan
  approve/deny, worktree-backed daemon tasks, recursive `.gitignore`
  walks and turn tool execution remain fakes or deferred exactly as
  the three branch records state.
