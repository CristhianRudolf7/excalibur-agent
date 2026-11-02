# Native document extraction and resumable reading

Native Office, OpenDocument, EPUB, text and image metadata extraction implement
Sections 10.9, 13, 14.4, 14.7 and 17 through the existing `docextract.Extractor`
and `tools.DocumentBackend` seams. No shared extraction types change. Office
images retain the package part reference; persisting media blobs belongs to
ingestion. Logical pages never claim physical pagination.

`FileDocumentBackend.Read` delegates supported extensions to
`OfficeDocumentBackend`, which also works as an explicitly injected backend.
This makes the implementation available through both existing execution
paths. Injected test backends remain supported. The existing deferred
`document_read` schema gains an optional string `cursor`; the result adds
`next_cursor`, `untrusted`, `content_notice`, and `paginated: false`. A cursor
is `v1:<logical page>:<rune offset>`. Page text is returned without splitting
UTF-8 or truncating JSON. Sources must remain unchanged between calls.

The tool names, fixed prefix, frozen eval tool table, skill hashes and profile
hash are preserved. No API, gateway, provider or MCP/ACP contracts change.
Native readers have independent default ON switches documented in
`docs/development/agent-quality.md`. All fixtures are generated in tests.
