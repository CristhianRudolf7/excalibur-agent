# Changelog

All notable changes to Excalibur are recorded here. Nothing below
describes as implemented what is only planned.

## [Unreleased]

### Added

- Wave 0 contracts skeleton (H0): `go.mod`, full `internal/` package
  tree with responsibilities, `cmd/excalibur` CLI command surface with
  stable exit codes, `pkg/extsdk`, `packages/contracts` JSON schemas.
- Shared domain types (`internal/types`): ULID identifiers with type
  prefixes and short project codes, run/task/criterion states with the
  Section 7.2 transition table, scopes, effect classes, model roles,
  event envelope with cursor, core planning structs.
- Wire contracts (`internal/api`): JSON-RPC 2.0 envelope, method names
  and request/response shapes for every family, typed event schemas;
  parity-tested exports under `packages/contracts`.
- Core interfaces with in-memory fakes: `Store` (+ porter, read pool,
  blob store, migrations), `Provider` (streaming chat, tool calls,
  usage with cache read/write, normalized errors), `Tool` (+ registry,
  `excalibur-extension.yaml` manifest), `internal/platform` (Linux
  backends, `!linux` stubs), `safego.Go` with panic isolation and a
  bare-`go` guard script, layered `internal/config` with Appendix B
  defaults.
- Verification harness: `Makefile` (`verify`, `test`, `lint`, `cross`),
  GitHub Actions workflow, `AGENTS.md`, architecture notes, decision
  records, per-area development docs.
- H1 providers: OpenAI, Anthropic, Gemini, DeepSeek, OpenRouter, Ollama
  and generic OpenAI-compatible adapters with streaming, tool calls,
  usage/cache accounting, normalized errors and breaker/fallback in the
  `models` gateway; contract tests against simulated servers only.
- H2 storage: four-lane porter with group commit, read pool with an
  interactive reservation, content-addressed blobs, expand-contract
  migrations, network-FS `DELETE` fallback, retention/purge with
  tombstones, non-reused short codes.
- H3 durable engine: write-ahead journal with idempotency classes,
  crash recovery without repeated effects, PTY terminal with live
  output, file tools with fingerprints, extension registry.
- H4 non-interactive mode: `excalibur exec` with stable exit codes,
  frozen `eval` profile with recorded hashes (tool catalog, skill
  catalog, tools hash), ATIF trajectories, telemetry metrics,
  Harbor adapter and bench launchers; eval tool surface enforced by
  the engine gate.
- H5 verifiable work: task_db batches with optimistic concurrency,
  requirement/criterion/evidence states with staleness, completion
  gate, run-state machine, R1–R6 loop detector with staged response,
  per-tree budgets with closing mode.
- H6 context and cache: layered compiler with stable L0–L2 hashes and
  deltas, observation masking with pins, incremental compaction with
  literal instructions, provider cache adapters, calibrated token
  counting.
- H7 programming tools: deferred tools behind `search_tools` /
  `call_tool` without prefix churn, edit history with three-way revert,
  snapshots, LSP diagnostics, incremental code graph, leases with
  heartbeat/expiry, worktrees with integration queue.
- H8 subagents and delegation: three-level hierarchy with profile
  clamping, delegation cards, mailboxes with ack deadlines, interrupt /
  normal directives, cascade pause/resume/cancel, skill index with
  conflict/drift detection, `excalibur task` family, MCP delegation
  server, ACP adapter.
- H9 daemon and master: loopback JSON-RPC gateway with idempotent
  intake and cursor resume, fast lane without model calls, parallel
  per-conversation turns with steering, project registry with scope
  isolation on every query, daemon-routed delegation, notification
  classes with quiet hours and single delivery.
- H10 recovery and vault: boot reconciler with safe mode and
  quarantine, agenda (cron/timezone, reminders, triggers, durable
  workflows, flow simulation), retention scheduler, systemd install /
  supervised updates with migration rollback, secret vault with
  reference resolution and output redaction.
- H11 memory: bitemporal store with validity/recorded axes, RRF
  fusion over lexical/semantic/temporal/relational retrieval,
  admission with contradiction disputes, daily review with
  safeguards, embedder outage degradation with dual-read reindex,
  document library with exact page/paragraph citations.
- H12 skills and decider: integrated/programming/assistant/domain
  skill groups with validation, external installs with hash-change
  detection, model roles with escalation to an expert, calibrated
  decider with abstention bands and LLM fallback that never takes
  the code verdict.
- H13 capabilities and automation: capability task contracts over
  browser/search/vision/voice/channels/nodes/documents/apps,
  four-route extension kit with conformance gate, durable workflows
  with compensation, integrated jobs (research, debate, monitor)
  behind user confirmation.
- W5 closure: §26.1–§26.11 walkthroughs as backend integration tests
  (`tests/walkthrough_*_test.go`), `docs/development/coverage.md`
  with one row per architecture section, decision record 0034.
