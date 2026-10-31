# Deferred tools, harness documents/media/web, agent-created tools

Contract and backend tests of the w2-deferred-tools wave (milestone H7):
deferred mechanism and `search_tools`/`call_tool` (13.3), harness-owned
deferred tools for documents, media and web (13.3, 14.3, 14.4, 14.7),
agent-created tools (13.9), capability contract fakes (14.1) and the
`tool-retrieval` and `terminal-vs-tools` suite scaffolding (23.5).

Architecture reference: Sections 13.3, 13.9, 13.10 (remaining rows),
14.1, 14.3, 14.4, 14.7, 23.2 (benchmark-source blocklist) and 23.5.
Appendix B values used verbatim. See `docs/decisions/0016-deferred-tools-harness-fakes.md` for the
wave decisions.

## Contract

- **Capabilities** (`internal/capabilities/capability.go`): `Manifest`
  (identity, version, operations with schemas, effect/idempotency,
  platforms/profiles, resources, concurrency), `Operation`, `TaskState`
  (`working`, `input_required`, `completed`, `failed`, `cancelled`),
  `ErrorClass` with `ClassifyError`, `TaskRecord` with idempotency key
  and poll interval, `DefaultPollInterval` 2 s, shared
  `BlockedBenchmarkSource` (23.2), `MCPToolName` (`mcp__server__tool`,
  hash-abbreviated over 64 chars). `internal/capabilities/fake`
  provides scripted outcomes, single-effect idempotency counting and
  durable progressions with restart-safe task ids and cooperative
  cancel.
- **Deferred mechanism** (`internal/tools/deferred.go`):
  `DeferredCatalog` separates core prefix from deferred entries;
  `PrefixHash` is sha256 over sorted core definitions and never changes
  when deferred tools register or skills resolve. `Search` is
  deterministic BM25 over name, description, tags, keywords and group
  (top 5, zero-score tails dropped, name tie-break). `SearchToolsTool`
  (`read_only`/`pure`/`immediate`/`fast`) returns 1..5 schemas as JSON;
  `CallToolTool` (`exec`/`keyed`/`cooperative`, not fast) rejects core
  names and self-calls and delegates to `Executor.Execute` (13.4
  validation). `WireSearchCall` fills the core stub providers without
  changing the prefix. `ResolveSkillTools` pre-resolves skill-declared
  tools.
- **Capability adapter** (`internal/tools/capability_adapter.go`):
  `ToolsFromCapability` converts each operation into a deferred tool
  through the same registry. Polling follows `working` to terminal,
  surfaces `input_required` need text for the engine `ask_user`,
  maps `failed` by error class, `cancelled` to canceled, and forwards
  idempotency keys so repeats never repeat effects.
- **Web** (`internal/tools/web.go`): `web_search` (core, fast) with
  `query/freshness/domains/lang/k`, `web_read` (core) with `url`, and
  `source_search` (deferred) with `source/query/k` over the 17 known
  sources. `MemorySearchBackend` is the deterministic simulation.
  Benchmark URLs are denied and logged; benchmark-naming queries in
  `source_search` are denied and logged; every allowed read records a
  `SourceEvent` for the Sources listing.
- **Documents/media** (`internal/tools/documents.go`):
  `document_read` extracts text with page/section/paragraph anchors for
  txt/md/json/html/csv/log; `pdf_ops` merges, splits, extracts,
  rotates, annotates/fills/redacts through atomic writes;
  `file_convert` runs only the manifest pairs
  (`md->html`, `txt->md`, `md->txt`, `json->md`, `csv->json`,
  `html->md`); `diagram_render` renders mermaid/plantuml/graphviz/svg/
  drawio to SVG with dialect-specific errors (png/pdf need a
  rasterizer); `chart_render` renders bar/line to SVG;
  `vision_extract` honors the 14.4 contract (`text`/`layout`/`describe`
  per page with model-used and provenance) via the deterministic fake.
- **Agent-created tools** (`internal/tools/packages.go`): `PackageManager`
  validates the manifest, runs every test through the 13.4 pipeline
  (effectful tools in no-effects dry-run mode), activates only on full
  pass, and tracks version, status, calls and last use.
  `tool_package` (`create`/`stats`/`list`) creates, tests and activates
  in one call.
- **Eval suites** (`internal/eval/deferred_suites.go`):
  `tool-retrieval` pins 8 deferred queries with hash, scores
  precision@1/precision@5/MRR with the same BM25 the tools serve;
  `terminal-vs-tools` pins 5 tasks with hash and aggregates
  success/tokens/time/errors per arm with average deltas.

## Backend tests

- `internal/capabilities`: manifest validation, terminal states, error
  classification, shared blocklist, deterministic MCP abbreviation;
  fake: single-effect idempotency, working/input_required/completed
  progression, cooperative cancel with re-poll.
- `internal/tools/deferred_test.go`: all 99 enumerated catalog names
  validate (snake_case, <=64, unique); search precision over the pinned
  queries; `search_tools` returns 1..5 deferred-only schemas;
  `call_tool` rejects core/unknown/self and surfaces schema parts;
  prefix hash unchanged by deferred registration and skill resolution;
  MCP abbreviation deterministic and valid.
- `internal/tools/web_test.go`: search registers sources, domain
  filter, benchmark reads denied and logged (3 markers), allowed read
  content and event, source allowlist/blocked-query handling.
- `internal/tools/documents_test.go`: anchors and sections, page
  selection, unsupported format; pdf merge preserves content, extract,
  bad angle, annotate; convert pairs list, md->html headings,
  unsupported pair error; diagram mermaid/graphviz SVG, concrete syntax
  errors, png honesty; chart SVG and bad-kind error; vision
  text/layout/model reporting and bad mode.
- `internal/tools/packages_test.go`: create+activate searchable via
  `search_tools`, invoke through the pipeline, usage stats; bad
  manifest and failing tests never activate; effectful dry-run path;
  `tool_package` create/stats/list round trip.
- `internal/tools/capability_adapter_test.go`: working/input_required
  surfacing, input-failure mapping, cancel mapping, idempotent resend
  single effect.
- `internal/eval/deferred_suites_test.go`: deterministic prepare/hash,
  precision@5 equals 1 on the pinned corpus, terminal comparison
  deltas negative (tools arm cheaper/faster), registry fixtures
  present.

## Deferred (other H7 waves)

LSP (13.8), code graph (10.10), file history three-way merge and
`revert_edit`/`workspace_snapshot` (13.7), leases/worktrees/integration
queue (16), browser/device/apps/media beyond this wave's harness set,
MCP client wiring and `extsdk` paths (14.12, H13). Each lands with its
wave; the seams above (registry, pipeline, catalog, suites) are the
integration points.
