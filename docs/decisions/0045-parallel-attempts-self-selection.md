# 0045 — Parallel attempts with self-selection (best-of-N)

Date: 2026-11-01. Workstream: parallel attempts (`internal/attempts`,
`internal/cli/attempts.go`).

## What

Best-of-N with self-selection (Section 18.2): `EXCALIBUR_ATTEMPTS=<N>`
fans one `exec` run out to N isolated working copies (git worktree when
available, else overlay copy), each attempt runs the normal loop to its
verification phase as a sub-`RunExec`, then selection keeps the attempt
whose own executable checks pass (completed first, then more verified
checks, fewer errors, then a cheap verifier-role model comparison of
evidence as the final tie-break). Only the winner overlays the real
directory. Per-attempt outcomes land in `attempts.json`; outer metrics
sum all attempts.

## Why

Frozen full-run evidence (275 trials, 31.3% pass): all failures end as
`completed` on shallow work (14.2 vs 9.5 tool calls). A single shot has
no alternative when the first plan is wrong; self-verification alone can
only send the same run back to work.

## Decisions and discrepancies

- Default stays 1 (off) until measured, per the round brief (overrides
  the usual default-ON rule). Clamp N to 8; concurrency is
  `min(N, NumCPU)`.
- Recursive `RunExec` for attempts (own logs/metrics/ATIF per attempt)
  instead of sharing one recorder: keeps trial records valid and the
  index aggregation unchanged (it reads only the outer `excalibur/`
  dir). Sub-runs get fresh IDs and `DisableAttempts` to prevent
  recursion (additive `ExecOptions` field).
- `ExecRun.ProjectDir` added (additive) so `Work` hooks and tests write
  through the isolated copy; file tools already resolve via the
  instructed project directory.
- `telemetry.TaskMetrics.MergeSnapshot` added (additive): sums
  usage/cost/calls, maxes peaks, so best-of-N cost is honest.
- Verified checks approximated as successful tool results
  (`ToolCalls - ToolErrors`); the gate has no persisted per-criterion
  count to read. Cheap model comparison is one 16-token verifier call
  with deterministic fallback; it only fires on exact ties.
- Copy mode skips `.git`/`.excalibur` and symlinks; apply never touches
  `.git`. Deletions propagate (winner-missing files removed). Observed
  edge: small parent step budgets starve (15/3 = 5 steps each, all
  paused) — scale steps with N when measuring.

## Alternatives considered

- Sharing one recorder/ATIF across attempts with interleaved rows:
  rejected (unattributable tool calls, racy sequence numbers, index
  pollution).
- Judge-only selection: rejected (Section 18.2 and P3 require executable
  evidence; the model tie-break is last only).
- Worktree-only isolation: rejected (non-git projects must work; copy
  is the fallback and git failure falls through to copy).

## Verification

- `internal/attempts` unit tests, `internal/cli/attempts_test.go`
  (single/bogus stay single, N=2 selects completed and applies winner,
  eval records both copies), full `go test ./... -race` green,
  cross builds, `check-no-bare-go.sh`.
- Live `excalibur exec --profile eval` (operator credentials,
  `gpt-6-luna` low) on own hello-file and csv-sum tasks with N=2:
  completed, winner applied, `attempts.json` N=2, metrics summed;
  N=3 fix task shows the step-share edge. No Terminal-Bench runs.
