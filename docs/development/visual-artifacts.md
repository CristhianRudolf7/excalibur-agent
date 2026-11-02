# Visual artifact backend

The daemon retains visual files returned through `tools.Result.Artifacts` in
assistant turns and delegated chat runs. This extends the client surface in
Section 3.5 and uses the durable journal and content-addressed blobs from Sections 17.1 and 4.2. Decision 0098 records
the contract and limits. The tool surface and eval profile do not change.

A client can subscribe to `artifact.created` for progress and `artifact.ready`
for the completed turn's outputs. Event payloads carry the conversation and
run IDs. The ready payload contains `artifacts`; the created payload contains
one `artifact`. Each metadata record carries its opaque ID and tool call ID,
kind, title, format, optional dialect/group, MIME, size, creation time and
original file path. Ready events also cover failed and canceled turns that
produced outputs; turns without outputs do not emit artifact events.

To fetch an output, call `artifacts.get` with `conversation_id`, `run_id` and
`id` from its metadata. The response contains `artifact`, `content`, `encoding`
and `source`. SVG content is UTF-8 text; PNG/PDF content is base64. Source is
UTF-8 text, or empty if the tool did not retain it. Files are snapshotted at
collection time, so fetch is independent of later filesystem changes.

After reconnecting, call `artifacts.list` with `conversation_id`. It returns
an `artifacts` array, bounded to the latest 100 records. Include `run_id` to
retrieve all retained records for a particular turn. Conversation and run
identities are checked against each artifact's persisted origin. The local
single-principal daemon has no separate client authentication in this feature.

Backend verification covers:

- Path deduplication, return order, tool call attribution and metadata.
- Count, per-file, source and aggregate byte limits; unsupported formats,
  invalid UTF-8, nonregular files, symlinks and unclean paths.
- Created events before model completion and one ready event after success,
  model failure, tool failure or cancellation, including concurrent completion.
- Scoped RPC fetch/list, unknown IDs, traversal attempts, extra path parameters,
  all three formats and content/source limits on persisted records.
- SQLite close/reopen, stable IDs, rediscovery and retrieval after file deletion.
- Registry parity and exported JSON schema fields/required properties.

Run the API, gateway and daemon Go tests with the race detector. All providers
and tools used in these tests are local or scripted; no paid API is called.
