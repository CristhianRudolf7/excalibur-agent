# 0030 — Daemon wiring: every gate family on real packages

Date: 2026-10-31. Scope: cross-milestone wiring on the daemon side
(agenda launcher/confirmer/event sources, orchestrator under the
master, capabilities registry, memory review, decider at the Table
2.1 points, recovery at boot, CLI/MCP/ACP through the daemon API).

## What

- `capabilities.Registry` + `DefaultRegistry()`: every native
  capability behind one registry; gate `capability.*` invokes,
  polls and lists through it; unbound manifests fail loudly.
- Agenda SQLite (`SQLAgendaStore`, `SQLReminderStore`,
  `SQLTriggerStore` + `EnsureSQLSchema`), `Agenda.CatchUp` (the boot
  `AgendaCatchUp` seam), `TriggerPump` with the six §21.3
  normalizers, `OrchestratorLauncher`, `DaemonTriggerLauncher`,
  `QuestionConfirmer` (orchestrator question flow, 15 min default).
- `ModelDecider` (master triage), `MemoryJudge` (admission band),
  `SkillScorer` (vector leg), `JudgeStuck`, `JudgeNotify`,
  `RerankTools`: Table 2.1 rows through `models` ports with
  abstention and deterministic fallbacks; intake never waits on a
  model.
- `OrchestratorRunStore`, `EngineReconciler`, `EngineRunner`,
  `MemoryReviewLoop`, `RAMWatchdog`, `TaskService` (tasks as
  orchestrator runs), `CapabilityRoutes` (MCP lazy dial),
  `ProjectFiles` (files.list with .gitignore + agent_touched),
  `FileConfig`, `MetricsStore`.
- Daemon lifecycle: full `Boot` at `Start` when a `RunStore` is
  present (structured crash history unified with the JSONL trail),
  services start/stop, addr-file rendezvous, `Backend()` returns the
  composed backend, `conversation.history` reads the master store.
- CLI/MCP/ACP: `task *` daemon-first with embedded fallback,
  `daemon start|stop`, routed `chat/status/tasks/projects/memory/
  skills/plan` families, `VaultEntryParams.Value` (dedicated secure
  call carries the value; see below).
- Additive exports: `workspace.LoadIgnore` + `History.Paths`;
  `master.History` + `ConversationStore.MessageIDs`;
  `api.VaultEntryParams.Value`.
- Additive seam fix: `engine.RecoverProber` declares `Probe` with
  `[]byte` and therefore does not satisfy `journal.Prober`
  (`json.RawMessage`); the daemon `recoverProber` closes the gap
  without touching either contract.

## Why

The wave status files deferred exactly this integration work to the
daemon wave (agenda SQLite/launcher/confirmer/event sources,
orchestrator SQL store + engine Runner + master/daemon wiring,
capability gate wiring, memory gate/CLI wiring, live backends behind
fakes). Each seam now resolves to a real package; fakes stay only in
unit tests.

## Alternatives considered

- Orchestrator SQL store instead of `MemoryStore` in `Assemble`:
  rejected — no SQL implementation exists (H2/H3 seam), so the daemon
  uses the memory store and the boot adapters already speak
  `orchestrator.Store`, making the swap mechanical later.
- Turn tool execution inside master turns: left to the
  provider/model gateway (the `ModelCaller` seam); the daemon wires
  everything around it (intake, fast lane, delegation, questions).
- Daemon shutdown via a gate method: rejected — no contract change;
  `daemon stop` signals the pid file (Unix semantics, no new API).

## Deferred (unchanged behavior, documented)

- Live search/vision/TTS/model backends (still `live`-tagged
  stand-ins per §14.13); real channel adapters; plan approve/deny
  (no plan store yet — the gate returns the conflict); skills.install
  from staged dirs; worktree-backed daemon tasks (embedded flow
  unchanged); full .gitignore walk beyond one level (documented
  subset + heavy-dir collapsing).
