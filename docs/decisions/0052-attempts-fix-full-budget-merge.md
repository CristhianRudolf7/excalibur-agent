# 0052 — Parallel attempts fix: full budgets, merged records, honest pause

Date: 2026-11-01. Workstream: attempts fix (`internal/attempts`,
`internal/cli/attempts.go`, `internal/cli/exec.go`, `internal/eval`).

## What

Fixes the EXCALIBUR_ATTEMPTS=3 failure observed on real evaluation
trials (read-only records under `/home/developer/bench-runs/attempts3/`):
about half the runs ended `paused` with summary `paused [best of 3
attempts]`, and the outer `excalibur/tool_calls.jsonl` and
`model_calls.jsonl` were missing (all calls lived only under
`attempts/attempt-i/excalibur/`).

## Root causes (from the trial records)

1. Step starvation: each attempt received `ceil(parentSteps/N)` steps
   (`DivideSteps(25,3)=9`). Every attempt hit its step cap after 9
   model calls and paused before verification, so all three attempts
   paused and the winner was an arbitrary paused attempt.
2. Missing audit: the outer recorder never saw model/tool calls (the
   outer run fans out before the engine), so standard evaluation and
   auditing read zero work from the outer `excalibur/` dir.
3. Empty pause reason: the engine-budget pause path returned state
   `paused` with empty reason and bare summary `paused`, which the
   fan-out propagated as `paused [best of 3 attempts]` with no reason
   in `final.json`.

## Decisions

- Full per-attempt budgets by default (`StepsForAttempt`,
  `BudgetForAttempt`): each attempt gets the full parent steps and
  explicit USD cap so it runs to its own verification; the wall clock
  stays shared via the parent context. `EXCALIBUR_ATTEMPTS_FULL_BUDGET=0`
  restores the legacy `ceil(parent/N)` split for ablation.
- Merged outer records by default (`mergeAttemptRecords`): after the
  fan-out joins, every attempt's `model_calls.jsonl` and
  `tool_calls.jsonl` rows are appended into the outer `excalibur/`
  files with an explicit `attempt` index (0 stays explicit via generic
  maps, since the typed structs use `omitempty`), blobs are copied,
  and gate checks gain `fanout_attempt`. `eval.ModelCall` and
  `eval.ToolCall` gain an additive `Attempt` field;
  `eval.GateAttempt` gains `FanoutAttempt`. All additive, old rows
  without the fields still parse.
  `EXCALIBUR_ATTEMPTS_MERGE_RECORDS=0` keeps the legacy outer-only
  files for ablation.
- Honest pause: the engine-budget pause path in `RunExec` now sets
  reason `budget` with summary `paused: budget exhausted: ...`, and
  the fan-out maps a bare paused winner to reason `budget` and summary
  `paused: budget exhausted [best of N attempts]`.

## Alternatives considered

- Scaling parent steps by N at the call site: rejected (callers would
  have to know N; the fan-out owns the policy).
- Sharing one recorder across attempts with interleaved rows:
  rejected (unattributable calls, racy seq, decision 0045).
- Serializing attempts or snapshotting absolute paths such as `/app`:
  out of scope (empty `ProjectDir` tasks that write absolute paths
  still share the filesystem; isolation there is best-effort and the
  winner overlay stays a no-op for empty src).

## Verification

- `internal/attempts` unit tests plus
  `internal/cli/attempts_test.go`: full-budget default, legacy split
  behind the switch, new end-to-end
  `TestAttemptsFullBudgetCompletesWhereSplitWouldPause` (container-like
  temp dir, `EXCALIBUR_ATTEMPTS=3`, `MaxSteps=6`, 3-step
  write/read/done script: completes with winner applied and outer
  records tagged 0/1/2) and `TestAttemptsLegacySplitPauses` (same setup
  with the switch off ends paused with reason `budget`).
- Full quality bar green: `gofmt -l .`, `go vet ./...`,
  `staticcheck ./...`, `go test ./... -race`, `GOOS=darwin/windows
  go build ./...`, `scripts/check-no-bare-go.sh`.
- Live end-to-end (own small tasks, never Terminal-Bench): `excalibur
  exec --profile eval` with `EXCALIBUR_ATTEMPTS=2` on hello-file and
  csv-sum tasks completes with merged attempt-tagged records.
