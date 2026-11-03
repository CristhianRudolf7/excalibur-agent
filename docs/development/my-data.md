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

Import/export and daemon integration are in progress.
