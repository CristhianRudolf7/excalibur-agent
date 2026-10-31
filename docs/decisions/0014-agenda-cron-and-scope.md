# 0014 — Agenda: own cron parser, package scope, no new dependency

Date: 2026-10-31. Area: `internal/agenda` (Sections 17.4, 17.6, 17.7, 21).

## Decision 1: own cron parser instead of robfig/cron

The architecture (21.1) allows `github.com/robfig/cron/v3` or an own
expression. The package implements its own 5/6-field parser with no
new `go.mod` entry.

- What: `ParseCron` + `CronSpec.Next` in `internal/agenda/cron.go`.
- Why: exact DST semantics (nonexistent wall times skipped, ambiguous
  resolved to the first occurrence, verified after gap resolution)
  are easier to guarantee and test in a small owned implementation
  than by auditing a third-party scheduler's zone handling; it also
  avoids a cross-agent `go.mod` conflict and any license review.
- Alternative: adopt `robfig/cron/v3` (MIT) later if richer syntax
  (descriptors like `@daily`, seconds-first dialects) is needed; the
  `CronSpec` boundary keeps that swap local. Recorded in
  `docs/decisions/dependencies.md` as available but not adopted.

## Decision 2: self-contained package with narrow seams

`internal/agenda` defines its own `AgendaStore`, `ReminderStore`,
`Launcher`, `StepExecutor`, `Confirmer` and `EventWaiter`
interfaces with in-memory implementations, and depends only on the
standard library plus `internal/safego`.

- Why: the daemon, orchestrator, journal, store and tools packages
  are owned by other agents editing in parallel; depending on their
  unfinished APIs would break the build daily. The seams mirror the
  architecture's contracts (durable steps, fire_id idempotency, P3
  lanes) so adoption is a thin binding, and every behavior is
  covered by tests without paid APIs or a live clock.

## Decision 3: loop iterations run in deterministic scratch children

Re-executing a loop body must not be confused with crash resume
(which skips recorded nodes). Each iteration runs in a scratch
instance with a deterministic ID (`parent/loop#index`), records
merge into the parent in order, and a crash mid-loop replays the
loop with stable per-iteration idempotency keys (17.7), so effects
dedupe instead of duplicating.

## Format note

`fire_id` values (`fire_` + 12 random bytes hex) are local to this
package; no `internal/types` prefix was added because the shared
`types` package is owned by all agents and the document fixes no
format. The daemon can adopt these IDs verbatim into its journal
keys.
