# 0032 — SQL wiring: repositories behind the store porter

Date: 2026-10-31. Wave w4-wire-sql (cross-milestone persistence).

## What

Every subsystem that still persisted in memory or fakes now has a
SQLite repository behind the store porter, plus migration v5
(`wiring-tables`) on the main database and v2 on telemetry.db. No new
dependencies (stdlib plus the existing store module).

## New tables (v5, all additive per Section 4.6)

`task_requirements`, `task_history`, `plan_versions`,
`instruction_revs`, `file_fingerprints`, `criterion_versions`,
`run_reports`, `run_usage`, `agenda_fires`, `trigger_events`,
`workflow_versions`, `sessions`, `worktrees`, `integration_queue`,
`project_modes`, `skill_stats`, `skill_disabled`; additive columns on
`leases` (activity), `criteria` (status), `evidence`
(instruction_rev_seen), `runs` (lane, wait/pause/block reasons),
`agent_messages` (ack_note), `projects` (agents_json), `triggers`
(enabled). Telemetry v2: `task_metrics`, `spans`.

A planned `snapshots` SQL index was dropped: SnapshotManager already
persists manifests to disk with load-on-miss, so a second index would
be a second source of truth.

## Repositories (one porter round trip per call, background context)

- tasks `SQLStore` (+ `txPlanning` over one transaction):
  `OpenSQLStore(db)`.
- orchestrator `SQLStore`: `OpenSQLStore(db)`.
- workspace `SQLProjectStore`, `SQLLeaseStore`, `SQLHistory`,
  `SQLModeStore` (via `OpenModeStore`, which loads), `SQLQueueStore`,
  `SQLWorktreeIndex`; in-memory structs keep working-set logic and
  gain write-through plus `Restore*` after restart.
- agenda `SQLAgendaStore`, `SQLReminderStore`, `SQLTriggerStore`,
  `SQLWorkflowStore`, `SQLSessionStore`; triggers attach to
  `TriggerEngine` with `RestoreTriggers`.
- skills `SQLSkillCache` plus `Index.SaveCache`/`LoadCache`.
- telemetry `SQLStore`: `OpenTelemetryStore(db)`.
- journal already had `SQLStore`; untouched.

## Contract changes (all additive)

- `PlanningStore` gains `CriterionVersion(id) (int, error)`: the
  shared Criterion struct carries no version column, and the task_db
  batch needs the check/apply pair to agree without mutating.
  Implemented by MemoryStore (map, default 1) and the SQL stores.
- `ApplyBatch` now takes `PlanningStore` instead of `*MemoryStore`:
  memory keeps clone/restore, SQL runs check+apply in one porter
  transaction, other implementations run sequentially (fakes only).
  `ApplyReplan`/`ApplyProposal` (and its `apply` callback) take
  `PlanningStore` too.
- taskdb internals (`registerProjectLocked`, `xxxLocked` helpers,
  direct map access in op closures, `statusOfLocked` map reads) were
  rewritten onto the interface and deleted. `queryEvidenceLocked`
  now scopes evidence through requirements (Section 9.5) instead of
  iterating the global map: same rows in practice, since evidence is
  always created in-scope.
- SQL errors from the porter wrap command errors: callers detect
  `VersionConflict` with `errors.As`, not a type assertion.

## Why this shape (alternatives)

- Interface generalization over parallel implementations: the logic
  (lifecycle, gate, queries, orchestration) already programs to the
  interfaces; one SQL backend each is less code than per-caller
  forks and keeps unit tests on fakes unchanged.
- JSON envelopes with synced query columns (schedules, reminders,
  workflow instances, runs reasons) instead of fully normalized
  columns: full fidelity of the Go structs across restarts with the
  fewest schema columns, and filters still run in SQL.
- Short codes allocate with the same `INSERT..ON CONFLICT..RETURNING`
  UPSERT as `store.nextShort`, inside the caller's transaction
  (calling `store.NextTaskCode` inside a batch transaction would
  deadlock the porter), so codes never repeat, even for rolled-back
  batches (Section 4.4).
- Write-through mirrors (workspace/agenda/trigger engine) instead of
  SQL-primary structs: the in-memory logic (wait loops, heartbeats,
  git ops, dedup windows) stays untouched; SQL is the crash-recovery
  mirror. Map first, porter second; restores reload on boot.

## Deferred (not this wave)

Daemon boot assembly (open the family, attach the stores, pass them
into `orchestrator.New`/`NewAgenda`/engines) belongs to the daemon
owner (H9): `Daemon.Deps` is injected, and no production call site
constructs these stores yet, so there was nothing to switch without
invading another package. The `Open*`/`Attach*` constructors are the
seam; wiring them at boot is recorded here, not done silently.
