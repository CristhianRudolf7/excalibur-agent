# H9 — Daemon, master and conversation

Contract and backend tests of the H9 area: the gate (`internal/gateway`),
the daemon lifecycle (`internal/daemon`) and the master agent
(`internal/master`). The H8 orchestrator runs under the master; CLI/MCP
delegation routes through the daemon when active (Section 3.6).

Architecture reference: Sections 3.2 (processes), 3.5 (client surface),
3.6 (delegation), 3.7, 5 (execution core and master availability),
5.3–5.5, 6 (intake and interpretation), 6.7, 9.3–9.5 (scopes),
15.5–15.8 (via the orchestrator), 17.3 (boot), 22.1 (events) and the
walkthrough 26.11 with its checks 26.12–26.13.

## Contract

- **Gate** (`internal/gateway`): JSON-RPC 2.0 over HTTP POST `/rpc`
  (single and batch) and WebSocket `/ws`, blobs over HTTP
  (`PUT`/`GET /blobs/{sha256}`), loopback-only bind by default
  (non-loopback rejected without `AllowRemote`), `client_msg_id`
  idempotency, cursor subscriptions with type filters and gap-free
  replay. `Backend` is the method seam; `MemoryBackend` serves tests.
  `Client.Dial` + `DaemonActive` + `Outbox` are the delegation and
  resend-side pieces of Section 3.5.
- **Master** (`internal/master`): Section 6.1 intake (normalize,
  persist with interactive priority, deterministic preanalysis,
  decider triage only when unresolved — never an LLM call), the
  Section 6.1 fast lane (slash commands without a decider;
  natural-language status/control with high-confidence resolved
  targets; ambiguous targets ask; mixed messages split control from
  content), per-conversation sequential turns parallel across
  conversations with the <2 s cancel/restart rule (5.3.4), 8-step
  cap, provider-down deterministic fallback, request cards (6.3),
  ask-or-assume (6.4: blocking/non-blocking, 15 min default,
  24 h reminder, 3-question cap), the voice barge-in contract (6.6)
  and the Section 5.4 notifier (urgent at once, normal grouped in
  5 s windows, informative on demand; once per channel; quiet hours;
  activity links; single ack).
- **Daemon** (`internal/daemon`): `Start` (loopback bind, GOMEMLIMIT,
  crash-history check, boot adoption, orchestrator loop, gate serve),
  controlled `Stop` (gate drain, orchestrator stop), admission sized
  `NumCPU − reserved` (Appendix B: 2) for background work only,
  Section 17.3 adoption (resume <6 h reconciled / block on uncertain
  effects / pause on long outage / keep user waits), safe mode after
  3 crashes in 10 min (agenda registers but never launches; 30 min
  quiet or explicit order leaves; repeated run/step quarantined),
  `Bridge` (orchestrator events to bus + notifier), `OrchControl`
  (H8 status/control surface for the fast lane) and `DaemonBackend`
  (conversation via master, runs via orchestrator, task/files/vault
  families via the embedded backend seam).
- **Through-daemon delegation** (`internal/daemon/delegate.go`):
  `CallDaemon` maps unreachable daemons to `ErrDaemonDown` for the
  embedded fallback; `MustDaemon` reports whether delegation must go
  through the gate.

## Backend tests

- `internal/gateway`: loopback refusal, `client_msg_id` idempotency
  (one row after resend), unknown-method `CodeMethodNotFound`,
  notification without response, cursor replay without gaps or
  duplicates, blob round-trip (mismatch 400, missing 404), outbox
  flush, WebSocket RPC + subscribe ack, batch order, method-registry
  parity with `api.Methods` (3.7).
- `internal/master`: deterministic preanalysis corpus (commands,
  T/R/ULID refs, paths, URLs, relative dates across a DST fold,
  ambiguous mentions with all candidates), triage cost rule (one
  decider call, zero model calls), fast-lane table (slash-only
  without decider, resolved/ambiguous/mixed, stop semantics),
  ask-or-assume matrix (mandatory/irreversible/ambiguous/trivial,
  question cap, 15 min default, 24 h reminder), request-card
  invariant, turn cancel/inject, parallel conversations, provider
  fallback with live fast lane, panic containment, the `notify`
  suite (urgent/normal/informative × connected/disconnected × active
  elsewhere × quiet hours, dedupe, single ack, 5 s grouping) and the
  voice contract (5.5, 6.7).
- `internal/daemon`: start/stop lifecycle, adoption matrix and safe
  mode, crash history (enter/leave/quiet-exit, quarantine),
  delegation fallback, bridge forwarding, the 26.11 walkthrough
  (Lena status template with tool + live line + plan, R4 correction
  with interrupt/normal priorities and mailbox behavior, thesis
  pause cascade with gastos untouched) and the `avail` suite (ack
  and fast-lane p50/p95 at 0/4/8 background runs, CPU-saturated and
  provider-down).

## Decisions

- `docs/decisions/0024-h9-gateway-daemon-master.md`: what, why and
  the alternatives for the H9 seams.
