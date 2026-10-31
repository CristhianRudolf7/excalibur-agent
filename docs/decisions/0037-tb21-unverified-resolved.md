# 0037 TB21 Section 23.2 unverified items resolved

Date: 2026-10-31 against Harbor 0.25.0
(`fdba28fc411e47e8765420a1a063b4918fce3def`). All answers verified in
the installed Harbor package and with live `harbor run` trials.

## 1. Custom installed-agent registration: import path, not built-in

Harbor resolves `--agent` via `AgentFactory.get_agent_class_from_config`:
a value containing `:` (and not an `acp:` shorthand) is treated as an
import path `module.path:ClassName` loaded with `importlib` (see
`harbor/agents/factory.py`, `harbor/utils/import_path.py`). There is no
built-in `excalibur` agent. The bench passes
`-a excalibur_agent:ExcaliburAgent` with `PYTHONPATH` pointing at
`integrations/harbor` (set by `excalibur bench tb21 run`). Verified
live: Harbor imports the class and runs install/run phases.

## 2. Task timeout is NOT communicated to the agent

`Trial._run_agent_phase` enforces the timeout host-side with
`asyncio.wait_for(run(...), timeout=timeout_sec)`; no timeout value,
env var or file is forwarded into the container (verified by grepping
`harbor/trial/trial.py` and the live trial logs). The adapter therefore
uses the Appendix F declared budget: no `--timeout` is passed unless the
operator sets `HARBOR_AGENT_TIMEOUT_SEC`/`EXCALIBUR_TIMEOUT_SEC` via
`--ae`, in which case 90% is applied as the closing budget per Section
23.2. The bench never passes `--timeout-multiplier` (rejected if
present), so task timeouts and resources stay at defaults.

## 3. ATIF version: v1.8 emitted, v1.0-v1.8 accepted

Harbor's `Trajectory` model accepts `ATIF-v1.0` through `ATIF-v1.8`
(default `ATIF-v1.8`; see `harbor/models/trajectories/trajectory.py`).
The harness writes Harbor-compatible `ATIF-v1.8` to
`/logs/agent/trajectory.json` incrementally (minimal valid shape:
`schema_version`, `agent{name,version,model_name}`, `steps[]` with
`step_id/source/message`, `final_metrics`). The richer custom
trajectory stays under `excalibur/trajectory.json` for analysis. Both
live trials validated with Harbor's own `Trajectory.model_validate`.

## 4. AgentContext: new field names, filled from container metrics

Harbor 0.25.0 `AgentContext` uses `n_input_tokens` (incl. cached),
`n_cache_tokens`, `n_output_tokens`, `cost_usd`, `model_usage`
(`harbor/models/agent/context.py`) — not the old
`input_tokens/cache_read/...` names. The adapter fills them during
`run()` by `cat`-ing `/logs/agent/excalibur/metrics.json` inside the
container (host files sync only after the trial) and backfills in
`populate_context_post_run` after Harbor downloads logs. `model_usage`
is left for Harbor's trajectory backfill to avoid Pydantic warnings. A
non-zero `excalibur exec` exit is the agent's honest final state, not a
harness error: the adapter only raises when no records exist, so
verifier failures (reward 0) stay verifier failures.
