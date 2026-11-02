# Durable background document ingestion

Sections 10.9, 13, 14.4, 14.7 and 17 require ingestion to preserve originals,
exact extraction positions, cancellation and recovery. The daemon owns a
bounded worker pool over the v7 `library_jobs` queue. A separate page table
checkpoints completed OCR, so native extraction can repeat after interruption
while successful OCR results are reused. Explicit cancellation is terminal;
shutdown leaves unfinished work resumable. The initial attempt has two retries.

The worker depends on local interfaces matching the shared document-library
contract. The memory service owns document metadata, chunking and embeddings.
OCR runs only on image inputs and PDF pages selected by the injected policy
(or method/quality fallback), under a configurable per-document page budget.
Partial results explain missing OCR. Notices use the existing master notifier;
upload observations go through memory proposal with document provenance.

`library_ingest` accepts path or blob digest and returns pending document
metadata immediately. Its legacy text arguments retain synchronous behavior.
The shared embedded catalog now exposes this deferred tool; it previously
existed only through daemon capability wiring. The names hash changes from
`b9bedd828535af82488a5a7034279a376a3165377cdac3c87adddf7d7b4408eb` to
`10061d02781f3ff61b92a4fd265ccd6358ed381345a4c377c14f0f040c4c7add`.
The core prefix, eval tool table, skill hash and eval profile hash do not change.
The real tool definition permits default and delegate profiles only.

The parallel library-core branch supplies migration v6 and the canonical
DocMeta/DocumentInfo methods. Until integration, the standalone ingest branch
has a types-only bridge in `internal/memory/library_ingest_contract.go`, and
daemon construction checks the local Store interface before binding. Delete
the bridge when integrating library-core. Register v7 after v6; never apply a
placeholder migration v6. `RegisterDefaultExtractors` is the integration hook
for PDF and Office; this branch registers only its plain text/Markdown fallback.
The API owner connects document progress events through the Notifier interface
and shares the daemon blob store with uploads.

Queue persistence and document creation use separate existing service
transactions. Repeating submission repairs a pending document whose queue
insert was interrupted. Terminal notices and memory proposals are best effort
after the durable terminal state; they are not a durable transactional outbox.
