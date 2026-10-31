# H5 — Verifiable work

Contract and backend tests of the H5 area: tasks, plans,
requirements, criteria and evidence (Section 8), the `task_db`
tool with grouped operations, the completion gate (Section 7.5),
the delivery report (Section 7.6), and verification, loop
detection and limits (Section 18).

All logic lives in `internal/tasks` as engine-independent library
code: it depends only on `internal/types` and on the
`PlanningStore` interface defined in `store_mem.go`, so the engine
(H3) and the orchestrator (H8) can adopt it without import cycles.
Persistence behind `PlanningStore` has an in-memory implementation
(`MemoryStore`) used by every test; the SQL binding lands with the
storage milestone (H2) behind the same interface.

## Contract

- **Levels (8.1)**: `levels.go`. N1/N2/N3 with deterministic
  promotions (project touch or >15 steps moves N1 to N2; an
  estimate past one day, more than one milestone, or a user request
  moves N2 to N3); a forced level wins. `Promote`, `NeedsProject`.
- **Planning model (8.2)**: `model.go` (plans, plan items,
  dependencies, milestones, projects, logbook, task-requirement
  edges), `graph.go` (acyclic inserts via `WouldCycle`/`FindCycle`,
  `Descendants`, deterministic `AffectedSubgraph`). Task states
  derive from the root run via `types.DeriveTaskState`; the model
  never writes them. Reopening a finished task starts a fresh root
  run and keeps the history (`MemoryStore.ReopenTask`,
  `TaskHistory`).
- **Criterion state and freshness (8.3, 8.6)**: `freshness.go`.
  `IsFresh` implements the freshness algorithm (impact-area
  fingerprints plus the applicable instruction revision);
  `LatestForCriterion` selects the newest evidence across checks;
  `ComputeStatus` resolves satisfied/failed/unverified; the three
  queries (`QueryUnchecked`, `QueryFailed`, `QueryStale`) run in
  `QueryCriteria`; `CriteriaAffectedByEdit` is the reverse index
  path to criteria. `CodeVersion` records commit plus dirty-tree
  hash.
- **Macro and micro plans (8.4)**: `plans.go`. `EnsureMacroPlan`,
  `NeedsApproval` (N3 or on request), `SubmitForApproval`,
  `ApprovePlan` (comments come back as directives),
  `ApplyProposal` (validates with `ValidatePlan` and lands a new
  version), `EnsureMicroPlan`/`ArchiveMicroPlan` (one id per
  incarnation), `CompactMicro` for the injected task-layer form.
- **Lifecycle (8.5)**: `lifecycle.go`. `Query` builds the no-model
  snapshot (tree, progress, blocks, agent, last report);
  `PauseOrder` cascades with the user motive at the root and the
  parent motive below; `ResumeOrder` returns exactly what the order
  paused (tracked via `PausedBy`); `CancelOrder` goes bottom-up with
  compensations and asks a one-line confirmation (`ConfirmFunc`).
  All three run against the `RunTree` interface; `MemRunTree`
  (tests) adapts it in memory.
- **Replan (8.5)**: `replan.go`. Triggers as constants,
  `ComputeAffected` for the deterministic subgraph (changed nodes
  plus descendants plus requirement-linked tasks), `ApplyReplan`
  freezing finished non-invalidated work, validating criteria
  replacements, landing a new plan version, and emitting directives
  plus the master notice on scope changes.
- **Verifier (8.7)**: `verifier.go`. `ShouldVerify` (N2 with 5+
  requirements, N3 milestones, user requests), `VerifierInput`
  (no executor reasoning by construction), `RunVerifier` with the
  2-round cap reporting both positions, `RecordVerdicts` as
  independent-verification evidence.
- **Checklist (8.8)**: `checklist.go`. `ChecklistLineFor`,
  `RenderChecklist`, `SpliceReadme` (marker-delimited splice,
  outside content preserved), `RegenerateChecklist` through the
  `Editor` lease/history pipeline, `ChecklistPath` with the eval
  profile outside the evaluated tree.
- **General board and notepad (8.9)**: `notebook.go`.
  `GeneralBoard`, versioned `Notebook` notes.
- **task_db (8.10)**: `taskdb.go`. `ApplyBatch` runs grouped
  operations (projects, tasks, plans, items, dependencies,
  milestones, requirements, criteria, checks, evidence,
  assumptions, decisions, logbook, queries) against a private
  store copy and swaps it in only when every operation validates;
  short-code resolution (`T45`, `R4`, `C12.3`) works for rows the
  same batch created; updates carry optimistic-concurrency
  versions and reject with `VersionConflict`; queries default to
  20 rows and ~2000 tokens with a has-more flag.
