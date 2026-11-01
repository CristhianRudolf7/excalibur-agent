# 0053 — Long-horizon persistence: continuation, milestones, pacing, background discipline

Date: 2026-11-01. Workstream: long-horizon (`internal/tasks/longhorizon.go`,
`internal/engine/longhorizon.go`, `Run` milestone fields,
`Deps.LongHorizon`, `internal/cli/exec_engine.go` pacing/discipline).

## What

Paired measurements show earlier harness changes did not make the agent
work longer or deeper: hour-scale tasks end after 11-23 tool calls as
`completed` while the result is wrong. This workstream adds
time- and evidence-aware continuation (bounded, then closing mode), an
explicit milestone plan for large tasks with progress checks every N
steps, a budget-pacing line in L5, and background+poll discipline for
long commands. Switch `EXCALIBUR_LONG_HORIZON=on|off` (default on).

## Decisions

- Continuation fires both on a vacuous pass and when the honest end
  would land while significant budget remains (wall time when capped,
  steps otherwise; default 30% time left or 5 steps left). Gaps are
  fresh-verification plus exercise-the-deliverable gaps, plus the
  never-exercised end-to-end hole. Bounded by `MaxContinuations` (3);
  after the bound the run enters closing mode (`tool_choice: none`).
  Legacy runs without an instruction stay on the pre-long-horizon path
  so unit/e2e fixtures without a request keep their call counts.
- Draft-free tasks: freshness already enforces a verification pass
  after the last change. The never-exercised hole fires only when a
  change exists but nothing verified it, so read-only probes and
  trivial write+read tasks still finish while truly unverified work
  continues. `read_file` counts as verification for freshness but never
  as exercising: exercising requires `exec`/`call_tool` with run,
  import, query, parse or check markers (strongverify rule).
- Large means long instructions (>=400 chars) or enough drafts (>=3).
  The plan is explore, one implement milestone per draft (capped 4),
  verify, report (generic 4-phase fallback without drafts), pinned to
  working notes with a next-milestone nudge every 8 steps.
- Pacing line `pacing: elapsed ..., steps used/max (... left), time ...
  left of ... (...% left), milestones done/total` rides L5 every call
  (elapsed always, other segments only when capped), combined with the
  existing BudgetState. Each half has its own switch so ablations add
  no bytes.
- `exec` calls matching long patterns without `background:true` earn
  one volatile note per step naming the background+poll rerun. Finish
  while a session is alive and unverified keeps blocking through the
  artifact gate.

## Alternatives considered

- Extending `GateAttempts` instead of a separate continuation bound:
  rejected (gate attempts own the deterministic gate; long-horizon owns
  the time-aware extension, measurable separately).
- Forcing exercising checks for every draft-free task: rejected (breaks
  trivial file tasks and existing e2e call counts; freshness plus
  change-gated exercising preserves them).
- Milestone plan via `task_db` or model-declared plan: rejected
  (embedded exec/eval has no recorded plan; deterministic drafts keep
  the decision stable inside a run with zero extra model calls).

## Verification

- `internal/tasks/longhorizon_test.go`, `internal/engine/longhorizon_test.go`,
  `internal/cli/exec_longhorizon_test.go` (see agent-quality doc).
- Full quality bar green: `gofmt -l .`, `go vet ./...`,
  `staticcheck ./...`, `go test ./... -race`, `GOOS=darwin/windows
  go build ./...`, `scripts/check-no-bare-go.sh`.
- Live end-to-end (own small tasks, never Terminal-Bench): `excalibur
  exec` on deliberately long multi-step tasks confirms the mechanism is
  active in the records.
