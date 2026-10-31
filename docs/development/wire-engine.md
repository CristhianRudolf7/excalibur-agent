# Wire engine (cross-milestone execution path)

English only. The production composition root for the execution path
(wave 4): it replaces the fakes, seams, stubs and "deferred to wiring"
notes left by the milestone waves with the real implementations,
faithful to the architecture document (referenced by section number
only). Fakes stay only in unit tests.

## Contract

Package `internal/wireengine` owns the embedded wiring; the engine
package owns the loop and the orchestrator owns the authoritative row.

- Engine Runner used by the orchestrator (engine <-> orchestrator):
  `internal/engine.EngineRunner` implements `orchestrator.Runner`
  (RequestInterrupt with toolOnly vs full per 15.8, ApplyDirective to
  the next safe point per 7.3/15.5, Snapshot for status/watchdogs/acks
  per 7.2/15.6/15.7). Interrupts cancel the current step context; a
  full interrupt exits the loop gracefully without failing so the
  orchestrator owns the resulting pause or cancel, while a tool-only
  interrupt injects a note and the run decides how to continue.
- Journal replay on resume (17.2, 17.3): `ResumeReplays` lists the
  journal in sequence order, runs `engine.Recover` (no repeated model
  call, no repeated non-idempotent effect), `ApplyRecovery` and
  `ReplayPending` with recorded idempotency keys. `AdoptIntoOrchestrator`
  mirrors the decision into the orchestrator row through the 7.2
  diagram (recovering first when needed).
- Workspace leases, worktrees and shared edit history inside the edit
  tools (13.6, 13.7, 16.2, 16.4): `WorkspaceLeases` adapts the
  workspace `LeaseManager` to `tools.LeaseManager`; `SharedHistory`
  records every edit in both the tools history and the workspace
  history under the same id so `file_history` and `revert_edit` observe
  the same edits; engine `LeaseHeartbeat`/`LeaseRelease` hooks renew
  leases while running or waiting on tools/subagents and release on
  pause, block, user wait and termination.
- LSP diagnostics after edits and code-graph impacted-test selection
  in check selection (10.10, 13.8, 18.2): `FileDeps.Diagnostics` is
  wired to a `DiagnosticProvider` (production: the codeintel Manager,
  which satisfies the interface); edit outcomes append the diagnostics
  of the touched files. `ImpactedChecks` expands the changed set with
  `Graph.ImpactedTests` before the ladder `SelectDuringWork`, so the
  checks covering impacted tests run during the work.
- Deferred tools reachable through search_tools/call_tool without
  breaking the cache prefix (13.3): `BuildEmbedded` registers built-ins
  first, snapshots the core prefix hash, then registers every deferred
  harness family (codeintel, workspace, documents/media, web
  source_search, personal, packages) via `RegisterDeferred`/`MarkDeferred`
  and wires the real `search_tools`/`call_tool` providers. The prefix
  hash is asserted unchanged after wiring.
- Skills tools registered in the catalog (12.4): `WireSkills` fills
  the `search_skills`/`load_skill` core stub providers from a real
  `skills.Index` without touching the prefix; `load_skill` resolves the
  skill's declared deferred tools up front via `ResolveSkillTools`.
- Vault secret resolution in providers and tool processes plus
  redaction in all outputs (20.1): `VaultResolver` adapts the vault to
  the provider registry hook (vault priority, then env, then config;
  eval never passes a vault); `ExecSecretResolver` resolves `secret://`
  env entries inside the tool process with tool/run/project scoping;
  `VaultRedactor` (value plus base64/URL variants plus known patterns)
  is installed as the executor `Redact` hook so every output is
  redacted before truncation, the journal, context, telemetry, ATIF
  and blobs. Locked vaults deny with a clear unlock error.
- Eval profile using all of it in embedded mode: `BuildEmbedded`
  assembles the registry, deferred catalog, executor, leases, shared
  history, diagnostics, skills, codeintel and vault redaction in one
  place. `excalibur exec` with `ExecOptions.Provider` drives the real
  engine loop (`internal/cli/exec_engine.go`) with core-only tool
  definitions in the model prefix and records the real trajectory in
  ATIF.

## Backend tests

- `internal/engine`: runner contract is covered from `wireengine`
  (attach, directive at safe point with ack, snapshot, full interrupt
  stops without failing); existing 7.x/17.x suites still pass with the
  per-step cancellable context and graceful cancel paths.
- `internal/wireengine`:
  - `runner_test.go`: Runner implements `orchestrator.Runner`;
    directive applied at next safe point and acked; full interrupt
    stops the loop without failing.
  - `resume_test.go`: crash between a read-only intent and its result
    replays exactly once with the recorded key, never calls the model
    again, reconciles to queued; unknown orchestrator runs fail
    adoption; spawned runs adopt resume through recovering to queued.
  - `leases_history_test.go`: write/edit share one history across the
    tools and workspace stores; `file_history` lists it; `revert_edit`
    restores the preimage; leases serialize and release on pause;
    edit outcomes carry LSP diagnostics.
  - `catalog_test.go`: `search_tools` finds `code_graph`;
    `call_tool` runs a deferred tool with validated args; the prefix
    hash never moves; `code_graph`/`lsp`/workspace/media tools are
    deferred, never core; `search_skills`/`load_skill` work against a
    real index.
  - `vault_test.go`: vault-first provider resolution with env fallback;
    `exec` resolves `secret://` env inside the process; outputs carry
    `‹secret:name›` markers and never values; locked vaults deny.
  - `checks_test.go`: editing `lib.py` selects the `test_mid.py` check
    through the graph while the plain impact-area selection selects
    nothing.
- `internal/cli/exec_engine_test.go`: end-to-end through `excalibur
  exec` with a scripted fake provider — write then done (file on disk,
  2 model calls, stable tool prefix order, valid ATIF, Appendix F
  artifacts), and write/search/call_tool/deferred history (4 model
  calls, deferred result in ATIF).

Nothing in this document describes as implemented what is only
planned: browser/device/app live backends stay on contract fakes per
14.13; voice/reception rows and daemon-routed delegation stay with
their owners.