- **Gate (7.5)**: `gate.go`. `Evaluate` checks criteria with the
  non-executable evidence rules (inspection on judgement needs
  the verifier in N2/N3, code criteria never count on judgement,
  user_confirmation waits without blocking forever), the project
  checklist, children (paused/blocked children force an explicit
  decision), instruction revisions, the once-per-task contrast,
  and the verifier verdict. `HonestState` maps failures after
  `GateAttempts` to completed_with_pending or blocked;
  `ConfirmationOnly` lets a lone confirmation wait close later
  without redoing work.
- **Delivery report (7.6)**: `report.go`. `BuildReport` holds
  state with reason, summary, deliverables, evidence, files,
  assumptions, questions, pendings and metrics, with
  `PlainLanguage` mapping paused-by-budget/time/order and
  completed_with_pending to "incomplete".
- **Ladder and selection (18.1, 18.2)**: `verify.go`.
  `LadderRank`, `CodeCriterionSatisfiedBy`,
  `InspectionSatisfiedBy`, `SelectDuringWork` (impacted plus
  in-progress), `SelectBeforeClose` (full folder plus every
  check), `SelectCandidate` by distinguishing tests, never by a
  judge.
- **Loop detector (18.3)**: `loops.go`. Per-execution
  `LoopDetector` with h1/h2 signatures and the R1-R6 rules at
  Appendix B thresholds, staged responses (note, escalate,
  blocked), the `wait_for` long-poll exemption, and the optional
  stuck judge (step 30+, every 10, p >= 0.9) that only fires the
  note.
- **Limits (18.4)**: `limits.go`. `Tracker` with ok/closing (<10%
  left)/exhausted states and `ExhaustedBy`, `RemainingBudget` and
  `Distribute` so a child never exceeds the parent remainder,
  plus `UserBudget`, `ProviderQuota` and `SystemLimits`
  (256 MiB WAL alert default).

## Backend tests

- `levels_test.go`: promotions N1/N2/N3, forced level, project
  requirement (row 8.11.4).
- `graph_test.go`: cycle rejection, stable cycle report,
  descendants, affected subgraph, plan validation matrix.
- `freshness_test.go`: edit stales evidence on the spot,
  instruction bumps stale, the three queries, reverse index, code
  version roundtrip (row 8.11.2).
- `lifecycle_test.go`: task-state derivation table, reopen with
  history, pause/resume cascade with order scoping,
  bottom-up cancel with confirmation, snapshot query (rows
  8.11.3, 8.11.6).
- `replan_test.go`: affected subgraph, frozen work, versioned
  plan, directives and master notice, invariant guards (row
  8.11.5).
- `verifier_test.go`: trigger rules, reasoning-free context,
  agreement, re-run with the 2-round cap, verdict recording (row
  8.11.7).
- `checklist_test.go`: marker splice, history conservation,
  line states, eval path (row 8.11.8).
- `notebook_test.go`: general board, note versions (8.9).
- `plans_test.go`: macro dedup, approval flow, micro lifecycle,
  compact render (8.4).
- `taskdb_test.go`: cyclic and missing-reference batches
  rejected whole with the concrete reason, grouped ops with
  same-batch queries, optimistic concurrency, row/token caps
  with has-more (rows 8.11.1, 8.11.9).
- `gate_test.go`: every defect blocks completion, clean gates
  pass, honest states after two attempts, later confirmation
  closes without redo (row 7.7.1).
- `report_test.go`: full-field reports on pause and block,
  plain-language states (row 7.7.2).
- `verify_test.go`: judgement rules, ladder order, work vs
  close selection, test-based candidate choice (rows 18.5.1,
  18.5.2).
- `loops_test.go`: planted R1-R6 trajectories with staged
  note/escalate/blocked, exempt wait_for polling, judge
  note-only, zero false positives on benign traffic (row
  18.5.3).
- `limits_test.go`: closing then exhausted per limit, budget
  extension resumes, tree remainder caps, user/provider/system
  budgets (row 18.5.4).

## Deferred (not in H5)

No SQL binding (H2 owns the porters; `PlanningStore` is the seam),
no engine step loop or journal wiring (H3), no orchestrator,
subagent spawning, mailboxes or directives delivery (H8), no
leases or edit history pipeline (H7 provides the `Editor`
adapter), no memory consolidation behind the notepad (H11).
