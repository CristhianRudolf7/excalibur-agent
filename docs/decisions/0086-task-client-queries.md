# 0086: Read-only task client queries

Add `tasks.query` and `tasks.get` to the client API without changing model
schemas, the gateway backend interface, or eval pins. Routes use the existing
passthrough seam and read the SQLite planning store. The daemon's `task_db`
provider uses that same store; its definition stays unchanged.

The client status mapping is:

| Durable state | Client status |
| --- | --- |
| open | pending |
| in_progress | in_progress |
| paused, blocked | blocked |
| completed, completed_with_pending | done |
| failed | failed |
| canceled | cancelled |

Levels n1, n2 and n3 map to brief, project and initiative. Titles use the
request interpretation, falling back to the original request. Criteria map
satisfied to met and not_applicable to n/a; failed and unverified keep their
names. Criterion status reuses the existing evidence/fingerprint/instruction
freshness calculation; no stored flag can turn missing evidence into a pass.
Completed tasks with pending criteria retain those criteria states.

Pages sort by immutable task ID. The opaque cursor encodes the last returned
ID, so newly inserted preceding rows do not shift subsequent pages. Counts
apply all supplied filters, before pagination. The default limit is 50 and the
maximum is 200. Project filters use the task's project scope. Text matches the
case-insensitive title, original request and short code.

Detail includes linked requirements, criteria, check evidence, task-owned
plans with blocking dependencies, and stored runs. Run creation and terminal
update timestamps supply started_at and ended_at because the durable run
schema has no separate execution start/end columns. A blocked root run supplies
blocked_reason. Task-scoped notebook fragments come from identity_docs/doc_fragments, ordered
by position. General and project notebooks are excluded to keep unrelated
notes out of task detail. The current task schema has no deadline column, so
optional due_at is omitted rather than fabricated.

`SQLStore.OnTaskUpdated` is configured before serving. Task lifecycle and
criterion, evidence and freshness writes capture the previous state within
the transaction and emit
`task.updated` only after a successful commit. Atomic task_db batches share
this hook. Failed writes and unchanged states emit nothing. New check evidence and
invalidation through instruction revisions or file fingerprints also notify
when the computed criterion state changes. The event contains
the final task summary, including criterion totals. Existing store interfaces
remain unchanged; the callback is additive to the concrete SQL store.

Wire shapes are in internal/api/task_reminder.go, the shared web contract and
packages/contracts/task-reminder.json. Tests cover mapping, filters, counts,
keyset pagination, detail joins, post-commit notifications, conflicts and RPC
routing.

## INT-2 integration merge

The integration merge keeps the tasks/reminders surface alongside the skills,
usage and whiteboard additions. The merged catalog passes both daemon/embedded
parity tests with BE-4's embeddedToolsHash
`cb7e3eb40a79a35320c2bd4bf3ba0c94aa875e1c57d8d252490190187dec3dbd`.
This is the only pin changed relative to the pre-merge BE-1 branch: whiteboard
adds a deferred tool name; reminder wiring fills an existing provider and
requires no further name-hash change. The prefix pin remains
`95b1a140c09eb43f24691ca087d359a7630592d9c147ae2fb9b84e00fc06adae`
and the eval profile pin remains
`6bfd22cc417b7ddefb81d2193c181dcc2406d5f951ab6a90f50d1db1f9f4e025`.
The Go registries and JSON exports contain the same ordered 72 methods and
25 events, preserving every entry from both merge parents.
