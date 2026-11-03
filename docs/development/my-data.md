# My data

## Backend

The personal collections engine uses `collections.db` in the daemon data directory,
independently of the harness database. Collection and field metadata, ten retained
schema snapshots (including records), and one typed table per collection persist
through restarts. Renaming a field changes metadata only. Schema alterations are
atomic and support conversion dry runs. Invalid conversions block the entire edit.
Required field additions to nonempty collections are rejected; use optional
fields or migrate records into a new schema. Values are never invented. Collection deletion retains data for thirty days;
expired trash is purged at database open. Record and collection deletion report
incoming relation references instead of silently cascading.

Record values use field IDs. CRUD accepts decimal commas, Spanish booleans, ISO
dates and Spanish day/month/year dates. Queries support field filters, text search,
validated sort columns and opaque cursors bound to their query and schema version.
Pagination uses a deterministic ID tie breaker; concurrent inserts/deletions can
shift offset pages. Summary cards use SQL aggregates, including weighted averages
that exclude missing values and return null for zero total weight. SQL min/max
also support date and text fields.

Engine coverage: `internal/collections/engine_test.go` exercises all field types,
required/unique/range checks, persistence, conversion failure and rollback, schema
snapshots, trash expiry and restoration, incoming/outgoing relations, filtering,
sorting, pagination, and SQL summary cards.

Imports read CSV (comma, semicolon or tab, UTF-8/BOM or Latin-1) and XLSX from
daemon blobs. The existing bounded OOXML package reader supplies the complete
cell grid; a style pass converts Excel serial dates, including the 1904 epoch.
Source row numbers survive leading blank/title rows and sparse sheets. Schema inference and exact, accent-insensitive,
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
failures, date/text extrema, current record counts in trash, and refusing
unsupported future database migrations.

### Verification

Completed checks:

- `gofmt -l .` and `scripts/check-no-bare-go.sh` are clean.
- Touched-package vet/staticcheck and whole-repository Linux/Darwin/Windows builds
  pass. Whole-repository staticcheck reports five existing `ST1005` style warnings
  in `internal/googleapi/tools.go` at lines 193, 216 and 218. That package is outside
  this round's permitted scope and has not been changed.
- Race tests pass for `internal/collections`, `internal/api`, `internal/gateway`,
  `internal/daemon/...`, and `internal/tools/...`. This includes all RPC/WS/tool/chat
  tests, contract parity, and pinned embedded/eval hashes. Both full daemon race
  runs passed; the latest engine edge cases and daemon integration checks also pass.
- The full tools race suite passes after stabilizing its existing compiler-exit
  progress test. The command now waits for an explicit release file, replacing a
  scheduling-sensitive 400 ms sleep that failed twice in full-suite race runs.
  Five repetitions of the synchronized test and the full-suite retry pass. Runtime
  terminal code and tool definitions are unchanged.
- Web `npx tsc --noEmit -p .` and `npx tsc -b --pretty false` pass from `apps/web`.
- All web Vitest tests pass: 121 files, 646 tests (`--maxWorkers 2`).
- Existing Playwright chat/agent/settings smoke tests pass: 32 tests using owned
  port 5356 (`EXCALIBUR_WEB_TEST_PORT=5356`, two workers).
- `git diff --name-only 766dc51` contains no forbidden paths.

The web agent owns collection UI flows and TypeScript contract additions; this
backend worktree contains no UI changes. The existing Playwright smoke suites
verify baseline chat/agent/settings flows; collection UI e2e belongs to that branch. The documentation file is shared: retain
this Backend section when merging the web agent's section. No changes to the
embedded/eval catalog, provider wiring, robot code, integrations or web contracts
are required from this branch.

# Personal collections

## Web

The section is named **My data** in English and **Mis datos** in Spanish.
It uses the existing semantic theme tokens, shared controls, and sidebar groups.
The sidebar entry follows Extensions and precedes Settings. The expanded sidebar
lists collection names, icons and record counts, with collapsible children and an
agent creation shortcut when empty. The compact rail keeps the same ordering.

### Routes and interactions

| Route                        | Behavior                                                                                      |
| ---------------------------- | --------------------------------------------------------------------------------------------- |
| `/data`                      | Collection cards, record counts, update dates, creation and spreadsheet import shortcuts      |
| `/data/$collection`          | Search, typed filters, column selection, sorting, cursor pagination, summary cards and charts |
| `/data/$collection/new`      | Schema-generated record creation                                                              |
| `/data/$collection/$id`      | Formatted record detail, file preview, relation links and incoming related records            |
| `/data/$collection/$id/edit` | Schema-generated editing with preserved values                                                |

Deletion has no route. Its confirmation dialog shows the display value and warns
when related records reference the selected record. Pending and failed requests
remain visible; failures offer retries or retain the user's form values. Saves
and deletes show transient, accessible status notices. Exports download CSV or
XLSX blobs using the current structured filters.

