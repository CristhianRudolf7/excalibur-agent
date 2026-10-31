# 0016 Deferred Tools, Harness Media/Web, and Capability Fakes

Date: 2026-10-31. Wave: w2-deferred-tools (milestone H7).

## Context

Sections 13.3 (deferred tools, search_tools/call_tool), 13.9
(agent-created tools), 14.1 (capability contract), 14.3/14.4/14.7
(search, vision, documents), 23.2 (benchmark-source blocklist) and 23.5
(tool-retrieval, terminal-vs-tools).

## Decisions

1. **Lexical BM25 now, vectors later.** Section 13.3 says BM25 +
   vectors. The harness ships the deterministic lexical half in
   `internal/tools/deferred.go`; vector re-ranking arrives with the
   memory embeddings (10.8) and only reorders within the same
   deterministic tie-break. Rationale: no embedding backend exists in
   this wave, and retrieval must stay deterministic for the 23.8 setup
   check.

2. **SVG is the harness render target.** `diagram_render` renders all
   five dialects to SVG with concrete syntax errors. PNG/PDF output
   returns an honest input error naming the missing rasterizer instead
   of fake bytes. `chart_render` renders bar/line to SVG. Rationale:
   no external renderer is vendored; Section 14.7 forbids supposing
   universal compatibility, and effective formats are verified by
   tests.

3. **File conversions are allowlisted.** `file_convert` supports only
   `md->html, txt->md, md->txt, json->md, csv->json, html->md`. Any
   other pair is an input error listing the supported pairs.
   Rationale: Section 14.7 requires the manifest pair list and tests
   without supposing compatibility.

4. **Catalog count: 99 enumerated vs 98 stated.** Sections 13.2 and
   13.3 tables enumerate 99 distinct names; the 13.10 verification row
   says 98. Validation covers the full enumerated set and the test
   asserts 99 with this note. Alternative considered: dropping one
   tool to hit 98, rejected because every table row is a required
   responsibility.

5. **Shared benchmark blocklist.** `capabilities.BlockedBenchmarkSource`
   is the single implementation; `eval.BlockedBenchmarkSource`
   delegates to it and the web tools enforce it. Rationale: Sections
   23.2 and 13.3 must never drift apart.

6. **vision_extract is a contract + deterministic fake.**
   `MemoryVisionBackend` returns placeholder text/boxes/descriptions
   with input hash and page provenance. Real model wiring stays in the
   vision role (19.1, 23.5 vision suite). Rationale: no vision model in
   this wave; the contract, provenance and model-used reporting are
   what the harness owns.

7. **tool_package tests for effectful tools run in no-effects mode.**
   Read-only packages execute their implementation; effectful ones
   check the declared dry-run text instead of executing. Rationale:
   Section 13.9 requires tests through the 13.4 pipeline in no-effects
   mode when the tool has effects.

## Alternatives

- Vendoring a PDF/diagram rasterizer: rejected, adds cgo and license
  surface with no contract benefit in this wave.
- Live web search: rejected, ordinary tests must never call paid APIs;
  the memory backend is the Section 14.1 simulation.

## Consequences

- `internal/capabilities` owns the durable-task lifecycle and the
  blocklist; `internal/tools` adapts capabilities to deferred tools
  with prefix-hash stability.
- `internal/eval` gains deterministic tool-retrieval and
  terminal-vs-tools scaffolding with hashed fixtures.
