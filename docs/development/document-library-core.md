# Document library core

The memory service implements extracted document persistence and scoped
retrieval for Sections 10.9, 13, 14.4, 14.7 and 17. Extraction engines and
background scheduling are separate collaborators. The service accepts the
shared `docextract.Document` representation without changing that contract.

Uploads start with `CreatePendingDocument`. Blob digests deduplicate within
a scope, including concurrent calls serialized by the store writer. A supplied
existing ID stages a replacement upload. `SetDocumentProgress` records pending,
processing, ready, partial, failed or canceled state with bounded page progress.
Ingestion refuses a canceled document and uses an optimistic version/content
check to avoid publishing results over a concurrent extraction.

`IngestExtracted` stores complete pages, structural chunks and physical
rectangles. Headings bound sections. Child chunks target 400 approximate
tokens, with about 12 percent overlap at sentence or paragraph boundaries.
Tables remain separate and large tables split by rows with repeated headers.
Text from adjacent pages can share a chunk while its rectangles retain each
page number. Page labels, slide numbers and spreadsheet anchors feed citation
rendering. Unpaginated prose continues to cite sections and paragraphs.

Embedding calls run in batches of 64 outside the write transaction. Current
embedding-space vectors from identical chunks in the preceding version are
reused. Unchanged extracted text advances the version without changing chunk
IDs; page snapshots are stored for the new version. Historical chunks from
changed text remain inspectable, while search uses the current version.
FTS indexes title, source name and section path in addition to text; embedding
inputs retain only source text and the existing context sentence. Failed
embedding calls queue pending vectors and preserve lexical search.

`ListDocuments`, `GetDocument`, `DocumentPages`, `ChunkDetail` and
`GrepDocument` enforce the service's scope set. Lists use stable upload-time
and ID cursors. Pages are limited to 20 per request. Grep is bounded by the
match count, pattern length, chunk count and scanned bytes. `LibrarySearch`
accepts document IDs before candidate ranking. `ExpandHit` adds adjacent
same-section text and tables without changing the cited chunk.

The shared catalog registers four deferred read tools for default and
delegate, with the same service binding for embedded and daemon callers.
The existing library search provider returns text, neighbouring context,
`[[doc:ID#chunk]]` references and human citations. Every library tool labels
source content as untrusted. The existing fixed search stub stays in its
frozen catalog location; see decision 0075. Embedded callers supply
`wireengine.EmbeddedOptions.Memory`; daemon assembly binds its memory service.

Backend tests generate all document fixtures in Go. They cover v5 migration,
FTS metadata enrichment and deletion, upload deduplication, state validation,
page limits, cursors, scopes, regex limits, cross-page rectangles, page labels,
slide and sheet citations, embedding batches and reuse, unavailable embeddings,
version filtering, deletion, tool discovery and invocation through both paths,
and default/delegate/eval gating. No live extraction or model call is required.
