# 0024 — H9 gateway, daemon and master seams

Date: 2026-10-31. Scope: milestone H9 (Sections 3.2, 3.5–3.7, 5, 6,
15.5–15.8 via the H8 orchestrator, 17.3, 22.1, 26.11/26.13).

## What

- The gate bus IS the telemetry bus: `Server.PublishEvent` writes to
  the shared `telemetry.Bus`, so subscriptions, replay and the sink
  share one sequence. No bridge goroutine exists; `ForwardBusToSubs`
  documents that the wiring is the absence of a bridge.
- `DaemonBackend` composes the master (conversation), the H8
  orchestrator via `OrchControl` (runs) and the embedded
  `MemoryBackend` for the task/files/passthrough families. The
  task.* delegation surface is served today; the SQL task store can
  replace the embedded registry behind the `Backend` interface
  without touching the gate.
- Intake runs the decider for natural-language control/status
  queries too (not only for unresolved cases): the fast lane needs
  the decider confidence before serving them (Section 6.1 table
  wording "preanalysis plus decider with high confidence").
- `DefaultDecide` takes explicit `uncertain`/`waitingUser` flags:
  the daemon supplies reconciliation facts; the pure table stays
  testable without the journal.
- Quarantine is conservative by design: the in-memory rule holds a
  run that crashed twice in-window, and boot history carries the
  full three-strike step match. Releasing is always explicit.

## Why

- One bus removes a whole class of gap/duplication bugs between a
  live feed and a persisted log: the cursor is the bus sequence.
- Composition keeps H9 inside its packages: the orchestrator and
  task stores stay owned by their milestones; the daemon only
  adapts.
- The decider-on-control reading is the only one under which the
  "ambiguous target asks" row can carry a confidence at all.

## Alternatives considered

- Separate gate bus with a forwarder: rejected (two sequences to
  reconcile, cursor ambiguity on failover).
- Fast lane serving NL control on preanalysis alone: rejected (no
  confidence source; contradicts the Section 6.1 table).
- Automatic quarantine release after the quiet window: rejected
  (Section 17.3 demands an explicit order for the likely cause).
