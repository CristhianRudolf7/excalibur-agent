# Harbor adapter

Harbor adapter connecting Excalibur to Terminal-Bench 2.1 and 4.0
(Section 23.2, installed route). Pinned Harbor 0.25.0
(`fdba28fc411e47e8765420a1a063b4918fce3def`).

Operational use only; the contract lives in Section 23.2 and
`docs/decisions/0036-tb21-harbor-pinning.md` /
`0037-tb21-unverified-resolved.md`.

- `excalibur_agent.py`: `ExcaliburAgent` (async Harbor 0.25.0 API:
  `install` uploads the host static binary and verifies sha256 inside
  the container; `run` executes
  `excalibur exec --profile eval --json --logs-dir /logs/agent` and
  fills `AgentContext` (`n_input_tokens`, `n_cache_tokens`,
  `n_output_tokens`, `cost_usd`) by reading
  `/logs/agent/excalibur/metrics.json` inside the container, with a
  host-side backfill in `populate_context_post_run`). Registration is
  by import path: `PYTHONPATH=integrations/harbor harbor run -a
  excalibur_agent:ExcaliburAgent ...`. Credentials travel only via
  environment variables (`--ae EXCALIBUR_*`); timeouts are NOT sent by
  Harbor, so the adapter applies a 90% closing budget only when
  `HARBOR_AGENT_TIMEOUT_SEC`/`EXCALIBUR_TIMEOUT_SEC` is set. Non-zero
  exec exits are honest agent states (verifier decides), not harness
  errors.
- `excalibur_agent_test.py`: stdlib unittests with a fake binary
  (`EXCALIBUR_TEST_NO_HARBOR=1` forces the fallback path).
- `ACP_NOTES.md`: ACP route notes (`excalibur acp --profile eval`
  through Harbor's generic acp agent).
