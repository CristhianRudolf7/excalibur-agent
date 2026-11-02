# 0092: Persistent conversation whiteboards

The INT-2 whiteboard contract adds five client methods, six event types and
one deferred agent tool. Existing model tool definitions, provider protocols
and eval configuration remain unchanged.

`internal/whiteboard` owns validation, service behavior, snapshots and tool
execution. Shared drawing and event types live in `internal/api/whiteboard.go`.
The gateway's existing registered-method passthrough dispatches the methods to
the daemon's whiteboard service. Production startup binds that same service
to the model tool, the bus and the existing database. Conversational turns
carry their trusted conversation ID in context; model arguments cannot forge
it. Embedded default/delegate runs use their run ID as conversation identity
and run-local storage unless a persistent service or database is supplied.

Migration 8 adds active boards, immutable version checkpoints and an op log.
All board changes use the porter. Client idempotency is scoped to a board and
checked before the version check. A stale version returns `conflict` with
`data.current_version`; it applies no changes. The UI fetches and rebases its
intended element patches against that version. Updates merge supplied fields
into the current element, leaving unrelated elements intact. Authorship is
assigned at the boundary and preserved on updates. Calls are bounded to 500
elements/IDs and 2 MiB; a board also has a 2 MiB serialized cap. Deletes remove
the active board and retain version/audit evidence. Presentation operations
have version checkpoints and focus persists the viewport. A cancelled step
sequence retains its completed operations and stops before subsequent steps.

An open without a title or board ID reuses the active/default board. A title
without a board ID creates another board; subsequent implicit operations use
that active board. A restart selects the oldest surviving board as default.
Read and list are observations and do not advance the board version.

Snapshot requests are registered before publishing the event. The UI uploads
a PNG through the existing blob endpoint and replies using
`whiteboard.snapshot`. The response accepts an additive optional `request_id`
for exact correlation; board-only responses work when exactly one request is
pending for that board. Unknown, ambiguous and expired replies are rejected.
Reads wait eight seconds by default, honor cancellation and return element
data with `image_timeout: true` when the UI does not respond. PNG export uses
this same handshake and returns a blob reference. JSON exports preserve the
complete scene; native SVG exports retain drawing primitives and textual
content of math, Mermaid and code widgets. The UI owns KaTeX, Mermaid and
syntax rendering; PNG captures that rendered appearance.

The new builtin `whiteboard-teaching` skill belongs to `creative-tools` and is
excluded from the frozen eval skill index. The tool is registered deferred
for default/delegate, excluded when building the eval registry, and admitted
on the conversational assistant's explicit allowlist. Existing tools are
unchanged. `EXCALIBUR_WHITEBOARD` defaults on; `0`, `false` and `off` disable
registration, profile admission and API/tool use.

Verification is recorded in `docs/development/whiteboard.md`.
