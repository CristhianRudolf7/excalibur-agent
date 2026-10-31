# 0003 — ATIF version selection and OTEL conventions pin

Date: 2026-10-31. Area: telemetry/eval (H4 measurement half).

## What

The harness emits ATIF trajectories selectable as `v1.7` or `v1.8`
(`internal/telemetry` `ATIFv17`/`ATIFv18`, default selected per trial),
and OpenTelemetry spans tagged with a pinned GenAI conventions version
(`GenAIConventionsVersion = "1.37.0"`).

## Why

The architecture document (Section 23.2) reports that Harbor docs name
ATIF v1.7 with an RFC for v1.8 and directs the implementation to emit
whichever version the pinned Harbor validator accepts; it also notes
the GenAI conventions are still Development upstream and the version
in use must be fixed. Neither the validator version nor the Harbor
checkout is available on this branch, so both versions are supported
and validated locally instead of hard-coding one.

## Alternatives considered

- Emit v1.7 only: simpler, but risks rejection if the pinned Harbor
  expects v1.8 metadata (provider, profile hash).
- Add the OTel SDK dependency now: rejected; span shape is validated
  without a collector so unit tests stay network-free, and the SDK can
  be adopted later with the version recorded in `dependencies.md`.
- Store telemetry in SQLite immediately: rejected; the H2 store owns
  the porter and migrations, so this branch uses fsynced JSONL sinks
  at the same call sites (events, metrics, trajectory), which also
  satisfies the incremental-write rule for cut trials.

## Consequences

`eval report` and the Harbor adapter accept both ATIF versions;
`Validate` enforces the v1.8 extras (provider, profile hash). The
decision is revisited when the Harbor version is pinned: record the
validator version and, if only one ATIF version survives, remove the
other.
