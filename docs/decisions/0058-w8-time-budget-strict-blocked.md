# 0058 W8 time-based budgets and strict blocked policy

Date: 2026-11-01. Workstream: `budget-and-blocked` (director's W8 design, urgent).
Applies the director's hypotheses from the v6-default (74 trials, 40.5% pass:
21 blocked after one failed validation, 14 paused on the step cap) and
muse-spark-v6 (13/13 paused `budget exhausted` after ~2 minutes and ~200k
tokens on 15-minute–8-hour tasks) records, plus the before/after analysis in
the task brief. No Terminal-Bench task contents, tests or solutions were used;
only general harness behaviour from read-only trial records.

## What changed

- Wall-clock-only binding in non-interactive profiles when
  `EXCALIBUR_TIME_BUDGET_ONLY=on` (default ON in eval, OFF elsewhere;
  forced OFF when `EXCALIBUR_TIME_BUDGET=0` so the silent baseline stays
  silent). Effective budgets resolved per run in `internal/cli`
  (`EffectiveBudgetsFor`): explicit `--timeout` first, else the Harbor
  90% override (`HARBOR_AGENT_TIMEOUT_SEC`/`EXCALIBUR_TIMEOUT_SEC`), else
  the configured arm fallback (`EXCALIBUR_EVAL_WALL_SECS` aliases
  `EXCALIBUR_ARM_TIMEOUT_SEC`/`EXCALIBUR_ARM_WALL_SECS`, default 3600s).
  Safety caps that cannot bind before the wall: steps 2000
  (`engine.SafetyMaxSteps`), cost `EXCALIBUR_COST_CAP_USD` (default 20),
  tokens/tool-calls uncapped; per-call input stays bounded by the model's
  max input via the existing full-window context control.
- Engine (`internal/engine`): `Profile.TimeBudgetOnly` marks the mode.
  Only the wall clock pauses (`timeOnlyStatus`); legacy step/cost/token
  exhaustion forces closing with a one-time safety note instead of
  pausing. Closing (<10% wall left) injects the one-time
  `TimeOnlyClosingNote` (leave the best deliverable, call `finish`);
  the honest pause names `time`. Fixed instructions gain closing and
  blocked-policy discipline lines when the switches are on.
- Strict blocked (`EXCALIBUR_STRICT_BLOCKED`, default ON):
  `finish status=blocked` while time remains is refused unless the
  reason plus the agent's own tool history shows deterministic external
  impossibility (network unreachable/404 after retries, missing
  file/tool plus a failed install or dead network, permission denial).
  Validation-failure language with prior tool work and no external
  evidence is refused with a structured recovery prompt (failing
  check/output, three materially different approaches, pick one,
  continue). Operator-missing-data claims without validation language
  stay accepted for legacy compatibility. Refusals count in
  `FinishRefusals` plus `StrictBlockedRefusals`, surfaced in
  `explicit_finish.json`.
- Records: `budgets.json` (mode, wall/steps/cost, sources) next to the
  other trial artifacts and mirrored under `excalibur/` in eval;
  `profile.json` carries `wall_secs`, `wall_source`, `max_steps`,
  `cost_cap_usd`, `cost_source`, `time_budget_only`, `strict_blocked`
  so `profile.sha256` moves with the budgets; `explicit_finish.json`
  pins `time_budget_only`, `strict_blocked`, `strict_blocked_refusals`,
  closing/safety notes. Attempts fan-out gives every attempt the full
  safety caps with the shared wall clock (legacy split preserved only
  when `EXCALIBUR_ATTEMPTS_FULL_BUDGET=0` is explicitly set).

## Alternatives considered

- Removing step/cost limits entirely (uncapped) instead of large safety
  caps: rejected, the task explicitly asks for safety caps sized not to
  bind (2000 steps, $20) so a runaway still has a backstop.
- Strict blocked requiring positive external evidence for every blocked
  claim (refusing operator-missing-data without network/permission
  marks): rejected as over-strict; the existing
  `TestExplicitFinishBlocked` contract (`need /data/secret.csv from the
  operator`) carries no failure language and must stay accepted. The
  policy therefore refuses only validation-failure-shaped claims while
  time remains.
- Wall fallback of 0 (uncapped) when Harbor is unknown: rejected; eval
  would then have no binding budget and the "never paused except on the
  hard wall" guarantee would be vacuous. The 3600s arm default is
  documented in `budgets.json` (`arm_default`) for the ablation.

## Consequences

- Eval profile hash moves with budgets via `profile.sha256` (the frozen
  `EvalProfileConfig.Hash()` itself is untouched; the arm hash already
  captures the switches). Reference comparisons must compare
  `budgets.json` alongside the frozen hash.
- Existing tests that relied on the 25-step default or quick pause now
  set `EXCALIBUR_TIME_BUDGET_ONLY=0` (or small `MaxSteps`) to keep the
  legacy path; new `timebudgetonly`, `strictblocked` and `budgetsonly`
  suites pin the W8 contract.
- Connectors untouched: no change to `internal/providers/**`, the model
  gateway wire protocol, `internal/gateway`, `internal/api`,
  `packages/contracts`, `integrations/**`, MCP/ACP servers, or the
  `bench` Harbor invocation.
