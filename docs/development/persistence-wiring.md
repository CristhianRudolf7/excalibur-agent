# Persistence wiring (w4-wire-sql)

SQL-backed persistence for every subsystem that still ran on memory
or fakes: migrations, one repository per area behind the store
porter, production constructors, and the backend tests. Architecture
sections: 4 (storage), 8 (planning), 15 (subagents), 9/16/13.7
(workspace), 21 (agenda), 12.2 (skills cache), 17.1 (journal), 22
(telemetry).

Fakes stay in unit tests only. Every repository test below runs
against a real temporary SQLite database (file, not shared memory)
and includes a close/reopen crash check.

## Migrations

- Main registry v5 `wiring-tables`
  (`internal/store/migrations/v5.go`): `task_requirements`,
  `task_history`, `plan_versions`, `instruction_revs`,
  `file_fingerprints`, `criterion_versions`, `run_reports`,
  `run_usage`, `agenda_fires`, `trigger_events`,
  `workflow_versions`, `sessions`, `worktrees`,
  `integration_queue`, `project_modes`, `skill_stats`,
  `skill_disabled`; additive columns on `leases`, `criteria`,
  `evidence`, `runs`, `agent_messages`, `projects`, `triggers`.
- Telemetry registry v2 (`telemetry.go`): `task_metrics`, `spans`.
- `registry_test.go` pins main latest = 5, telemetry latest = 2.

## tasks — `internal/tasks/sqlstore.go`

`OpenSQLStore(db) *SQLStore` implements the full `PlanningStore`
(projects, tasks + reopen history, plans + queryable versions,
items, dependencies with cycle rejection, milestones, requirements,
criteria with SQL-tracked optimistic versions, checks, evidence with
freshness revisions, assumptions, decisions, logbook, task edges,
instruction revisions, fingerprints, short codes from
`short_counters`).

`ApplyBatch` accepts any `PlanningStore`: memory uses clone/restore,
SQL runs check+apply in one porter transaction (atomic batch with
in-transaction code allocation), other implementations run
sequentially (fakes). New additive method
`CriterionVersion` (see 0030). Callers use `errors.As` for
`VersionConflict` (porter wraps command errors).

Tests (`sqlstore_test.go`): full PlanningStore round trip including
version conflicts, cycle rejection, plan versions, evidence
freshness, short-code no-reuse; batch atomicity with rollback and
code continuity; reopen/close/reopen crash survival.

## orchestrator — `internal/orchestrator/sqlstore.go`

`OpenSQLStore(db)` implements `Store`: agents (hierarchy, name
search), runs (lane, scope, wait/pause/block reasons, budget,
progress), mailbox (at-least-once insert-ignore, send-order pending,
ack with note), instructions (forward-only revisions, latest, text by
revision), questions (open filter, answer), delivery reports (latest
wins), accumulating usage.

Tests (`sqlstore_test.go`): full round trip plus reopen survival of
agents, runs, mailbox order, acks, usage accumulation.

## workspace — `internal/workspace/sqlstore.go` + hooks

- `SQLProjectStore` (`OpenProjectStore`): Registry semantics
  (canonical roots, unique names/aliases) over `projects`.
  `Registry.AttachProjectStore` + `RestoreProjects`.
- `SQLLeaseStore` (`OpenLeaseStore`): grant/release/heartbeat/expire
  mirror. `LeaseManager.AttachLeaseStore` + `RestoreLeases`.
- `SQLHistory` (`OpenSQLHistory`): same API as `History`, rows in
  `file_versions`, content in the store blob store (refs stay
  content-derived `blob-sha256:<hex>`).
- `SQLModeStore` via `OpenModeStore` (loads overrides, write-through
  `SetOverride`).
- `SQLQueueStore` (`OpenQueueStore`): `IntegrationQueue` write-through
  + `RestoreQueue` keyed by integration branch.
- `SQLWorktreeIndex` (`OpenWorktreeIndex`): create/forget trail
  hooked into `WorktreeManager`.

Tests (`sqlstore_test.go`): registry round trip + restore with
settings/agents/VCS intact; lease mirror with release/restore/expire;
history record/revert across reopen with blob survival; modes, queue
and worktree index persistence.

## agenda — `internal/agenda/sqlstore.go` + hooks

- `SQLAgendaStore` (`OpenAgendaStore`): schedules as JSON envelopes
  with synced query columns; delivered set + pending queue in
  `agenda_fires` (deliveries idempotent across crashes).
- `SQLReminderStore` (`OpenReminderStore`): envelopes with
  next-fire/status sync; due/snooze/list.
- `SQLTriggerStore` (`OpenTriggerStore`): rules plus
  (rule, event) dedup marks and firing times;
  `TriggerEngine.AttachTriggerStore` + `RestoreTriggers` (dedup
  window + rate-limit hour).
- `SQLWorkflowStore` (`OpenWorkflowStore`): append-only versioned
  definitions, instances pinned to their version.
- `SQLSessionStore` (`OpenSessionStore`): sessions with heartbeats
  and handoffs.

Tests (`sqlstore_test.go`): schedule/fire round trip + reopen with
no-duplicate redelivery; reminders due/snooze/complete; trigger
dedup + rate limit surviving restart with rule removal; workflow
versions with pinned instances across republish; session CRUD.

## skills — `internal/skills/sqlstore.go`

`OpenSkillCache(db)`: cached entries, per-model stats, per-model
disabled groups, install versions. `Index.SaveCache` (entries in one
transaction, then stats/disabled) / `Index.LoadCache`; the daemon
loads, rescans for drift, and saves periodically.

Tests (`sqlstore_test.go`): entry/stats/disabled/version round trip;
save/close/load cache across restart with stats and disabled groups
intact.

## telemetry — `internal/telemetry/sqlstore.go`

`OpenTelemetryStore(db)` over a database opened with the
`Telemetry()` registry (use `TelemetryOptions`): `AppendEvent`
(returning the cursor sequence), `EventsSince`/`EventsForRun`,
`SaveMetrics`/`Metrics` (full exported snapshot), `AppendSpan`/
`SpansForTrace`.

Tests (`sqlstore_test.go`): event ordering, cursor resume, metrics
snapshot, span parent linkage, reopen survival with sequence
continuity.

## journal

Already SQL-backed (`journal.SQLStore` over the porter journal lane
plus pooled reads, `sql_test.go` with reopen test). Untouched.

## Production constructors

Each area exposes `Open*(db *store.DB)`; interface-based consumers
(`orchestrator.New`, `NewAgenda`, `AttachJournal`,
taskdb `ApplyBatch`) accept them without changes. Daemon boot
assembly (open the family, attach stores) is H9-owned and recorded
in `docs/decisions/0030-sql-wiring.md`, not done here.
