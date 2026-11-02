# Visual artifact delivery

The client contract in Section 3.5 gains visual artifact metadata tied to a
conversation, run and tool call. Collection uses `tools.Result.Artifacts`;
model prose and client paths cannot create artifact records.

The daemon keeps the first reference for each absolute, clean output path,
in tool return order, up to 20 artifacts per run. SVG, PNG and PDF snapshots
are limited to 8 MiB each, UTF-8 source to 1 MiB, and their combined content
to 16 MiB per run. Unsupported formats, nonregular files, final symlinks and
invalid sources fail collection. Count and aggregate size limits omit excess
artifacts. The daemon stores bounded content snapshots in its existing content-addressed
blob store before recording their metadata and blob references beside the
originating run in the journal, plus a conversation index for rediscovery.
Embedded fixtures without a blob store can keep bounded snapshots inline. Snapshots
preserve the actual output when files are subsequently overwritten or deleted.
This is additive to the journal in Section 17.1 and does not alter engine
replay or the shared storage interfaces.

Collection runs in daemon tool adapters for conversational and delegated chat
turns. The engine, provider connectors and benchmark tool profiles are unchanged.

`artifact.created` carries one persisted artifact as soon as a tool returns.
`artifact.ready` carries the retained artifacts once the assistant turn or
its delegated run returns, including failure and cancellation. Both payloads
include `conversation_id` and `run_id`; each artifact also has `tool_call_id`,
its original path, MIME, byte count and creation time. Empty turns emit neither.
A journal completion marker prevents repeated ready announcements for the same
run. As with the existing bus, live publication and journal commits are not an
atomic transaction: clients recover recorded outputs through artifact listing
if a process stops between those operations.

`artifacts.get` requires `conversation_id`, `run_id` and the opaque artifact
`id`. It returns metadata, `content`, `encoding` (`utf-8` for SVG, `base64`
for PNG/PDF) and `source` (empty when none was retained). A missing ID or a
mismatched conversation/run returns `CodeUnknownTarget`. Invalid identifiers
and additional parameters, including filesystem paths, are rejected.
`artifacts.list` requires `conversation_id`, accepts an optional `run_id`,
and returns an `artifacts` array in collection order within each run. Without
a run filter it returns at most 100 artifacts from the most recently indexed
runs first. A specific run returns all its retained artifacts.

The Section 20 single local principal owns the daemon's conversations. Origin
checks isolate outputs across conversation/run scopes; this feature does not
introduce user authentication or treat the transport client ID as credentials.
Fetches read only journal snapshots, so path traversal, file replacement and
symlink retargeting after collection cannot disclose additional file content.
