# Architecture overview

Excalibur is a personal AI assistant and programming harness: a single
Go binary (`cmd/excalibur`) that receives requests, converses through a
master agent, plans verifiable work, executes tools as low-priority
child processes, coordinates subagents, remembers across scopes,
compiles small stable contexts, verifies with executable checks and
recovers from failures without duplicating effects.

Behavior is specified by the architecture document (outside this
repository, never committed here). Sections below reference it by
number only.

## Components (3.1)

One daemon per OS user. Goroutines are agents; child processes exist
only for tool execution, each in its own process group. Key pieces:

- **Gate** (`internal/gateway`, `internal/api`): JSON-RPC 2.0 over
  WebSocket plus HTTP blob upload/download. Loopback-only by default.
- **Intake** (`internal/master`): normalize, persist with interactive
  priority, deterministic pre-analysis, decider triage, fast lane or
  master turn.
- **Master** (`internal/master`): light interlocutor, fast tools only,
  delegates everything else (P5).
- **Orchestrator** (`internal/orchestrator`) and **engine**
  (`internal/engine`): run queue, one step loop per run, completion
  gate, loop detector.
- **Context compiler** (`internal/ctxcompiler`): layered, cache-stable
  prompts; masking before compaction.
- **Memory** (`internal/memory`): layered memory with scoped retrieval,
  consolidation, document library, code graph (`internal/codeintel`).
- **Tools** (`internal/tools`): small fixed core, deferred tools on
  demand, extensions via `pkg/extsdk`.
- **Models** (`internal/models`) over **adapters**
  (`internal/providers`): roles, routing, prices, decider, failover.
- **Agenda** (`internal/agenda`): reminders, cron, triggers, durable
  workflows.
- **Journal** (`internal/journal`) over **porters**
  (`internal/store`): durable log, reconciliation, retries.
- **Bus and telemetry** (`internal/telemetry`): typed events with
  cursors, per-task metrics, ATIF traces.
- **Capabilities** (`internal/capabilities`): browser, search, vision,
  voice, devices, documents, apps, messaging, nodes, MCP, store, GPU.
- **Vault** (`internal/vault`): the only security piece of this
  version (20.1).
- **Daemon** (`internal/daemon`): boot, supervision, updates.
- **Eval** (`internal/eval`): `exec`, eval profile, Harbor adapter.
- **Platform** (`internal/platform`): every OS dependency behind
  interfaces; Linux implemented, the rest stubs.
- **CLI** (`internal/cli`, `cmd/excalibur`): thin layers over the core.

## Design principles (2)

State lives in the database and context is a projection (P1);
deterministic code decides everything it can (P2); executable
verification beats self-grading (P3); small stable context prefix (P4);
light interlocutor (P5); scopes isolated in SQL (P6); learning only
behind evidence (P7, deferred); few well-designed tools (P8); explicit
budgets (P9); OS behind interfaces (P10).

## Deployment (3.2, 3.3)

Full profile (Linux desktop/server) and eval profile (ephemeral
container, `exec` only). Remote nodes are external capabilities.
Background work is admission-controlled with reserved cores for the
interactive path; tool processes run at low priority; panics are
isolated per owner via `safego.Go`.
