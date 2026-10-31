# 0003 — H5 placement: gate, report and Section 18 live in internal/tasks

Date: 2026-10-31. Status: accepted.

## What

The completion gate (7.5), the delivery report (7.6) and all of
Section 18 (verification ladder, check selection, loop detector,
limits) are implemented in `internal/tasks` as engine-independent
library code, not in `internal/engine`. The package depends only on
`internal/types`, so the engine (H3) and the orchestrator (H8) can
adopt it without import cycles.

## Why

The H5 brief asks for the gate and report explicitly as
engine-independent library code, and `internal/engine` is owned in
parallel by the H3 agent (step loop, journal wiring). Placing the
deterministic gate, report, ladder, detector and budgets in
`internal/tasks` avoids edit conflicts and keeps every H5 rule
testable without an engine instance.

## Alternative

Thin wrappers in `internal/engine` delegating to `internal/tasks`.
Deferred to H3: if the engine owner wants the wrappers, they are
one-line calls with no logic of their own.
