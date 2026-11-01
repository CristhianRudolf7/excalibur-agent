# 0050 Strong verification before finishing (W3)

Date: 2026-11-01. Scope: `internal/tasks/strongverify.go` (new),
`internal/tasks/selfverify.go` (config + exercising suggestions),
`internal/engine/verify_hooks.go` (independent verifier call).
Status: implemented.

## What

Automatic triage of 44 failed evaluation trials of the current
agent-quality build showed tool errors down to ~0 while failures are
now dominated by discipline and verification: 13 trials declare
success on wrong results (for example an illegal move written to a
file, or generated artifacts never validated). The W1
self-verification phase (0042) is gameable in two ways: any passing
tool result counts as fresh evidence (`ls -l` after a write passes),
and runs whose artifacts are created by `exec` itself (redirections,
builds, downloads, with no `write_file` marker) pass vacuously.

Two mechanisms, both default ON, both ablatable:

1. Exercise the deliverable (deterministic, in `ApplySelfVerify`):
   after the last change the history must hold passing checks that
   run, import, query, or parse-and-validate each produced artifact
   and each stated output format. Observations and writes never
   count.
2. Independent verifier (one cheap verifier-role call per finish
   attempt, engine closing hook only, when the deterministic checks
   would otherwise pass): a fresh small context (instruction,
   drafts, commands run, evidence lines, diff, final excerpt — never
   executor reasoning) returns the verifier's own small checks plus
   per-draft PASS/FAIL with reasons. FAIL returns the agent to work
   with the reasons, bounded by `GateAttempts`. The prompt bans
   hidden test suites explicitly; errors and unparseable replies
   fall back to the deterministic outcome.

## Why this shape

- Extend, do not duplicate: the exercise layer lives in the same
  `ApplySelfVerify` merge point and the same `SelfVerifyConfig`
  (new `EnableStrong*` fields), the verifier reuses
  `BuildVerifierContext` plus a prompt/parse pair in the same
  package. No second gate, no second attempt counter, no tool
  execution inside the gate (the gate stays tool-free per 0042).
- The verifier is advisory by design ([E][JudgeBench]: a cheap model
  is never the only judge of code): the deterministic exercise
  layer carries the gate, the verifier only adds disagreement
  reasons. It speaks last and only on would-be passes, so failing
  runs spend no extra call.
- Cost is bounded and minimal: at most one verifier call per
  passing finish attempt, none when there are no drafts (nothing
  checkable) or when the daemon path already requires the Section
  8.7 verdict (the orchestrator spawn owns it; no double spend).
- Verifier disagreement maps to the existing honest `blocked` state
  (the pendings carry the exact `verifier disagreement` phrase
  `HonestState` already matches), per the Section 7.3/8.7 rule that
  after two rounds both positions stand.

## Alternatives considered

- Harness re-runs every check before completing. Rejected again
  (see 0042): effect and idempotency handling inside the gate plus
  double cost on every finish. The verifier reviews evidence and
  writes the checks it would run; actual re-execution stays the
  agent's job, forced by the exercise layer.
- Requiring recorded criteria rows on the embedded path. Rejected
  (see 0042): drafts plus history evidence give the same
  enforcement without new state.
- Fail-closed verifier (block on gibberish). Rejected: a flaky model
  reply would convert passing runs into honest-stops on format
  alone. Unparseable replies fall back to deterministic; only an
  explicit FAIL blocks.
- Bare-substring change detection. Rejected after a live false
  positive: a bare `>` marker mistook the `>` in a `printf '<%s>'`
  validation format for a redirection, so a correct
  run-with-assertion was blocked. Change detection now requires a
  command boundary plus a target (`redirectRe`); null sinks and
  stderr merges are stripped first.

## Live verification (2026-11-01, own tiny tasks, never Terminal-Bench)

`excalibur exec --profile eval` with the operator credentials file
(`gpt-6-luna`, low effort), three tasks in fresh temp dirs:
`hello.py` printing a fixed string, `numbers.csv` + `sum.json`
with JSON parse validation, `run.sh` with a run-and-assert check.
All `completed` exit 0; records show exercising checks after the
last change (`python3`, `json.tool` parse, `bash` run with `test`
assertion), gate attempt 1 passed, and verifier-role calls present
in `model_calls.jsonl`. The third task first exposed the `>`
false positive above (ended `completed_with_pending` on a correct
run) and passes after the fix.

## Ablation

All switches in `tasks.SelfVerifyConfig`, environment-overridable,
defaults ON; listed in `docs/development/agent-quality.md` (W3
section). Key names: `EXCALIBUR_STRONG_VERIFY` (master),
`EXCALIBUR_STRONG_VERIFY_EXERCISE`, `EXCALIBUR_STRONG_VERIFY_VERIFIER`.

## Coordination

- Engine loop prompts: untouched (owned by another task); retry
  notes keep the `self-verify before finishing: ...` shape.
- Two `internal/cli` embedded scripts were already updated to the
  W1 contract; the new layer only changes which checks satisfy it
  (existing `verify_test.go` passing/refine cases updated for the
  extra verifier call — still owned by this workstream).
- Sibling workstreams: no prompt, tool-surface, or record-shape
  changes; pendings text stays greppable (`not exercised`,
  `independent verifier disagreement`) for the measurement/triage
  loop.