- `docs/decisions/0030-daemon-wiring.md`: cross-milestone wiring
  (agenda, orchestrator, capabilities, memory review, decider,
  boot, CLI/MCP/ACP through the gate).
- `docs/decisions/dependencies.md`: `coder/websocket` v1.8.13 (ISC).
  The wiring wave adds no dependencies.

## Wiring (wave 4)

Every gate family resolves to a real package through
`DaemonBackend.AttachServices`; nil hooks keep the embedded
`MemoryBackend` for embedded profiles and unit tests.

- **Agenda** (`internal/daemon/launch.go`, `internal/agenda`):
  `OrchestratorLauncher` (fires spawn coordinator children,
  reminders post Section 5.4 notices), `DaemonTriggerLauncher`
  (task/flow/notify/session per trigger action),
  `QuestionConfirmer` (confirm nodes become answerable
  `question.answer` questions, 15 min default, decline on nil
  store), SQLite stores + `EnsureSQLSchema`, `Agenda.CatchUp`
  (boot catch-up with safe-mode deferral), `TriggerPump` with the
  six §21.3 normalizers (mail, channel, webhook, file, calendar,
  capability completion).
- **Decider at Table 2.1** (`internal/daemon/deciders.go`):
  `ModelDecider` (triage type/scope/durable/clarify with
  abstention and deterministic fallback),
  `MemoryJudge` (ambiguous-band admission), `SkillScorer`
  (skills vector leg), `JudgeStuck` (step ≥ 30, every 10, p ≥ 0.9,
  note-only), `JudgeNotify`, `RerankTools` (13.3).
- **Orchestrator under the master** (`internal/daemon/services.go`):
  `OrchestratorRunStore` + `EngineReconciler` + `EngineRunner`
  (supervision-tree Runner with directive/notes interrupts),
  `MemoryReviewLoop` (10.6/21.9, yields to activity),
  `RAMWatchdog` (platform sampler → `WatchdogTick`),
  `TaskService` (task.* as orchestrator runs, visible to master).
- **Gate families** (`internal/daemon/routes.go`, `caps.go`,
  `files.go`, `config.go`, `tasks.go`): task.*, files.list
  (.gitignore + heavy dirs + agent_touched), project.*, memory.*
  (scoped retrieve, authoritative correct, provenance), skills.*
  (search/load over the builtin index), capability.* (registry +
  MCP lazy dial), vault.* (values only in the dedicated call),
  question.answer, metrics.query, config.*, plan.* (conflict until
  a plan store lands).
- **Daemon lifecycle** (`daemon.go`, `safe.go`, `addrfile.go`):
  full `Boot` at `Start` (structured crash history unified with
  the JSONL trail, quarantine mirrored), services start/stop,
  addr-file rendezvous (`EXCALIBUR_DAEMON_ADDR` override),
  `Backend()` exposes the composed backend,
  `conversation.history` reads the master store.
- **CLI/MCP/ACP** (`internal/cli/daemonroute.go`,
  `cmd/excalibur/daemon.go`, `routed.go`): `task *` daemon-first
  with embedded fallback on unknown/down, `daemon start|stop`
  (pid file), routed `chat/status/tasks/projects/memory/skills/
  plan` families, `VaultEntryParams.Value` for the dedicated
  secure call.

## Backend tests (wiring)

- `internal/daemon`: launcher spawn/notice rows, confirmer
  yes/no/nil rows, triage bands + deterministic fallback,
  admission/stuck/notify/rerank rows, full gate-family routing
  (task/memory/skills/capability/project/files/vault/question/
  metrics/config), WebSocket event fan-out through the bridge,
  boot-with-adapters (long outage parks paused, single notice),
  review-loop tick.
- `internal/cli`: daemon-first task flow against a live in-process
  daemon (start/status/wait/result/list/logs/reply/cancel).
- `internal/agenda`: SQL store round-trips (schedules, reminders,
  triggers, fire_id idempotency, pending), CatchUp live vs
  safe-mode, trigger pump dispatch + dedup.
- `internal/capabilities`: registry round-trip, hot-add,
  browser flow.
- `internal/workspace`: `LoadIgnore` + `History.Paths` for the
  files.list filters.
- `tests/`: walkthroughs 26.1–26.11 through the real gateway
  (receipt idempotency, conversion, project fan-out + cancel
  cascade, scope isolation, leases, redirect, power-cut boot,
  eval profile, memory contradiction, pressure/outage, master
  control).

## Current limits and later integrations

Concrete Telegram and WhatsApp transports remain outside this tree. Messaging,
calendar and meeting capability simulations do not establish live connectivity.
The production daemon now wires persistent stores, plan approve/deny, provider
models, browser/search, audio and Google tools; see [persistence](persistence-wiring.md),
[model wiring](wire-engine.md), [browser/search](browser-search.md),
[voice](voice-live.md) and [Google](google.md) for their evidence and limits.
Project file enumeration intentionally collapses heavy directories rather than
providing an unrestricted recursive filesystem listing.