All fourteen shared field types have generated controls. Numeric inputs accept
localized decimal commas; integer and range constraints are checked before RPC.
Dates use native date controls; datetimes convert local input to ISO instants.
Date-only display stays on the selected calendar day, while datetime display
uses the principal's configured timezone. Selects and multi-select chips use the
schema options. Relation controls search the target collection and retain a
selected record outside the current search results. Files upload original bytes
through the existing SHA-256 `/blobs` flow. Saving is disabled during uploads.
Structured `-32602` `data.errors` responses appear beside their fields.

TanStack Router guards navigation and browser unload while a form is dirty.
The discard dialog allows users to stay or leave. Form contents remain stable
when a background event refreshes collection metadata.

### Agent handoff and API boundary

The new-collection action opens an editable chat draft asking the agent to
propose the structure and wait for confirmation. Spreadsheet import uploads the
original Excel/CSV blob and opens a draft with the file attached, its digest,
and instructions to propose the schema, dry-run imports, report normalization
and ask about unresolved conflicts. The UI never creates a collection or commits
an import implicitly. Schema changes and whole-collection deletion remain agent
operations with the daemon's confirmation policy.

The original spreadsheet is attached directly rather than sent through document
text extraction. The composer sends its digest with the conversation attachment
list; the draft also names the blob so the daemon-only `my_data` tool can import
it. The attachment is addressed to the selected agent and can be removed before
sending. No additional model endpoint or credentials are used.

The additive client types and all fourteen RPC method shapes live in
`apps/web/src/lib/rpc/contract.ts`. Import mapping is an array of
`{ column, field: string | null }`, matching the backend's `ImportMapping`.
The existing `clientMethods` inventory is unchanged, following the other
feature-specific additive interfaces; the daemon worktree owns the shared JSON
catalog and handlers. Merge both branches before using this UI with a daemon.

Queries share the `mydata` prefix. A single shell-level subscription uses the
existing `/ws` reconnect/replay transport for `collections.changed`. Events and
reconnects invalidate collection lists, schemas, rows, related records, picker
results and summary cards. Successful saves populate the detail cache from the
canonical response, then invalidate lists and summaries. Pagination keeps the
previous page visible until the requested cursor resolves. Pages request at
most 25 rows; relation search requests at most 30.

### Verification

Unit coverage in `features/mydata/` exercises every generated field type,
localized conversion, required/integer/range checks, RPC field errors, filter
operators, sorting and cursor state, explicit deletion confirmation, collection
navigation, signed summary charts, and live invalidation. The sidebar rail suite covers entry ordering.

`e2e/mydata.spec.ts` runs against a stateful mocked JSON-RPC daemon and blob
routes. It covers the complete overview/list/create/view/edit/delete journey,
file uploads, filters, sorting, column selection, both export formats, unsaved
changes, agent handoffs and live updates. Mobile checks visit every route at
390 px in English, both themes and reduced motion, asserting no page overflow.
The existing Extensions navigation suite covers the shared sidebar layouts.

Set `EXCALIBUR_MYDATA_SCREENSHOT_DIR` to capture screenshots outside Playwright's
ignored test output directory. Screenshots and command logs for this round are
stored outside the repository in the round's
`status/next/mydata-web-notes/` directory. The baseline Extensions screenshot
comes from an isolated archive of commit `766dc51`. Final verification results
and merge notes are recorded in `status/next/mydata-web.md`.

### Verified results (2026-11-03)

| Check                                   | Result                                                                      |
| --------------------------------------- | --------------------------------------------------------------------------- |
| Full Vitest suite                       | 123 files, 672 tests passed                                                 |
| Latest feature unit checks              | 29 tests passed, including three additional signed-chart cases              |
| Sidebar rail unit checks                | 8 tests passed                                                              |
| Playwright feature and Extensions flows | 11 tests passed; all 6 feature flows passed again after visual fixes        |
| Portable screenshot helper              | Fresh full CRUD flow passed with external screenshot destination            |
| TypeScript                              | `tsc -b` and `tsc --noEmit -p .` passed                                     |
| Web production build                    | Passed                                                                      |
| Prettier and oxlint                     | Passed; existing unrelated lint and bundle-size warnings remain             |
| Go formatting                           | No unformatted files                                                        |
| Go builds                               | Linux, Darwin and Windows passed with `-p 2`                                |
| Go vet                                  | Daemon and tools packages passed                                            |
| Daemon race suite                       | Passed, including embedded/eval tool-surface and prefix pins                |
| Tools race suite                        | Passed in an isolated run, including cancellation and deferred-prefix tests |
| Forbidden-path audit                    | No changes in restricted paths                                              |

The initial tools race run failed the existing `TestPipelineCancelModes` check;
a subsequent run was terminated with `SIGTERM` before producing results. The
isolated complete tools race run passed (153 seconds, exit 0). No Go source or
tests were changed. Logs retain these earlier outcomes for the integration audit.

The backend branch supplies the daemon handlers, agent tool, shared method/event
catalogs and persistence. This branch verifies the browser boundary with mocks;
a live daemon integration check remains a merge responsibility. When combining
the documentation, retain both the backend branch's **Backend** section and this
**Web** section.
