# Harbor adapter

Harbor adapter connecting Excalibur to Terminal-Bench 2.1 and 4.0
(Section 23.2, installed route).

- `excalibur_agent.py`: `ExcaliburAgent` (install/configure/run);
  `install_binary` with sha256 verification, `configure_env` via
  environment only, `fill_agent_context` from
  `/logs/agent/excalibur/metrics.json`.
- `excalibur_agent_test.py`: stdlib unittests with a fake binary.
- `ACP_NOTES.md`: ACP route notes (`excalibur acp --profile eval`
  through Harbor's generic acp agent).
