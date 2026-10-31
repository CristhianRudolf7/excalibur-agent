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
- `docs/decisions/dependencies.md`: `coder/websocket` v1.8.13 (ISC).

## Deferred

- SQLite-backed gate stores (client_messages, messages, blobs
  refcounts) — the seams exist (`Backend`, `BlobStore`); the porters
  land with the storage milestone integration.
- Real channel adapters (Telegram/voice) behind `ChannelSender` and
  the voice hook; agenda wiring for reminders/schedules.
- `excalibur daemon start|stop` CLI and MCP server delegation call
  sites: owned by the CLI/MCP milestones; the `CallDaemon` seam is
  ready for them.
