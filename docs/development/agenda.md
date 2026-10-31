# H10/H13 — Agenda, workflows and flow simulation

Contract and backend tests of the automation area: agenda and time
zone (21.1), reminders (21.2), event triggers (21.3), durable
workflows (21.4), retries in autonomous and scheduled work
(21.5/17.7), textual flow simulation (21.6), built-in workflows
(21.7), self-initiated research hooks (21.8), internal scheduled jobs
(21.9), missed schedules (17.4) and long multi-day jobs (17.6).

All logic lives in `internal/agenda` as daemon-independent library
code: it depends only on the standard library plus
`internal/safego` for its timer and parallel fan-out. Persistence
goes through the narrow `AgendaStore` / `ReminderStore` interfaces
with in-memory implementations used by every test; the daemon backs
them with SQLite. Work delivery goes through the `Launcher`
interface; step effects through `StepExecutor`; user confirmations
through `Confirmer`; event waits through `EventWaiter`. No new
`go.mod` dependency was added (see
`docs/decisions/0014-agenda-cron-and-scope.md`).

## Contract

- **Time zone (21.1)**: `timezone.go`. The user zone is an IANA name
  (device zone by default, confirmed at the profile layer); every
  fire time is stored in UTC with the zone it was expressed in.
  `time/tzdata` is embedded so behavior never depends on the system
  database. `ResolveWallTime` maps wall times explicitly: normal
  times to themselves, spring-forward gaps to the next valid wall
  time, fall-back overlaps to the first occurrence. Candidate UTC
  instants derive from the zone offsets around the date, never from
  the runtime's unspecified gap/overlap choice.
  `RecalculateForZoneChange` keeps local wall times and recomputes
  `next_fire_utc` with a per-schedule report when the user moves
  zones.
- **Cron (21.1)**: `cron.go`. Own 5/6-field parser (minutes, hours,
  dom, month, dow, optional seconds; `*`, steps, ranges, lists,
  month/dow names) evaluated always with an explicit zone.
  `CronSpec.Next` returns the first fire strictly after the base,
  skipping nonexistent DST wall times and re-checking the fields
  after gap resolution. Standard cron day semantics (dom OR dow
  when both are restricted). A progress guard keeps pathological
  normalizations from stalling; expressions with no fire inside the
  search bound (February 30th) return an error.
- **Schedules (21.1, 17.4)**: `schedule.go`. Kinds `at`, `every`,
  `cron`, `after` (fires on dependency completion via
  `AfterComplete`) and `window` (low-activity window spec).
  `every` anchors to the stored `NextFireUTC` so fires stay on grid
  across restarts. Missed policies `run_once_now` / `skip` /
  `run_all`; every fire carries a `fire_id` idempotency key and
  `ReconcileMissed` advances the schedule past the downtime window.
  Series enumeration is capped (`maxMissedSeries`).
- **Agenda service (21.1, 17.3)**: `agenda.go`, `store.go`. The
  service keeps the next firing in memory (a timer over the minimum
  `next_fire_utc` across schedules and reminders) and persists
  every firing with its `fire_id`, so a crash during delivery never
  duplicates. `Tick` is the single firing path used by the timer
  loop and by tests. Safe mode registers due fires as pending
  without launching; `ExitSafeMode` flushes them in order.
  `ReconcileBoot` replays 17.4 per schedule after downtime.
  `MemoryAgendaStore` records delivered `fire_id`s (17.4 dedup).
- **Reminders (21.2)**: `reminders.go`. Create, list, snooze,
  complete and delete; optional cron recurrence in the user zone;
  unacknowledged reminders repeat per `RepeatPolicy` (default 3
  repeats every 30 minutes, an initial value to calibrate);
  acknowledged or non-recurring reminders end.
- **Triggers (21.3)**: `triggers.go`. Normalized events (type,
  source, payload, id); rules with filter (types, sources,
  case-insensitive content match), action (`start_task`,
  `launch_workflow`, `notify`, `start_session`), per-rule
  `MaxPerHour` rate limit and per-(rule, event) dedup window
  (default 24 h).
- **Workflows (21.4)**: `workflow.go`, `engine.go`. Versioned graph
  of typed nodes (`tool`, `agent`, `script` in any language,
  `condition`, `wait` for time/event/user, `parallel`, bounded
  `loop`, `subflow`, `confirm`) with typed edges (`When`
  conditions) and `ExportGraph` (nodes + connections JSON for an
  n8n-style UI). Validation enforces node types, edge references
  (conditions parsed at definition time), a single entry,
  reachability from the entry (composite children count as entered
  through their parent) and bounded loops. Instances pin the
  definition version; only an explicit `Migration` retargets them.
  Every node is a durable step (`StepRecord` with node + instance
  idempotency key); resume replays recorded outputs and skips
  completed nodes. Dry run substitutes declared `dry_run`
  behaviors and never executes effect tools without one
  (recorded as skipped). Condition nodes and edge guards share a
  deterministic expression evaluator (`==`, `!=`, comparisons,
  `&&`, `||`, `!`, `contains`, dotted paths). User confirmations
  block as `waiting_confirm` and resume from the stored answer; a
  decline aborts through reverse-order compensations. `RunTests`
  runs the per-version definition tests with a mock executor that
  refuses to invent effect outputs.
- **Retries (21.5/17.7)**: `retry.go`. `ClassifyFailure` maps errors
  to transient/input/logic/permanent; `DecideRetry` applies the
  17.7 backoff (30 s, 2 min, 10 min, 1 h, at most 4 attempts,
  `Retry-After` wins) with no blind retry for input/logic and
  pause-and-notify for permanent. Retries reuse the same node +
  instance idempotency key; `CompensationOrder` reverses completed
  nodes for saga abort (compensations must themselves be
  idempotent).
