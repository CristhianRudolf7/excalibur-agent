# 0015 — Code graph, tree-sitter backend and LSP client choices

Date: 2026-10-31. Area: codeintel (H7: Sections 10.10, 13.8, 14.12, 18.2).

## What

`internal/codeintel` implements the code and document graph, the
tree-sitter parsers, the impacted-test selection, the configurable
LSP client/manager and the `lsp` + `code_graph` deferred tools.

1. **tree-sitter via `github.com/smacker/go-tree-sitter`** (MIT),
   pinned at `v0.0.0-20240827094217-dd81d9e9be82`, behind `//go:build
   cgo`. Grammars used: go, python, javascript, typescript (+tsx),
   rust, java, c, cpp, ruby. A pure-Go fallback
   (`parse_fallback.go`: `go/ast` for Go, regex families for the rest,
   section parser for docs) serves `!cgo` builds and languages without
   a bundled grammar, so `GOOS=darwin`, `GOOS=windows` and
   `CGO_ENABLED=0` builds keep working.
2. **Dynamic edges resolve live from a persisted uses index**
   (`nameuses` table) instead of being pinned at parse time: calls,
   references, tests and cites are computed from holder name-uses
   against the project name index on every query. Incremental updates
   therefore cannot leave stale cross-file links, and indexing order
   never matters.
3. **Graph persistence is owned by codeintel** (`codegraph.db`,
   `synchronous=NORMAL` per Section 4.2) with short mutex-guarded
   transactions and an in-memory authoritative index. The H2 store
   package is not reused: this keeps the change additive and avoids
   coupling to the parallel storage wave; only the path helper
   (`Layout.CodeGraphPath`) is shared by convention.
4. **`lsp rename` reports the workspace edit; it does not apply it.**
   The agent applies edits with the file tools so history, leases and
   fingerprints (Sections 13.6–13.7, owned by the tools-core wave)
   stay consistent.
5. **Extensionless relative/dotted imports resolve to two directory
   modules** (target dir and its parent): the syntax cannot tell a
   sibling file (`./lib` → `lib.js`) from a package directory
   (`pkg.core` → `pkg/core/`). Dependents match on either;
   dependencies list both.
6. **File watching uses `github.com/fsnotify/fsnotify`** (BSD-3-Clause,
   `v1.10.1`) with recursive adds and 200 ms debounce into
   `NotifyChanged`. Process spawning for real servers goes through
   `os/exec`; tests inject pipes (`NewPipeClient`).

## Why

The architecture demands tree-sitter (deterministic, multilingual,
Section 10.10) with cgo behind build tags so cross builds keep
working (Section 3.4). `smacker/go-tree-sitter` is the only
single-module option bundling C grammars for all needed languages;
the upstream `tree-sitter/go-tree-sitter` ships no grammars, which
would multiply dependencies and pin risk. Both new dependencies are
permissive (MIT, BSD-3-Clause) with no copyleft or
commercial-use restriction, and are recorded in `dependencies.md`.

## Alternatives considered

- One grammar module per language: rejected; nine extra pins and
  nine C toolchains to audit for one deterministic parse step.
- Storing resolved call edges at index time: rejected; any reindex
  order or partial update could strand edges pointing at renamed
  symbols. Live resolution costs a map lookup per edge.
- Applying LSP rename edits inside the tool: rejected; it would
  bypass the edit history and lease serialization owned by another
  wave and risk double-apply on retry.
- No file watcher (polling or daemon-owned): rejected; Section 10.10
  names the watcher as an update trigger, and fsnotify is the
  library the architecture already plans for this (Section 3.4).
