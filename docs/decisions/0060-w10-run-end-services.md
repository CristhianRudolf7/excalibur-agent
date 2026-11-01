# 0060 W10 run-end services and strict blocked closure

Date: 2026-11-01. Workstream: `w10-run-end-services` (director's W10 design).
Implements the director's three run-end fixes from read-only v9-default
trial records (no Terminal-Bench task contents, tests or solutions used):
the pypi-server service lost at run end, the qemu-alpine-ssh blocked
accepted with 0 refusals after the agent killed QEMU itself, and the
extract-elf blocked claiming a missing "reference solution".

## What changed

- Run-end survival (`internal/tools/terminal.go`, `exec_tools.go`,
  `internal/wireengine/catalog.go`, `internal/cli/exec_engine.go`,
  `exec.go`): `exec background:true` (non-PTY) sessions detach into
  their own session (`setsid`, no `Pdeathsig`, stdin from `/dev/null`,
  stdout+stderr to a log file under `/tmp`) so they survive the
  agent's exit; `exec` foreground commands keep the current
  `Setpgid`+`Pdeathsig=SIGKILL` behaviour. Detached output is tailed
  into the session buffers so polling (`exec_io read/wait_for`) works
  during the run. Surviving processes (pid, command, log path) are
  recorded in `services.json` (run dir, mirrored under `excalibur/`
  in eval) with a `services` event. Switch
  `EXCALIBUR_DETACH_BACKGROUND` (default ON; off restores foreground
  for background too).
- Strict blocked closure (`internal/engine/strictblocked.go`): blocked
  while time remains is accepted only with deterministic external
  evidence (network unreachable/404 after retries, missing file/tool
  plus failed install or dead network, permission denial). The legacy
  empty-history operator case (`need /data/secret.csv from the
  operator`, no prior work) stays accepted. All other unevidenced
  blocked is refused: validation-shaped claims get the three-approaches
  recovery prompt, the qemu-shaped unevidenced claim gets the generic
  external-evidence refusal so it is never accepted with 0 refusals.
  New W10 detectors: `reference solution/mapping/coverage` claims are
  never external (the reference is the hidden verifier); self-caused
  unavailability (history shows `kill`/`signal`/`close` and the reason
  admits the service was stopped/unavailable) is never external.
  Closing mode (no time remains) never accepts blocked: the run must
  end with `finish status=completed` and the best deliverable in place.
  Existing switch `EXCALIBUR_STRICT_BLOCKED` (default ON) gates the
  whole policy; off restores the legacy path-exists-only check.
- Agent teaching (`internal/tools/finish.go`,
  `internal/engine/explicit_finish.go`, `internal/cli/exec_engine.go`):
  the `finish` description carries `keep_running` guidance (required
  services left running, temporary helpers stopped) and states that
  killing a required process is a failure, never a blocked reason.
  Fixed instructions add the run-end services line (detach survival),
  the strict line names missing-reference/self-kill/closing-completed,
  and the closing/long-horizon lines require completed (never blocked)
  with the best deliverable.

## Alternatives considered

- Detaching via double-fork/nohup in the shell command instead of
  `setsid` in the harness: rejected, the model cannot be trusted to
  prefix every server correctly and the harness must guarantee
  survival for background sessions.
- Keeping the W8 operator exception for all non-validation-shaped
  blocked (accept qemu/extract-elf): rejected, it is exactly the hole
  that accepted both premature blockeds with 0/20 refusals.
- Allowing blocked in closing when external evidence exists:
  rejected per the director's closing rule (never blocked in closing;
  leave the best deliverable and complete).

## Consequences

- `TestStrictBlockedAcceptsWhenNoTimeRemains` now expects refusal in
  closing (must complete); this is an intentional departure from W8,
  recorded here per the director's task.
- `internal/engine` validation marks gain W10 shapes (`reference`,
  `coverage`, `timed out`, `stopped`, `unmet`, `criterion`, etc.);
  operator-missing-data with no work stays accepted.
- Connectors untouched: no change to `internal/providers/**`, the
  model gateway wire protocol (`internal/models`, `internal/gateway`),
  `internal/api`, `packages/contracts`, `integrations/**`, MCP/ACP
  servers, or the `bench` Harbor invocation.
- Records: `services.json` is the only new trial artifact; existing
  hashes move only via the prompt text (no frozen-table change).
