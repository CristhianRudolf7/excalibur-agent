# Exec, delegation and benchmarks (H4 closure + H8 delegation)

Contract and backend tests of the end-to-end wiring: `excalibur exec`
and exit codes (6.5), deployment profiles (3.3), the frozen `eval`
profile with hashes and deterministic control (23.2), `excalibur eval
smoke|report`, metrics/ATIF into `/logs/agent`, the
`default`/`delegate`/`eval` profiles with the ten `excalibur task`
orders (3.6), the `excalibur mcp` delegation server with its 7 tools
and progress notifications, `excalibur mcp install`, the
`integrations/clients/` templates with the `excalibur-delegate`
skill, and `excalibur acp`.

All logic lives in `internal/cli` (thin layers over the shared
core); `cmd/excalibur` only parses flags, streams output and maps
stable exit codes. No agent or provider logic lives in either place.
See `docs/decisions/0025-exec-delegation-no-new-deps.md` for the
scoping choices (stdlib only, embedded deterministic step, explicit
question protocol, file-based adoption, ACP subset).

## Contract

- **Profiles (3.3, 3.6)** (`profile.go`): `default` (full assistant,
  persistent user state), `delegate` (project-scoped memory only,
  `--no-memory` disables it; persistent in `<project>/.excalibur/`
  or the daemon when active), `eval` (no memory, ephemeral state
  with hashes). `--ephemeral` is rejected with `default`.
  `DataDir` lays out eval state as `<logsDir>/excalibur`, delegate
  state as `<project>/.excalibur`; `ResolveLogsDir` prefers
  `--logs-dir`, then `EXCALIBUR_LOGS`, then `/logs/agent` when the
  Harbor mount exists, else a temp dir. `EvalDataReady` aborts on a
  non-empty data dir without `--profile-override`.
- **Exec (6.5)** (`exec.go`): level-1 root run, no master, no
  conversation, request file from the petition. Never asks: the
  assume branch applies; an empty request ends `blocked` (code 3)
  naming the missing datum. Final states map to 0/2/3/4/5/130 with 1
  for harness errors (`exit.go`). `--output-schema` validates the
  final result against the JSON-schema subset (`schema.go`: objects,
  required members, nested objects/arrays, scalars, enums).
  `--timeout` expiry pauses (4); canceled context cancels (130); an
  explicitly exhausted budget pauses (4). Every run writes the
  Appendix F records: `profile.json` + hash, `trajectory.json`
  (ATIF v1.8, incremental and validated), `events.jsonl` (streamed
  with `--json`), `metrics.json` (canonical Harbor path
  `/logs/agent/excalibur/metrics.json` under eval) and
  `result.json`. `EffectiveTools`/`ToolsHash` pin the per-profile
  tool surface; `EvalToolAllowed` gates the 23.2 table (assistant
  tools hidden, `task_db` project ops rejected, task-internal
  planning enabled).
- **Tasks (3.6)** (`taskstore.go`, `tasksvc.go`, `worktree.go`):
  file-backed records under `<base>/tasks/<task_id>/` with short
  codes (`T1`…), JSONL logs and the exec artifacts in the same
  directory, so status/wait/result/logs work with no daemon and the
  daemon adopts the same layout later (17.3). `start` returns
  immediately and spawns a detached `__run-task` child (inline in
  tests); `wait` polls with the exec exit codes; `result` serves
  the 7.6 delivery report; `reply` answers a parked `{{ask: …}}`
  question and resumes (consuming the marker) or records a
  directive; `cancel`/`pause`/`resume` follow 15.8 (kill the child,
  reject terminal/double control). Worktrees default on inside git
  repos: the task runs on `excalibur/<task>` and the result reports
  the branch and diff while the delegating tree stays clean.
- **MCP server (3.6)** (`mcp.go`): stdio JSON-RPC 2.0 speaking MCP
  `2026-07-28` (initialize, tools/list, tools/call, ping). Seven
  tools with short descriptions (P8): `excalibur_delegate` (with
  `wait:true` for the final result), `excalibur_status`,
  `excalibur_wait` (max 600s per call), `excalibur_result`,
  `excalibur_reply`, `excalibur_cancel`, `excalibur_list`.
  `wait`/`delegate+wait` emit `notifications/progress` per poll so
  the delegating agent never polls in a loop. Domain failures are
  tool `isError` results; protocol errors stay JSON-RPC errors.
- **Install (3.6)** (`mcinstall.go`, `integrations/clients/`):
  fragments for `codex` (TOML section), `claude-code` (JSON, plus
  the `claude mcp add` equivalent), `opencode` (type `local`,
  merged into existing config), `muse-code` (JSON). `--print`
  writes stdout; otherwise the client file is created/merged
  (never duplicated); `--skill` installs `excalibur-delegate`.
- **Skill**: `integrations/clients/excalibur-delegate/SKILL.md`
  (Agent Skills format: when to delegate, CLI and MCP flows, exit
  codes, result contents), pinned equal to the embedded copy.
- **ACP (3.5)** (`acp.go`): stdio adapter with initialize,
  session/new, session/prompt (runs the instruction under the
  configured profile, default `eval`) and session/cancel. This is
  the route Harbor's generic `acp` agent uses.
- **Eval package (23.2, 23.7)**: frozen `eval@1` profile, stats,
  replay, report, smoke, suites and Harbor adapter predate this
  milestone and are unchanged; the CLI exercises them (smoke via
  `eval smoke`, report via `eval report`, metrics into
  `AgentContext`).

## Backend tests

- `internal/cli`:
  - `profile_test.go`: profile parsing, ephemeral rules, data-dir
    layout, logs resolution, eval data-dir abort/override, tool
    surface per profile and hash stability, `EvalToolAllowed`
    matrix (23.2 table rows).
  - `exec_test.go`: full 6.5 exit-code table through the CLI entry
    point; empty request → blocked/3 without asking; output-schema
    pass and harness-error mismatch (6.7 row); Appendix F
    artifacts with eval ATIF path, profile hash and metrics
    feeding `AgentContext`; eval guards (dirty dir abort,
    recorded override, trial isolation); canceled context → 130;
    exhausted budget → paused/4; ask marker assumed and recorded;
    mid-work failure leaves valid partial ATIF/events/metrics
    (23.8 cut-trial row); expired deadline pauses; single-model
    identity in metrics (23.8 row).
  - `tasksvc_test.go`: start/status/wait/result/list/logs round
    trip with task-dir artifacts; question → wait returns it →
    reply resumes to completion (3.7 row); `never_ask` assumes and
    records; pause/resume/cancel with double-control and unknown
    task rejection; short codes; worktree branch with a clean
    delegating tree; real child kill + file-based adoption (3.7
    row).
  - `mcp_test.go`: initialize protocol version, 7-tool listing,
    unknown-tool error, delegate/wait/result/status/list flow
    (3.7 row), question → wait → reply and `never_ask`
    equivalence over MCP.
  - `mcinstall_test.go`: `--print` for all four clients, JSON
    validity, TOML section, template marker agreement, write +
    merge without duplication, skill install pinned to the
    shipped `SKILL.md`.
  - `acp_test.go`: initialize/session/prompt round trip with
    `end_turn` content and exit code; unknown session/method
    errors; cancel ack; three-route agreement (installed exec,
    ACP, background task) on one instruction (23.8 row).
- `cmd/excalibur`: help/unknown routing, implemented commands
  fail clean (never 69), exec usage errors, eval smoke/report
  commands.
- `scripts/bench/build.sh` produces the static musl binaries with
  sha256; `eval smoke` passes locally before any paid trial.
