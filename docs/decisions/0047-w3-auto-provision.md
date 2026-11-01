# 0047 Automatic dependency provisioning (W3)

Date: 2026-11-01. Workstream: `w3-auto-provision` (branch `auto-provision`).
Packages: `internal/tools` (`autoprovision.go`, `exec_tools.go` wiring).
Connectors untouched: `internal/providers/**`, `internal/models`,
`internal/gateway`, `internal/api`, `packages/contracts`,
`integrations/**`, MCP/ACP servers, `bench` Harbor invocation.

## What

`exec` results now carry a `provision:` report when the tool output
matches a missing dependency. Detection is purely textual over the
recorded output (general engineering knowledge, never benchmark
answers) across shell (`command not found`, `: not found`), Python
(`No module named`), Node (`Cannot find module`, bare specifiers
only), Ruby (`cannot load such file`), R (`there is no package
called`), Perl (`Can't locate ... in @INC`), Go (`cannot find
package`), Rust (`can't find crate`, `no matching package`,
`unresolved import`), Java (`package ... does not exist`) and shared
libraries (`.so: cannot open shared object file`). Standard-library
imports (Python `os`/`json`/..., Node `fs`/`path`/..., R `base`/...,
Go stdlib without a dot) and relative paths never provision; install
commands themselves never re-trigger (loop guard).

The helper maps each hit to install commands for the detected
managers: apt (with `DEBIAN_FRONTEND=noninteractive`,
`-y --no-install-recommends`, update-once caching), pip/uv with the
right interpreter (`python3 -m pip` preferred, `uv pip --python`
when uv exists), npm/yarn/pnpm, `gem --no-document`, R
`install.packages`, `cpanm`/`cpan`, `go get`, `cargo add`. Curated
maps cover `ps`->`procps`, `ss`/`ip`->`iproute2`, `cv2`->`
opencv-python-headless`, `chess`->`chess`, `PIL`->`Pillow`,
`sklearn`->`scikit-learn`, `liblapack.so.3`->`liblapack3`, and ~60
other common binaries, imports and shared libs; unknown commands
fall back to the binary name, unknown pip names to the sanitized
import top-level. Every plan carries a `verify` step (`command -v`,
`python3 -c "import ..."`, `npm ls`, ...). Plans never contain
`upgrade`, `dist-upgrade`, `remove`, `purge`, `autoremove`,
`uninstall` or `--upgrade` (enforced by `isForbiddenProvisionCommand`
and pinned by tests).

Policy switch `EXCALIBUR_AUTOPROVISION=auto|suggest|off` (default
`auto`): `auto` runs the install when a TCP probe to `8.8.8.8:53` /
`1.1.1.1:53` succeeds and always reports the outcome; `suggest`
reports the commands without running; `off` restores the
pre-provisioning bytes (hint only). Java coordinates are always
suggest-only (they need a build-file edit).

## Why

Triage of 44 failed evaluation trials (`triage-all-on.md`, read-only
general behaviour): 12 fail as `environment/tooling-missing`
(`python3: command not found`, `file: command not found`,
`No module named 'chess'/'cv2'`, `liblapack.so.3`, `ps`/`ss: command
not found`) while a manager and network were available, and the agent
never installed the missing piece. Tool errors had already fallen to
~0, so the remaining failures are discipline and verification gaps;
this workstream removes the tooling-missing class at the harness
level instead of spending model steps on it.

## Alternatives considered

- Suggest-only always (no auto-install): rejected as default because
  small models often ignore hints and re-run the failing command;
  kept as `suggest` for ablation.
- System `apt-get upgrade` alongside installs to "fix" broken
  mirrors: rejected; the policy is install-only to avoid breaking
  the container, and the update runs at most once per process.
- `pip install` without `--break-system-packages`: the first live
  check failed on Ubuntu 24.04 PEP 668
  (`externally-managed-environment`); the helper now passes
  `--break-system-packages` for pip (uv needs none). The agent's
  venv workaround still works when the flag is unavailable.
- Auto-installing Java/Maven coordinates via `mvn install`: rejected;
  coordinates need a `pom.xml`/`build.gradle` edit, so Java stays
  suggest-only with the exact edit hint.
- Re-running the original command automatically after a successful
  install: rejected; the report tells the model to re-run and verify,
  keeping one tool effect per call and the records auditable
  (`provision:` lines in `tool_calls.jsonl`).

## Verification

- `internal/tools/autoprovision_test.go`: recorded-output detection
  table (triage quotes), curated-map and flag checks
  (`--no-install-recommends`, `--break-system-packages`,
  `--no-document`), update-once, never-upgrade/remove, stdlib and
  loop guards, policy matrix (off/suggest/auto offline/auto
  success/auto failure), mode parsing, exec integration
  (suggest appends, off is silent), real shell runner check.
- `go test -race ./internal/tools/` green; full `go test ./... -race`
  green except one transient `internal/gateway` websocket timeout
  under parallel load with the evaluation running (passes isolated
  5/5 and with `-p 1`; unrelated to this change, no gateway code
  touched).
- Live end-to-end (own tasks, never Terminal-Bench, `gpt-6-luna`
  low): `import chess` auto-installs via pip (`--break-system-packages`,
  exit 0, `1.11.2` on re-run, 2 tool calls); missing binary reports
  apt plan, update ok, install fails cleanly with manual guidance;
  `hello.txt` shows no false positive. Records carry `provision:`
  lines in `tool_calls.jsonl`.
