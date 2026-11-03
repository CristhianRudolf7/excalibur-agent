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
daemon blobs. Native Office extraction supplies cell values; a style pass converts
Excel serial dates, including the 1904 epoch. Source row numbers survive leading
blank/title rows and sparse sheets. Schema inference and exact, accent-insensitive,
then unambiguous fuzzy mapping are overridable. Automatic trimming, decimal commas,
select casing, and date changes are logged. Ambiguous slash dates require ISO
input. Conflicts identify row, column, source value, problem, and a suggested fix.
Valid rows commit together; invalid rows are skipped. Full normalized row hashes
prevent exact duplicates; an upsert key updates a single matching record and reports
multiple matches as conflicts. Dry runs execute the same transaction then roll back.
Without a collection or `create_schema:true`, imports only return a proposal.

Exports produce daemon CSV/XLSX blobs and preserve typed values on round trip.
XLSX uses native Office creation and extraction limits (20,000 cells); warnings or
limits fail explicitly rather than silently dropping rows. CSV is limited to
100,000 rows and all imports to 64 MiB/128 columns. Untrusted formulas are never
executed; XLSX exports store user strings as literal cells.

`internal/collections/importer_test.go` covers Spanish decimal-comma CSV,
Latin-1, tabs, title/header detection, inference, accent/fuzzy/explicit mapping,
mixed date conflicts, duplicate/upsert behavior, relation conflicts, dry runs,
automatic fix logs, sheet selection, rows beyond preview limits, Excel serial dates,
and CSV/XLSX typed round trips.

Daemon RPC and tool integration are in progress.
