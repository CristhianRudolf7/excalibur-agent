# 0015 — Orchestrator engine seam and embedded-mode serialization

Date: 2026-10-31. Status: accepted.

## What

`internal/engine` is owned by the H3/H5 agents and not merged yet, so
the orchestrator defines the seam it needs as `orchestrator.Runner`
(run id, interrupt request, directive delivery, live snapshot) plus
`orchestrator.Hooks` for planning progress, evidence invalidation,
prohibition judgment, external-action knowledge, lease release and
tool killing. Scripted engines stand in from
`internal/orchestrator/fake`. The real engine implements `Runner`
without changing the orchestrator.

Control orders serialize on the orchestrator mutex in embedded mode,
which is the arrival-order serialization Section 15.8 requires. The
daemon will front the same methods with an inbox; the semantics do
not change. The gateway lanes already run as a genuine Section 5.1
actor (one goroutine, bounded inbox, `safego.Go`).

## Why

Depending on an interface instead of a concrete engine keeps H8
testable and mergeable without touching engine-owned code, and keeps
every Section 15.10 and 23.5 case runnable with `-race` in embedded
mode today. A second engine-shaped interface in the orchestrator
would duplicate the contract; when `internal/engine` lands, its
runner type should implement `orchestrator.Runner` directly.

## Alternative

Block H8 on the engine merge. Rejected: the milestones run in
parallel precisely so the seams, not the merges, carry the schedule.
