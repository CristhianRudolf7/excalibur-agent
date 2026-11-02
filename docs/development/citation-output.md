# Assistant citation output

The master receives complete text through `TurnCaller.CompleteTurn` in the
production daemon and retains `ModelCaller.Complete` for compatible callers.
There is no assistant token stream: run progress carries tool output. Before this
change, the turn discarded its result, `send_message` was a catalog stub, and
`message.created` existed only for client intake in the embedded gateway.
Child reports use the orchestrator mailbox and notices; their references must
remain literal until the master delivers the final answer.

Citation validation belongs at the master assistant persistence boundary, before
both history and `message.created`, using the answering run and its descendants
(Section 10.9). Streaming callers must hold unfinished reference markers before
passing complete answer text to this boundary.

`Master.SendAssistant(ctx, conversationID, text, tree)` is the common final
boundary. Completed `TurnCaller` and `ModelCaller` turns call it automatically.
The engine-backed caller supplies its trusted run tree. The daemon binds
its memory service and bus when `NewDaemonBackend` is constructed. Trusted run
delivery should call `DaemonBackend.SendAssistant(ctx, conversationID, text,
runID)` so the backend resolves descendants from orchestrator records. Client
and model text never supplies the authorization tree.

`memory.CiteFlow{Library: library}.Process(ctx, text, tree)` can also be used
without conversation persistence. Its `CitationLibrary` interface separates
retrieval validation, trusted source metadata and web source reads. The current
`*memory.Service` adapter uses existing title, page, paragraph, section, chunk
text, MIME and blob metadata; `SourceRef` already carries page labels and boxes
for the richer library metadata adapter at integration.

Future assistant delta adapters can use one `memory.StreamHold` per stream:
pass each delta to `Write`, emit its returned text and call `Flush` at end of
stream. Hold-back alone does not validate sources; complete answer delivery must
still use the final boundary. Incomplete markers are released after 200 Unicode
characters so a malformed response cannot retain an unbounded stream.

Backend verification covers normalization variants and ranges, split stream
markers and the 200-character bound, malformed/unretrieved references, mixed
claims and list items, first-appearance numbering, excerpt limits, missing
metadata, child-run reads, web source filtering, persisted/event JSON parity,
switch rollback, cancellation during validation and immutable history snapshots.
The tests live in `memory/citeflow_test.go`, `master/output_test.go`,
`daemon/citation_output_test.go`, `api/sources_test.go` and the citation
instruction tests in `ctxcompiler` and `engine`. They use generated text, fake
embeddings, local databases and the in-process event bus.
