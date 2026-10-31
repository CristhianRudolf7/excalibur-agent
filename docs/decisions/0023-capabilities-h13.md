# 0023 Capabilities, Extensions and Personal Tools (H13)

Date: 2026-10-31. Wave: w3-capabilities (milestone H13, Sections 13.3
remainder and 14.1-14.13).

## Context

Section 14 requires every external capability behind one contract with
simulations, an MCP client over the official Go SDK, the four-route
extensions kit with CLI and conformance suite, and the remaining
personal-assistant deferred tools. Parallel waves own the registry
core, agenda, skills, exec and the daemon gate; this wave must extend
without breaking them.

## Decisions

1. **Additive seams only.** `Operation` gains the Section 13.1
   execution fields (`TimeoutSecs`, `OutputBudget`, `Inverse`,
   `Probe`, `DryRun`) plus `Group`/`Tags`/`Keywords`; the adapter maps
   them into deferred definitions. `MapRegistry.Unregister` and
   `DeferredCatalog.UnmarkDeferred` exist for extension retirement.
   No existing signature changed; the full suite passes unmodified.
2. **MCP `_meta` key and conservative derivation.** Both `excalibur/`
   and bare `excalibur` keys are accepted; missing fields derive from
   MCP annotations with the most conservative triple
   (write_external/non_idempotent/cooperative unless readOnlyHint or
   idempotentHint says otherwise). Unix-socket transport is rejected
   explicitly until wired. Rationale: untrusted servers must never
   claim weaker effects than they have.
3. **Route-1 manifests may be tool-less.** The server `tools/list`
   is the source of truth, so MCP manifests validate with an empty
   static list; bindings fill the catalog and the conformance suite
   checks the activated tools. stdio commands resolve against the
   extension directory so extensions ship their server.
4. **Effectful extension tools must declare `dry_run`.** The store
   gate and the package tests run effectful tools without effects, so
   the suite fails them without the declaration. Validation itself
   stays permissive; behavior is gated at conformance/install time.
5. **Cancel probe budget is 3 s.** A tool on a canceled context must
   return within 3 s (any status). Rationale: fast enough for the
   suite, slow enough for real cooperative stops.
6. **YAML shapes tolerated.** Timeouts accept duration strings
   (`"90s"`) or nanoseconds; schemas accept YAML maps or inline JSON.
   The single YAML implementation lives in `pkg/extsdk`; the internal
   loader converts through the parity-pinned JSON view. `ExtensionTool`
   carries `yaml:",inline"` on both sides: without it yaml.v3 decodes
   entries to zero values (found by test).
7. **Owner wins for shared tools.** `RegisterPersonalDeferred` skips
   names an owner wave registered first; `WirePersonalCore` fills
   `settings`/`reminders` stub providers only when unset (agenda owns
   reminders). Rationale: parallel waves integrate without conflicts.
8. **User representation stays a stub (14.11).** The deferred
   agent-to-agent channel refuses permanently: representation needs
   the Section 20 disclosure filter and policy. No impersonation path
   exists by construction.
9. **`cmd/excalibur` ext subcommands are implemented; the skeleton
   test was updated.** `ext doctor` no longer exits 69; the
   not-implemented test lost that case and gained a positive H13
   test. The change is confined to the `ext` family.

## Alternatives

- Vendoring live backends (search API, vision models, TTS/STT):
  rejected; ordinary tests must never call paid APIs and Section 14.13
  explicitly tests against contract simulations.
- Cancel semantics stricter than prompt-return (e.g. asserting a
  `canceled` status): rejected; cooperative cancel may legitimately
  finish first, and the harness re-polls the final state anyway.
- Recursive file watching: implemented as one subdirectory level plus
  watch-on-create, enough for `<root>/<ext>/manifest` layouts without
  a watcher per file.

## Consequences

- `internal/capabilities` owns every H13 contract with a fake;
  `internal/tools` adapts them to deferred tools with stable prefixes.
- `extensions/example-*` prove each route end to end, including a
  compiled MCP server dialed by command in tests.
- The MCP SDK (`v1.8.0`, MIT/Apache-2.0 transition, both permissive)
  is recorded in `docs/decisions/dependencies.md`.
