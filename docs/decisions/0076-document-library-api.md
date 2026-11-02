# Document library client API

The client uploads the original to `PUT /blobs/{sha256}`, then calls
`library.ingest`. Seven additive library methods expose asynchronous ingestion,
listing, metadata, pages, chunk locations, deletion and cancellation. The
`library.document_updated` event carries the document metadata directly.
This implements the client boundary for Sections 10.9, 13, 14.4, 14.7 and 17.

The daemon owns a `LibraryRoutes` service with a `LibraryBackend` seam and
server-configured scopes. Empty scopes deny access. A supplied `scope_id` must
belong to that set. Document mutations first resolve the document through the
scoped reader; the backend remains responsible for enforcing scope within its
queries and serializing mutations. Default ingestion scope is the configured
default, falling back to the first allowed scope. Duplicate ingestion is resolved
atomically by the ingester's Submit call using the digest and scope; the API
preserves its `existing` flag without creating another job.

Parameters must be objects and unknown fields are rejected. Lists default to 50
and permit at most 100 documents; pages use positive one-based ranges of at most
20 pages. Typed API errors distinguish invalid parameters, denied scopes,
unknown targets, canceled requests and backend failure. Deletion returns the
existing memory `BackupNotice` verbatim.

`NewLibraryServiceBinding` accepts local generic interfaces matching the shared
memory and ingester calls, with explicit DTO conversion functions. This keeps
the branch buildable before those parallel implementations land. Integration
must supply the native request/query/document/chunk conversions, attach
`BackendServices.Library`, and give the ingester a `LibraryServiceNotifier`.
No extraction, scheduling or memory behavior is duplicated in the transport.

The gateway's former in-memory blob store differed from memory's `db.Blob()`.
An additive `Config.DaemonBlobs` override and `StoreBlobs` adapter now connect the
production daemon upload/download paths to the same database blob store.
The adapter validates size and digest before writing. It uses the database
store's compression and layout rather than directly opening its blob directory.
`Config.BlobMIME` optionally looks up MIME by digest. A valid `?mime=` overrides
that lookup, invalid hints fall back to octet-stream, and PDF responses are
served inline. GET and HEAD both carry the metadata.

`EXCALIBUR_LIBRARY_API` defaults on and disables the seven routes when `0` or
`false` (case insensitive). No tool catalog, eval profile, skill hash or model
connector changes are introduced. The shared extraction contract is unchanged.

Verification covers all seven methods through the gateway, strict parameter and
scope rejection, duplicate ingest results, deletion notices, the service binding,
document events and uploads read back through `db.Blob()`. On November 2, 2026,
the full memory-capped race suites passed for API (1.098 seconds), daemon
(68.342 seconds), gateway (4.063 seconds on rerun), and the touched CLI package (16.705 seconds).
The first gateway run
failed the existing WebSocket subscription test's ten-second deadline; no code
or test deadlines were changed before rerunning the complete gateway suite.
Repository-wide vet, staticcheck, Linux/Darwin/Windows builds, formatting and
bare-goroutine checks passed. Web typecheck, lint and all 133 tests across
23 files passed. Go checks used low priority and bounded concurrency; race
tests ran in scopes with a 3 GiB memory cap and no swap.

An earlier full daemon run exposed a pre-existing memory-review race in
`TestServicesStartAndStopCleanly`: the store writer appends to `linkGraph`'s
`warnings` at `internal/memory/review.go:625` while the review caller returns
that slice at line 652. `store.DB.Write` can return on context cancellation
while its callback is still executing. The memory/store owner must reconcile
callback completion and captured result ownership; those files are outside
this task. The current passing suite does not establish that the earlier
race was fixed. Earlier principal initialization deadline failures also remain
recorded in the task status history; their test setup and deadlines were not
changed by this task.