- **Flow simulation (21.6)**: `flowsim.go`. Canonical one-line-per-node
  format parsed by `ParseCanonicalFlow`; `CountWalkthroughs`
  predicts the total with loops unfolded to k turns before
  enumerating; `Simulate` covers decisions, then interacting pairs,
  then exhaustive only when the total fits `ExhaustiveLimit`
  (500, Appendix B) — otherwise it refuses and the report says
  sampled, never exhaustive. `Walkthrough.Descriptor` (`D1=yes,
  B2x2`) is lossless: `checkRoundtrip` regenerates every
  walkthrough from its descriptor (loop turns replay per-turn
  decision queues; `BxN` markers fix turn counts). `Narrate`
  renders deterministic template narrations; `Review` reports
  unexpected ends, dead ends, planted errors, revisits (possible
  endless loops) and decisions that never vary.
- **Built-ins (21.7)**: `builtin.go`. The six workflows with
  version-1 tests: `deep-research-run` (bounded research rounds,
  evidence table, gap-driven extra rounds, cited report),
  `decision-debate` (specialist branches, bounded debate rounds,
  evidence-or-insufficient-evidence recommendation),
  `job-application-pipeline` (profile, search, bounded review loop,
  explicit confirm node before the effectful send with declared
  dry run and compensation, follow-up), `information-monitor`,
  `course-builder`, `project-session` (17.6 session shape).
  `ConfirmBeforeSend` proves every send-like effect node is
  confirmation-dominated on all entry paths;
  `VerifyCitedSources` enforces cited-only-read for 21.10.
- **Curiosity (21.8)**: `curiosity.go`. Proposals from interests,
  open questions and contradictions with objective, budget and
  validation criteria (cited sources + stated limitations by
  default); under-budget pre-approval, otherwise user approval;
  rejected proposals never run.
- **Internal jobs (21.9)**: `internaljobs.go`. The ten jobs with
  default cron cadences, all in lane P3; window-only jobs wait for
  the low-activity window and every job yields to user activity
  (`ShouldRun`).
- **Sessions (17.6)**: `sessions.go`. Heartbeat watchdog (stuck
  after 10 minutes running without heartbeat unless a long tool is
  registered), startup verification step, handoff notes, and
  `SessionChain` ordering (handoff satisfies dependencies).
- **Tools**: `tools.go`. The five registry names with short
  descriptions: `schedule`, `reminders`, `trigger_rules`,
  `workflow`, `flow_simulate`.

## Backend tests

- `timezone_test.go`: DST gap advances (02:30 to 03:00), ambiguous
  resolves to the first occurrence, cron skips the nonexistent
  02:30 on spring-forward day, zone change recalculates 09:00
  across zones, unknown zones rejected (21.10 row 1).
- `cron_test.go`: 5/6-field parsing, steps/ranges/names, next-fire
  basics and strict-after semantics, dom/dow OR semantics,
  impossible dates error, seconds precision.
- `schedule_test.go`: validation matrix, anchor-stable `every`
  grid, the three missed policies with unique ordered `fire_id`s,
  one-shot done flag, window inside/before semantics (17.4/17.8).
- `agenda_test.go`: tick fires once and records delivery (no
  double fire), safe mode parks then flushes in order, boot
  reconcile delivers missed fires, `after` fires exactly once on
  dependency completion, timer start/stop/restart.
- `reminders_test.go`: lifecycle (snooze/complete/delete),
  repeat-while-unanswered with cap, ack ends recurrence-free
  reminders, cron recurrence across the DST transition (21.10
  row 2).
- `triggers_test.go`: filter matching, (rule, event) dedup,
  hourly rate limit with reopening window, disabled rules and
  validation (21.10 row 3).
- `retry_test.go`: class markers, exact backoff schedule and
  attempt cap, `Retry-After` precedence, no-retry classes, stable
  idempotency keys, reverse compensation order (17.7/17.8, 21.5).
- `workflow_test.go`, `engine_test.go`: graph validation (bad
  refs, unreachable, unbounded loops, bad conditions), version
  pinning with explicit migration, resume without repeating
  completed nodes, dry-run skip of undeclared effects, confirm
  wait/resume/decline paths with reverse compensations, edge
  routing and no-match failure, parallel merge determinism and
  race freedom, loop bound and exit condition, subflow version
  pinning, transient retry with same key, no blind retry on
  logic failures, wait continuation (21.10 row 4).
- `flowsim_test.go`: count precedes enumeration, decision
  coverage, exhaustive report, over-limit refusal with pair
  fallback, loop unfolding with descriptor round-trip, planted
  error detection, dead-end findings, never-varying decisions
  (21.10 flow-sim row).
- `builtin_test.go`: all six validate with bounded loops, confirm
  dominance for the send node (and detection when stripped), dry
  run never runs send, declined confirmation aborts before send,
  every definition test passes, cited-sources check, debate
  rounds bounded (21.10 row on 21.7).
- `hooks_test.go`: curiosity approval ladder, ten internal jobs
  with P3 lanes and yield-on-activity, session watchdog
  (exemptions included), chain ordering with handoff, tool
  surface (21.8, 21.9, 17.6, 21.10).

## Deferred (not in this package)

Daemon wiring (timer supervision, orchestrator launcher binding,
SQLite backing of the stores, question-flow confirmer, capability
event sources), the `schedule`/`reminders`/`trigger_rules`/
`workflow`/`flow_simulate` registry bindings in `internal/tools`,
and live clock skew handling beyond monotonic UTC storage.
