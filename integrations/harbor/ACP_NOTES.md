# ACP route notes (Section 23.2)

No dedicated adapter is needed for the ACP route: the same static
binary built by `scripts/bench/build.sh` serves Harbor's generic `acp`
agent.

- Binary: `excalibur acp --profile eval` over stdio, speaking ACP with
  the Harbor `acp` agent process inside the container (binary
  distribution over HTTPS with sha256, or a local command).
- Prompt flow: Harbor sends the task instruction as the session prompt,
  answers permission requests per `permission_mode`, and converts
  session updates into ATIF itself.
- Usage: Harbor reads the tokens and cost the agent reports through
  ACP; the trajectory Harbor reconstructs is less rich than the native
  ATIF from the installed route (`integrations/harbor/excalibur_agent.py`),
  which stays the main path for comparing against other installed
  harnesses (Codex, Claude Code, Muse Code, OpenClaw, Hermes).
- Profile: identical `eval` rules (frozen config, single model, web
  tools gated by the task network signal, benchmark sources blocked,
  incremental ATIF/metrics under `/logs/agent`).
- Trade-off: the ACP route costs less adapter code but inherits the
  ACP connector's usage-mapping conventions, which are not verified to
  be part of the ACP standard; the installed route's `AgentContext`
  filled from `metrics.json` is the auditable reference.
