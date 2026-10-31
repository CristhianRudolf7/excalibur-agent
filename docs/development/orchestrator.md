# H8 — Orchestrator and subagents (embedded mode)

Contract and backend tests of the H8 orchestration area: the
three-level hierarchy, identities and profiles (Section 15.1, 15.2),
when to delegate (15.3), the delegation card (15.4), mailboxes and
communication (15.5), status snapshots (15.6), redirection and
correction propagation (15.7), pause/resume/interrupt/cancel/undo
(15.8), subagent limits (15.9), the `subagent` tool, and the actor /
queue / lane / resource substrate (Sections 5.1, 5.2). Everything
runs in embedded mode: no daemon, no model, no SQL.

All logic lives in `internal/orchestrator` plus `internal/agents`
(profiles, identities, role toolsets). Persistence sits behind the
`orchestrator.Store` interface with an in-memory implementation
(`MemoryStore`) used by every test; the SQL binding lands with the
storage milestone (H2) behind the same interface. The engine seam is
`orchestrator.Runner`, implemented in tests by
`internal/orchestrator/fake`; see
`docs/decisions/0015-orchestrator-engine-seam.md`. The card carries
an explicit profile field; see
`docs/decisions/0014-orchestrator-delegation-card-profile.md`.

## Contract

- **Profiles and identities (15.2, 13.2, 15.9)**: `internal/agents`.
  `profiles.go` holds the ten built-in profiles with model role,
  step budget (master 8, parent 150, child 60), context target and
  tool removals; `MasterTools` / `SubagentBaseTools` encode the
  Section 13.2 role sets (master: no file/terminal/edit tools;
  subagents: everything but `send_message`/`settings` plus
  `ack_directive`; level 2 additionally loses `subagent`).
  `CustomAgent` registers user agents with their own identity
  document and a reduced toolset. `ClampTools` reduces, never
  expands. `identity.go` builds and validates the `agt_` identity
  (readable name, description, profile, parent, level 0-2, scope);
  level 2 has no spawn (`MaySpawn`/`ChildLevel`).
- **Hierarchy (15.1)**: `orchestrator.go`. `RegisterMaster` (level
  0), `Spawn` (card → child agent + run, created → queued →
  running via `Admit`), `Tree`, `ActiveChildren`, `RunningCount`.
  Level-2 spawn attempts are rejected and journaled
  (`spawn_rejected`). Embedded roots without a master sit at level
  1. The fan-out caps apply at admission: 4 active children per
  parent, 8 running per user; waiting/paused/blocked runs hold no
  slot, so a waiting parent never deadlocks its children.
- **Delegation card and when to delegate (15.3, 15.4, 15.9)**:
  `card.go`. `DelegationCard` (goal, deliverable, scope, paths,
  inputs, pinned instructions, suggested skills, tools, budget,
  deadline, done criteria, report contract, fork context, profile).
  `Validate` enforces presence, profile membership and tool clamp.
  `AdviseFanOut` encodes the Section 15.3 rules (parallel /
  isolation / verification / distinct profile spawn; coupled
  sequential work warns and stays put; overlapping file sets warn).
  `SplitBudget` / `RemainingBudget` / `ClampChildBudget` keep every
  child inside the parent remainder.
- **Mailbox (15.5)**: `mailbox.go`. `Message` rows
  (directive/question/answer/report/completion/blocked) with
  at-least-once delivery, ack and dedupe by id; `PendingMessages`,
  `PendingDirectives`, `AckDirective` (records `instructions_rev_seen`
  and journals `directive_applied`). Any directive wakes a waiting
  run; normal directives to paused runs queue until resume.
  `AskQuestion`/`AnswerQuestion` park and wake runs waiting on the
  user. `Escalate` climbs unchanged to the first live ancestor, then
  to a master notice. The ack watchdog (`CheckDirectiveAcks`)
  escalates after 2 steps or 120 s running, except inside
  `after_completion`/`cooperative` tools, which stay pending with
  the maximum wait reported.
- **Control (15.8, 7.2)**: `control.go`. `Pause` (immediate
  interrupt, leases released, cascade with reason parent and
  `paused_by`), `Resume` (only what the same order paused),
  `Interrupt` (full → parked; tool-only → tool stops, run decides),
  `Cancel` (bottom-up with a report per run; definitive afterwards),
  `Redirect` (interrupt directive; resumes a paused target),
  `SetWaiting`/`Wake` (Section 7.2 wake events), `Block`/`Unblock`,
  `SetRecovering`, `Transition` (diagram-checked), `Finish` (walks
  running → verifying → terminal through the gate). Freed slots
  always re-run admission. Every state row journals
  `state_change` and emits a bus event.
- **Correction propagation (15.7)**: `redirect.go`. `PropagateCorrection`
  persists the next instruction revision first, computes the
  affected runs (task-linked plus scope-wide), decides one priority
  per run with `DecidePriority` (explicit stop, resource-key ∩
  impact-area `MatchResourceKeys`, decider prohibition at ≥ 0.7
  confidence, pending external action), fans the directives out
  top-down, calls `Hooks.InvalidateEvidence`, and returns the
  master confirmation (adjusted / already finished / waiting on a
  non-interruptible tool).
