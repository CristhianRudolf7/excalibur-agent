# Roadmap

Milestones of Appendix E. Status: H0 done; H1-H13 planned. Nothing is
claimed as implemented until its milestone lands with tests.

Phase A builds the programming harness first (measurable in
Terminal-Bench from H4 on); Phase B builds the personal assistant on
the same core. Each mechanism is compared on/off (ablation) before it
stays on in `eval`.

- [x] **H0. Repository skeleton and shared contracts (wave 0)** —
  module, package tree, shared types, API contracts, core interfaces
  with fakes, CLI skeleton, verification harness.
- [ ] **H1. Providers of language models** — `internal/providers`
  adapters (OpenAI, Anthropic, Gemini, DeepSeek, OpenRouter, Ollama,
  generic OpenAI-compatible) on the wave-0 contract; contract tests
  against simulated servers.
- [ ] **H2. Base storage** — identifiers, layered config loading, Linux
  `internal/platform`, SQLite porter, blobs, migrations.
- [ ] **H3. Durable engine and terminal** — journal, agent loop, gateway
  subset, context compiler basics, core file/terminal tools, extension
  registry.
- [ ] **H4. Non-interactive mode and benchmarks** — `exec`, eval profile,
  events, metrics, ATIF, Harbor adapter, bench launchers. First
  Terminal-Bench baseline.
- [ ] **H5. Verifiable work** — tasks/plans/requirements/criteria/
  evidence, completion gate, run states, loop detector, limits.
- [ ] **H6. Context and cache** — layers, deltas, masking, compaction,
  cache adapters, token counting.
- [ ] **H7. Programming tools and edit concurrency** — deferred tools,
  `search_tools`, history, snapshots, LSP, code graph, leases, worktrees.
- [ ] **H8. Subagents, programming skills, delegation** — orchestrator,
  profiles, mailboxes, directives, skill index, `task` family, MCP
  server, ACP.
- [ ] **H9. Daemon, master, conversation** — gate, intake, fast lane,
  projects, scopes, delegation through the daemon.
- [ ] **H10. Recovery, continuous operation, vault** — boot, safe mode,
  agenda, retention, install/update, secrets vault.
- [ ] **H11. Memory and documents** — retriever, consolidation,
  embeddings, library, citations.
- [ ] **H12. Assistant skills and decider** — assistant/domain skill
  groups, external skill installs, roles, escalation, decider.
- [ ] **H13. Capabilities, automation, extensions on Linux** —
  capability contracts, MCP, extension kit, triggers, workflows, flow
  simulation.
