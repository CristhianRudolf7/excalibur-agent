# My data

## Backend

The personal collections engine uses `collections.db` in the daemon data directory,
independently of the harness database. Collection and field metadata, ten retained
schema snapshots (including records), and one typed table per collection persist
through restarts. Renaming a field changes metadata only. Schema alterations are
atomic and support conversion dry runs. Invalid conversions block the entire edit.
Required field additions to nonempty collections require first supplying values;
values are never invented. Collection deletion retains data for thirty days;
expired trash is purged at database open. Record and collection deletion report
incoming relation references instead of silently cascading.

Record values use field IDs. CRUD accepts decimal commas, Spanish booleans, ISO
dates and Spanish day/month/year dates. Queries support field filters, text search,
validated sort columns and opaque cursors bound to their query and schema version.
Pagination uses a deterministic ID tie breaker; concurrent inserts/deletions can
shift offset pages. Summary cards use SQL aggregates, including weighted averages
that exclude missing values and return null for zero total weight.

Engine coverage: `internal/collections/engine_test.go` exercises all field types,
required/unique/range checks, persistence, conversion failure and rollback, schema
snapshots, trash expiry and restoration, incoming/outgoing relations, filtering,
sorting, pagination, and SQL summary cards.

Imports read CSV (comma, semicolon or tab, UTF-8/BOM or Latin-1) and XLSX from
daemon blobs. The existing bounded OOXML package reader supplies the complete cell grid;
a style pass converts Excel serial dates, including the 1904 epoch. Source row numbers survive leading
blank/title rows and sparse sheets. Schema inference and exact, accent-insensitive,
then unambiguous fuzzy mapping are overridable. Automatic trimming, decimal commas,
select casing, and date changes are logged. Ambiguous slash dates require ISO
input. Conflicts identify row, column, source value, problem, and a suggested fix.
Valid rows commit together; invalid rows are skipped. Full normalized row hashes
prevent exact duplicates; an upsert key updates a single matching record and reports
multiple matches as conflicts. Dry runs execute the same transaction then roll back.
Without a collection or `create_schema:true`, imports only return a proposal.

Exports produce daemon CSV/XLSX blobs and preserve typed values on round trip.
XLSX reuses the native sheet writer and OOXML container, independently of the
document preview extractor's 20,000-cell limit. CSV/XLSX imports support 100,000
rows, 128 columns, 1,000,000 grid cells, and blobs up to 64 MiB; limits fail
explicitly rather than silently dropping rows. Exports enforce the cell budget
and support filters for larger collections. Untrusted formulas are never
executed; XLSX exports store user strings as literal cells.

`internal/collections/importer_test.go` covers Spanish decimal-comma CSV,
Latin-1, tabs, title/header detection, inference, accent/fuzzy/explicit mapping,
mixed date conflicts, duplicate/upsert behavior, relation conflicts, dry runs,
automatic fix logs, sheet selection, rows beyond preview limits, Excel serial dates,
CSV/XLSX typed round trips, and complete export/import beyond 20,000 cells.

The daemon opens the collections store during production startup, passes its blob
adapter to imports/exports, and closes it after draining turns during shutdown.
All fourteen shared JSON-RPC methods route through the gateway to CollectionRoutes.
Validation returns `-32602` and `data.errors`; missing targets return `-32001`.
Mutation events publish `collections.changed` over the existing WebSocket feed.
Failed writes and dry runs do not publish events. JSON schema exports live in
`packages/contracts/collections.json`; Go/schema parity tests cover every shape.

`internal/daemon/collections_test.go` exercises all RPC methods through HTTP,
validation payloads, invalid parameter handling, mutation counts and a real
WebSocket subscription. The pinned embedded/eval hash test passes unchanged.

The `my_data` tool is registered only by daemon assistant wiring, deferred in the
catalog and restricted to the `default` profile. It is explicitly allowed in chat.
Its description teaches proposal-first creation, direct record changes, spreadsheet
attachment offers, import conflict handling, and localized navigation notices.
`propose` returns a readable schema table and three clearly fictional examples
without writing data. Creation/deletion and destructive schema operations require
`confirmed:true`; conversion previews do not. Imports default to dry runs and need
`commit:true` to write; creating an inferred schema also requires confirmation.
Schema snapshot restoration requires confirmation because it replaces records.
Trashed collections restore without overwriting active data. Exports include blob
URLs. Tool notices include English and Spanish; agent explanations follow the
conversation language.

`internal/daemon/collections_tool_test.go` exercises every action, confirmation
rejections, proposal purity, dry-run defaults, trash/schema restoration, deferred
registration and real chat execution with fake model responses. No model API calls
are used. Follow-up engine tests cover stale relations on trash restoration and
preserving unmapped required fields during upserts, per-record unique conversion
failures, and refusing unsupported future database migrations.

### Verification

Final checks are in progress. The web agent owns collection UI flows and TypeScript
contract additions; this backend worktree contains no UI changes.