- **Status (15.6)**: `status.go`. Deterministic snapshots from the
  database, orchestrator memory and the runner snapshot: tree,
  state/reason, queue position, step and model wait, tool in flight
  with elapsed time and live tail, computed last progress, latest
  report, plan progress (hook override), pending questions and
  blocks, budget and consumption, and the 15-step/10-minute report
  reminder flag. `ToAPI` maps to `run.status`; `Plain` renders the
  master one-liner.
- **Actors, queues, lanes, resources (5.1, 5.2)**: `lanes.go`
  (per-provider `Gateway` actor: lanes P0-P3, P0 keeps one
  concurrency slot and 10% of the minute quota, lower lanes wait for
  higher ones, P3 yields while the user is active),
  `orchestrator.go` admission (per-lane ready queue with per-scope
  round robin and queue positions), `resources.go` (RAM watchdog:
  halt, shed P3→P1, last-resort kill of a live tool in a
  watchdog-paused run, hysteresis resume one by one, never
  user-paused runs).
- **Delivery reports and undo (7.6, 15.8)**: `report.go`.
  `DeliveryReport` on terminal, paused and blocked runs;
  `PlanUndo` orders compensations reverse (edits/snapshots/commits
  via the harness, external effects only with a declared inverse)
  and marks the rest not reversible.
- **The `subagent` tool (13.2)**: `subagent.go`. Implements
  `tools.Tool` over one orchestrator: spawn/status/message/
  redirect/pause/resume/interrupt/cancel with idempotency-key
  dedupe. The engine injects the calling `agt_` id; the master acts
  anywhere, a parent only in its subtree, level 2 is denied.
- **Runner seam and fake**: `runner.go` (`Runner`,
  `RunnerSnapshot`, `RunnerRegistry`), `fake/` (scripted runner:
  steps, tools in flight per cancel class, safe-point acks,
  interrupt honoring, parked runs that consume nothing while tools
  wind down).

## Backend tests

All in `internal/orchestrator` (`*_test.go` in-package for pure
rules, `package orchestrator_test` with `harness_test.go` for
embedded-mode scenarios: one orchestrator over `MemoryStore` with a
controllable clock, a master, a running parent and attached fakes):

- `card_test.go`: card validation matrix, `AdviseFanOut` advice and
  warnings, budget remainder/split/clamp arithmetic, `PlanUndo`
  reversibility order.
- `hierarchy_test.go` (15.10 rows: depth, clamp, budget, caps): L2
  spawn rejected and journaled; card widening rejected; master with
  no work tools spawns a parent that has them; child capped by the
  parent remainder; 5th child queues at the fan-out cap and is
  admitted when a sibling pauses; the 9th execution queues at the
  running cap and a waiting parent frees its slot without deadlock.
- `mailbox_test.go` (15.10 row: mailbox): duplicate delivery stored
  once with a single wake; child messages wake a waiting parent;
  orphan questions become master notices; ask parks and answer
  wakes; ack records the seen revision; cooperative tools stay
  pending past the deadline while ordinary tools escalate after 2
  steps.
- `control_test.go` (15.8 over every state): pause cascade with
  `paused_by` and selective resume; tool-only vs full interrupt;
  bottom-up cancel with reports and definitive rejection after;
  redirect resuming a paused run; illegal transitions rejected;
  finish walking the gate path in the journal; block/unblock and
  recovering edges; concurrent orders serializing with cancel
  winning.
- `redirect_test.go` (15.7 point 4 + propagation): one test per
  interrupt rule; key/area matching; the 26.11 end-to-end (suite
  runner with session keys goes normal, implementer editing inside
  the area interrupts, finished runs reported, evidence invalidated
  at the new revision).
- `status_test.go` (15.10 row: status): tool in flight with elapsed
  time and live tail, computed last progress, plan and budget
  fractions, plain template, `run.status` mapping, nested tree with
  queue positions, reminder after 15 silent steps.
- `resources_test.go` (5.2): shed order P3-first, halted admission,
  hysteresis (first low sample holds, 60 s under 80% resumes P1
  first), user-paused runs never resumed, last-resort kill of a
  live tool.
- `lanes_test.go` (5.2): P0 reservation under saturation, lane
  order on a freed slot, P3 yielding while the user is active,
  minute-quota reservation.
- `subagent_test.go`: tool definition validity, spawn/status/
  message round trip, master-anywhere vs parent-subtree vs L2
  denial, every control action, idempotency-key replay without a
  second spawn.
- `suite_steer_test.go` (23.5 `steer`): seeded corrections across a
  live tree; adherence per case (interrupt at once with 0-step
  latency; normal at the safe point or via the 2-step escalation;
  non-interruptible tools pending with the maximum wait) plus the
  exact 2-step escalation of a silent addressee.
- `suite_control_test.go` (23.5 `control`): quiescence per cancel
  class (immediate clears at once, graceful ends next step,
  after_completion finishes within its bound with zero consumed
  steps, cooperative stays pending while the run continues,
  canceled runs terminal with a report and zero consumption) and
  name resolution (unique vs ambiguous vs unknown).

## Deferred (not in H8)

No daemon, master turns, reception, SQL, journal replay across
restarts (the journal interface is written, not replayed), leases
(`Hooks.ReleaseLeases` seam), plan-progress and evidence
invalidation wiring (`Hooks` seams), model lanes beyond the
`Gateway` actor, MCP/CLI/ACP delegation surfaces (they call the
same orchestrator methods), and the `avail`/`notify`/`crash` suites
(H9 and recovery milestones).
