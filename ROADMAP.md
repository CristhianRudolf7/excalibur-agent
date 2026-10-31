# Roadmap

Milestones of Appendix E. Status: H0–H13 implemented, each with its
verification-table tests green (see `docs/development/coverage.md`
for one row per architecture section with the test names).
Phase A built the programming harness first (measurable in
Terminal-Bench from H4 on); Phase B built the personal assistant on
the same core. Ablation comparisons (each mechanism on/off) stay
available through the eval suites in `internal/eval` before any
mechanism stays on in `eval`.

- [x] **H0. Repository skeleton and shared contracts (wave 0)** —
  module, package tree, shared types, API contracts, core interfaces
  with fakes, CLI skeleton, verification harness.
- [x] **H1. Providers of language models** — `internal/providers`
  adapters (OpenAI, Anthropic, Gemini, DeepSeek, OpenRouter, Ollama,
  generic OpenAI-compatible) on the wave-0 contract; contract tests
  against simulated servers (`TestChat*`, `TestResponses*`,
  `TestStream*` per adapter; never a paid API).
- [x] **H2. Base storage** — identifiers, layered config loading, Linux
  `internal/platform` (plus `!linux` stubs), SQLite porter, blobs,
  migrations (`TestConcurrentWritersNoBusy`, `TestGroupCommit`,
  `TestShortCodesNeverReused`, `TestCorruptRestore`,
  `TestWALAlertAndCheckpoints`, network-FS `DELETE` fallback).
- [x] **H3. Durable engine and terminal** — journal, agent loop, gateway
  subset, context compiler basics, core file/terminal tools, extension
  registry (`TestCrash*` suite, `TestEndToEndSimpleTask`,
  `TestEndToEndInteractiveProgram`).
- [x] **H4. Non-interactive mode and benchmarks** — `exec`, eval profile,
  events, metrics, ATIF, Harbor adapter, bench launchers
  (`TestExecExitTable`, `TestProfileContract`,
  `TestEvalSurfaceMatchesProfileTable`, `TestSmokeScaffold`,
  `scripts/bench`).
- [x] **H5. Verifiable work** — tasks/plans/requirements/criteria/
  evidence, completion gate, run states, loop detector, limits
  (`TestGate*`, `TestCanTransition`, `TestTransitionsFromEveryNonTerminalState`,
  `TestR1IdenticalRepeat`–`TestR6EditMismatch`, `TestClosingThenExhausted`).
- [x] **H6. Context and cache** — layers, deltas, masking, compaction,
  cache adapters, token counting (`TestCompileDeterministic`,
  `TestDeltasKeepStablePrefix`, `TestCompactionThreshold`,
  `TestEstimateCalibratesWithinMargin`, cache suites).
- [x] **H7. Programming tools and edit concurrency** — deferred tools,
  `search_tools`, history, snapshots, LSP, code graph, leases, worktrees
  (`TestToolRetrievalScaffold`, `TestTerminalVsToolsScaffold`,
  `TestGraphIncrementalNoStaleEdges`, `TestConcurrentFragmentEditsSerializes`).
- [x] **H8. Subagents, programming skills, delegation** — orchestrator,
  profiles, mailboxes, directives, skill index, `task` family, MCP
  server, ACP (`TestSuiteSteer`, `TestSuiteControl`,
  `TestSubagentAuthority`, `TestTaskRoundTrip`, `TestWalkthrough*` in
  embedded mode).
- [x] **H9. Daemon, master, conversation** — gate, intake, fast lane,
  projects, scopes, delegation through the daemon (`TestAvailSuite`,
  `TestFastLaneTable`, `TestTurnsParallelAcrossConversations`,
  `TestScopeIsolationFourPaths`, walkthroughs 26.11/26.1–26.6 through
  the gateway).
- [x] **H10. Recovery, continuous operation, vault** — boot, safe mode,
  agenda, retention, install/update, secrets vault
  (`TestCrashPointsTable`, `TestBootOrdersAndOutcomes`,
  `TestAgendaTickFires`, `TestVault_AllTypes_NoCleartext`,
  `TestUpdate*`, walkthrough 26.7).
- [x] **H11. Memory and documents** — retriever, consolidation,
  embeddings, library, citations (`TestFuseRRF`,
  `TestRetrieveBitemporal`, `TestReview*`, `TestVecRoundTrip`,
  `TestSourceEventsOnlyRead`, walkthrough 26.9).
- [x] **H12. Assistant skills and decider** — assistant/domain skill
  groups, external skill installs, roles, escalation, decider
  (`TestAllBuiltinsValid`, `TestInstall*`, `TestCalibrateSeparatesBands`,
  `TestDeciderNeverFinalVerdict`, `TestGatewayP0Reservation`).
- [x] **H13. Capabilities, automation, extensions on Linux** —
  capability contracts, MCP, extension kit, triggers, workflows, flow
  simulation (`TestFourRoutesHotAdd`, `TestConformanceGate`,
  `TestWorkflow*`, `TestSim*`, `TestWebReadsRecordSources`).
