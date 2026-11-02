# 0081: Bind document library ingestion to native readers and client routes

The document library index, durable ingestion queue and client API were built
against separate seams. Integration uses one memory service for document tools,
indexing and citations. Migration v6 installs the index before v7 installs jobs
and the OCR page cache; the temporary ingestion metadata declarations are removed.

Native PDF, Office, text and image metadata extractors are registered centrally.
The PDF reader supplies rendering and quality policy. Configured vision backends
supply the in-memory OCR adapter through the existing model gateway. Missing OCR
retains the ingester's explicit partial-document warning and page budget behavior.

The daemon binds client routes to the memory service and ingester with
server-owned scopes and converts native metadata into client update events. Its
HTTP blob store and ingester share db.Blob(). Embedded callers supplying a
MemoryStore receive an owned ingester and close it before their database. Hosts
injecting an already-bound memory service retain ownership of that worker.
No library memory is constructed for eval.

Citation sources read page labels and rectangles from indexed chunk metadata.
Generated PDF, DOCX and XLSX fixtures exercise upload, extraction, exact locations,
search references, answer rendering, OCR budgets and durable processing recovery.
Provider-backed OCR is replaced with a fake in ordinary tests.

The combined deferred catalog intentionally changes only its names hash to
1c000767c03fc87941becd0077add3cdffcc682079de4d2c57a24e7b55d03e74.
The prefix remains
95b1a140c09eb43f24691ca087d359a7630592d9c147ae2fb9b84e00fc06adae;
the eval profile remains
6bfd22cc417b7ddefb81d2193c181dcc2406d5f951ab6a90f50d1db1f9f4e025.
The existing feature switches govern this composition. Sections 10.9, 13,
14.4, 14.7 and 17 define its responsibilities.

Verification on the integrated branch passed: repository-wide gofmt, vet,
staticcheck, bare-go check, Linux/Darwin/Windows builds, capped race suites for
memory, library, daemon, gateway, API, tools, wireengine, docextract, Office,
CLI, command wiring and migrations, plus a separate 6 GiB engine race suite.
Final image-reader changes passed the full library/wireengine race suites and
all daemon library end-to-end tests. The existing CLI past-25-step regression
already has a 60-second outer deadline and passed without modification.
Web typecheck, lint and all 206 unit tests passed. No paid model calls, live
runs, benchmarks, new dependencies or assistant-turns merge were used.
