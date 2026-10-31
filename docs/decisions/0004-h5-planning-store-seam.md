# 0004 — H5 persistence: PlanningStore seam with an in-memory implementation

Date: 2026-10-31. Status: accepted.

## What

Planning rows live behind the `PlanningStore` interface defined in
`internal/tasks/store_mem.go`, with `MemoryStore` as the bundled
implementation. `task_db` batches validate and apply against a
private copy and swap it in only on full success; the SQL adapter
must run both phases in one porter transaction.

## Why

The SQLite porters and migrations belong to the H2 (storage) agent,
which works in parallel. Depending on the `Store` SQL surface
would either duplicate its schema decisions or block on them. The
interface keeps H5 logic complete and tested today (every test uses
`MemoryStore`, no paid APIs, no SQLite) and gives H2 a precise seam
to bind later without touching the logic.

## Alternative

Writing SQL against an assumed schema. Rejected: the schema is
another agent's decision, and guessing it would create conflicts.
