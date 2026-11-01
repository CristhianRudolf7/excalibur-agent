# 0070: Non-interactive wall budgets, time footer and spend caps

## Context

A time-only budget for eval runs already existed, but two things made it
misleading. The wall budget fell back to a 3600-second arm default while the
harness enforced the 900-second limit of the common terminal-benchmark agent
timeout. The agent never saw the time it had. Separately, the legacy 25-step
budget closed default and delegate exec runs early: after 23 single-tool
steps, fewer than 10 percent of steps remained, so the next request had no
eligible tools.

## Decision

- All non-interactive exec profiles (default, delegate, eval) use the
  time-only policy by default. `EXCALIBUR_TIME_BUDGET_ONLY=0` restores the
  legacy limits.
- The wall budget is the explicit `--timeout`, else `EXCALIBUR_EVAL_WALL_SECS`,
  else 900 seconds. Harbor operator variables and instruction-stated durations
  no longer set it.
- Tool results carry a time footer and pacing advice at 50, 75 and 90 percent
  (`EXCALIBUR_TIME_FOOTER`).
- Spend caps apply in time-only runs: the USD cost cap and an input-token cap,
  with a closing note at 100 percent and a paused stop at 125 percent
  (`EXCALIBUR_SPEND_CAP`, `EXCALIBUR_INPUT_TOKEN_CAP`).
- Loop detections never block a run. The exact-call guard refuses a repeated
  call for a bounded window (`EXCALIBUR_LOOP_GUARD_V2`).

## Consequences

- A default `excalibur exec` run without `--timeout` now has a 900-second
  budget. Users with longer tasks set `--timeout` or `EXCALIBUR_EVAL_WALL_SECS`.
- Instruction-stated durations are no longer a budget source. A task that
  states a longer duration than the harness enforces gets the enforced limit.
- Eval records keep the same fields; `budgets.json` changes its wall source
  names (`explicit`, `env`, `default`) and gains `input_token_cap` and
  `spend_caps`.
