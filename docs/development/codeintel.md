# H7 codeintel half — code graph, LSP and deferred code tools

Contract and backend tests of the H7 codeintel half (wave w2-codeintel):
the code and document graph of Section 10.10 with tree-sitter,
impacted-test selection used by Section 18.2, the configurable LSP
client of Section 13.8, and the `lsp` + `code_graph` deferred tools
registered through the Section 14.12 interface.

Architecture reference: Sections 10.10, 13.8, 14.12 and 18.2.
Appendix B values used verbatim (LSP idle shutdown 10 min; tool
timeout 60 s; output budget 40 KiB).

## Contract

- **Graph** (`graph.go`): nodes (file, module, symbol with
  function/method/type/test sub-kinds, document, section,
  requirement) with deterministic IDs (`file:`, `sym:`, `sec:`,
  `mod:`, `req:`), static edges (defines, imports, implements,
  requirement) in `codegraph.db` (own SQLite file at the
  `Layout.CodeGraphPath` location, `synchronous=NORMAL`, rebuildable
  per Section 4.2) plus dynamic edges (calls, references, tests,
  cites) resolved live from a persisted `nameuses` index against the
  project name index. `UpsertFile` replaces one file wholesale in a
  single short transaction; `RemoveFile` drops it; `OpenGraph`
  reloads everything; `AttachRequirement`/`Implementers` link and
  read requirement edges for Section 8.3.
- **Parsers** (`parse.go`, `parse_fallback.go`,
  `treesitter_cgo.go`, `treesitter_stub.go`): `ParseFile` detects
  the language by extension (go, python, javascript, typescript,
  java, c, cpp, rust, ruby, php, docs), parses with the preferred
  parser and builds the `FileUpdate`. On cgo builds tree-sitter
  grammars parse definitions and call sites; otherwise (and for
  languages without a bundled grammar) the pure-Go fallback parses:
  exact `go/ast` for Go, regex families for the rest, `#` sections
  plus backtick mentions for docs. `TreeSitterAvailable` reports the
  backend; `PreferredParser` agreement is tested on a shared fixture.
  Go first-party imports resolve against `go.mod` to directory
  modules; extensionless relative/dotted imports map to both the
  target and parent directory modules.
- **Indexer** (`index.go`): `IndexAll` scans the project (skips
  `.git`/`node_modules`/`target`/binaries/oversize files and a
  minimal `.gitignore`), upserts only hash-changed files and removes
  deleted ones; `NotifyChanged` reindexes listed paths (post-edit
  hook and watcher target); `Watch` follows the tree with fsnotify,
  debounces 200 ms and reindexes; every long-lived goroutine uses
  `safego.Go`.
- **Impacted tests** (`impact.go`, Section 18.2): `ImpactedTests`
  selects the tests covering a change — the changed test itself,
  transitive callers (calls/references closure), `tests`-edge
  holders, and tests importing the changed module; unknown paths
  fall back to every test file with `Conservative`. `TestFiles`
  lists every test file for the before-finalize full run.
- **LSP** (`lsp.go`, Section 13.8): `ServerConfig` (language,
  command, args, env, root dir) with `Validate`; `Client` speaks
  JSON-RPC 2.0 with Content-Length framing over stdio (pipes
  injectable for tests), with initialize/handshake, didOpen tracking
  with content-drift reopen, definition (Location+LocationLink),
  references, document/workspace symbols, rename (changes and
  documentChanges normalized to project-relative paths), cached
  `publishDiagnostics` exposed as `Diagnostics(paths)` implementing
  the `tools.DiagnosticProvider` shape; `Manager` lazily starts one
  shared instance per project+language (single-flight), reaps after
  `IdleTTL` (default 10 min, Appendix B) and stops everything on
  `Close`. Rename reports the edit; the agent applies it with the
  file tools so history and leases stay consistent.
- **Deferred tools** (`tools.go`, Sections 13.3, 14.12): `CodeGraphTool`
  (`callers`, `callees`, `dependencies`, `dependents`, `symbols`,
  `impacted_tests`, `implementers`, `stats`; read_only/idempotent/
  immediate) and `LSPTool` (`definition`, `references`, `symbols`,
  `workspace_symbols` across all configured servers when no language
  is given, `diagnostics`, `rename`; write_local/keyed/cooperative).
  Both validate names (snake_case, ≤64 chars), carry discovery hints
  (description, group, tags, keywords) for `search_tools`, register
  through the shared `tools.Registry` with duplicate rejection, and
  publish `Manifest()` (`excalibur-codeintel`, `excalibur-ext/1`,
  kind `builtin`, lazy) validated by `Manifest.Validate`.

## Backend tests

- `graph_test.go`: upsert/resolve round-trip, incremental update
  drops stale edges, remove+reload persistence, requirement
  attach/implementers, stats and doc cites.
- `parse_test.go`: language detection, test-path conventions, exact
  Go parse, fallback families (symbols/imports/calls),
  doc sections/mentions, unknown extensions, fallback/tree-sitter
  agreement.
- `index_test.go`: full scan (gitignore/binary skips), no-op
  second scan, edit/delete incremental updates, go.mod first-party
  rewrite with call-through impacted selection.
- `impact_test.go`: the Section 10.11 row — edits select exactly the
  covering tests, module-import selection, conservative fallback for
  unknown paths, `TestFiles` listing.
- `lsp_test.go`: fake scripted server over pipes — full round-trip
  (definition, references, both symbol forms, workspace symbols,
  rename with both edit forms, diagnostics push, unknown-method
  error), manager lazy sharing (single start under concurrency),
  idle reap + restart, config validation, URI helpers, frame errors.
- `tools_test.go`: the LSP/graph rows of Section 13.10 — catalog
  registration (names sorted, definitions validate, duplicates
  rejected), manifest contract, every `code_graph` action end to
  end plus input-error classification, every `lsp` action against
  the fake server, resource keys, provider conformance.

Not implemented here (other waves): `search_tools`/`call_tool`
(H7 search half), edit-history three-way merge (13.7), leases and
worktrees (16.x), task-requirement linkage population (the
`AttachRequirement` API is ready for the tasks wave), daemon
wiring of `Manager.Diagnostics` into `FileDeps`.
