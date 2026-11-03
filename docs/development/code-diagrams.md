# Repository diagrams

`code_diagram` projects the code graph from Section 10.10 into Mermaid or
Graphviz source and passes it to the shared `DiagramBackend`. It is a deferred
code tool on the default and delegate surfaces. Eval does not register it, and
the eval visualization skill retains its original bytes through `frozen_eval`.
The eval profile hash and the core tool prefix remain unchanged; the default
registry name pin changes because it contains one more deferred tool.

The tool accepts a repository or subdirectory inside the run workspace. Its
levels are `overview` (directory import relationships with weights), `module`
(files and package imports), `symbols` (definitions, calls, and implementation
edges present in the graph), and `flow` (outgoing calls from a unique entry
function, with `depth` defaulting to 3 and capped at 10). Symbol focus includes
immediate callers, callees, and implementation neighbors. Call resolution has
the conservative meaning of `code_graph`: potential static targets, including
same-named definitions, rather than a runtime trace. Ambiguous flow entries
require an exact symbol ID.

Each invocation builds a fresh snapshot through the existing parser and Go
module rewrite path, so edits and deletions appear on the next request. No LSP
server or model call is required. Indexing prioritizes a directory focus within
the requested subtree. It stops at 15 seconds, 1,000 code files, 20,000 visited
entries, or 32 MiB of source; individual files larger than 256 KiB are skipped.
The index excludes dependency/build trees, root ignore patterns, descendant
symlinks, and tests unless requested. A partial-index notice identifies the
requested subtree and priority directory. Narrow `path` or `focus` to obtain
more detail when a budget is reached.

Node selection uses weighted degree with stable ID tie-breaking, preserving the
flow entry. Diagrams default to 40 nodes, with a supported range of 2–200. An
oversized projection retains the most connected nodes and a `+N more` node,
aggregates duplicate relationships, and drops self-loops. Source ordering is
stable and directory groups become Mermaid subgraphs or Graphviz clusters.
An overflowing overview also produces up to six module details. All resulting
`ArtifactRef` records share a group and identify both source and rendered files.

Files live under `.excalibur/artifacts/<run-id>/code-diagram-<unique-id>` in the
run workspace (`standalone` when no run ID is available). The tool writes `.mmd`
or `.dot` source and requests SVG by default, or PNG/PDF when selected. Rendering
capabilities and errors come from the same backend as `diagram_render`; renderer
failures return a tool error. Text output contains counts, collapse notices, and
file paths, without the full graph. Completed artifact references survive a
later rendering failure; an invocation producing no rendered artifact removes
its source directory.

Fixture tests cover Go, Python, and TypeScript import, symbol, and call
projections without LSP. Additional tests cover weighted imports, implementation
edges, deterministic sources, node bounds, cycles, ambiguous entry functions,
subtree focus, index limits and refreshes, exclusions, cancellation, renderer
errors, path confinement, artifact metadata, and grouped detail diagrams. The
assembly test verifies shared media wiring and eval exclusion. Daemon parity
checks retain the pinned eval profile and core prefix.

Run the affected backend tests with:

```sh
systemd-run --user --scope -p MemoryMax=4G -p MemorySwapMax=0 \
  nice -n 19 go test -race -p 2 -count=1 \
  ./internal/codeintel ./internal/wireengine ./internal/skills ./internal/daemon
```

## Intent discovery

Default and delegate `exec` runs query the deferred catalog at run start.
Conversational daemon turns repeat selection for each new user message, and
chat children select from their task goal unless the delegation card specifies
an explicit tool grant. Up to three tools with matching intent metadata enter
the run's direct tool list. The catalog shares its lexical ranking with
`search_tools`; descriptions remain searchable but do not trigger activation.
Tags and keywords supply intent signals, with groups refining the rank. Diagram
metadata includes English and Spanish terms and supports plural forms.

A `tools_activated` journal entry records the selected names and the single
matching skill hint before the first model call. The skill index ranks hints
among skills declaring a selected tool; the hint names `load_skill` explicitly.
Selection does not change core catalog membership or its hash. Eval skips
activation entirely and retains its frozen profile and prompt behavior.

`TestIntentToolSelection`, `TestIntentActivationJournal`, and
`TestIntentSuggestionsUseMetadataAndBoundSelection` cover matching, unrelated
requests, deterministic bounds, Unicode/plurals, skill resolution, journal
records, and the unchanged core prefix and eval behavior.

`TestExecIntentToolsReachFirstModelRequest` exercises default, delegate, and eval
runs with a simulated provider. It checks English and Spanish requests, the
first provider tool list, the explicit skill hint, and journal ordering.
`TestChatIntentActivationPerNewMessage` verifies that a later diagram request
activates tools and a subsequent unrelated message does not retain them.
`TestChatActivatedRendererPublishesArtifacts` calls the real SVG renderer
directly through the chat engine and verifies both artifact events.
`TestChatChildIntentRespectsExplicitToolGrant` covers delegated discovery and
ensures a narrowed delegation card keeps its tool grant. Catalog pins require
no update because activation changes neither registry names nor the core prefix;
the eval profile hash remains unchanged.
