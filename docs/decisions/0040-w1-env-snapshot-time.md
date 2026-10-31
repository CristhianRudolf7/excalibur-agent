# 0040 W1 environment snapshot and time management

Date: 2026-10-31. Workstream: `environment-and-time` (branch
`agent-quality`).

## 1. What was ambiguous and what was chosen

The task owns environment detection (Section 13.5 parts, in a new
`internal/envsnap` package) and budget/time handling (`internal/engine`
budget hooks only). Connectors, the model gateway wire protocol, the
API, Harbor invocation and the bench command were hard constraints and
were not touched. Within that frame:

1. **Timeout source.** Harbor 0.25.0 never forwards the task timeout
   into the container (verified in 0037: host-side
   `asyncio.wait_for`, no env/var/file). W1 reads the operator
   override `HARBOR_AGENT_TIMEOUT_SEC`/`EXCALIBUR_TIMEOUT_SEC`
   (first parseable wins) and applies the Section 23.2 90% closing
   budget when no explicit `--timeout` is set. Alternative
   considered: defaulting to an Appendix F wall budget without an
   override. Rejected: Appendix F fixes comparison conditions, not a
   numeric budget, and inventing one would silently change eval
   behaviour. With no explicit flag and no override there is still
   no wall budget (as before).
2. **Snapshot placement.** Rendered into L2 (`ProjectContext`,
   stable scope layer) once per run through `envsnap.Cached`, so
   bytes are identical on every step and the L0-L2 cache prefix
   never moves (Section 11.1 rule 1). Alternative: a `clock`-style
   tool or L3/L5 injection. Rejected: a tool costs the model a call
   per run (exactly the waste Meta-Harness removes), and L3/L5
   rewrite or vary every call.
3. **Network probe without side effects.** TCP dial only (1.5 s
   timeout) against the configured provider base-URL host; no HTTP
   request, no paid API, unknown host means `unknown` rather than a
   guess. Dial failure reads as `restricted` with the reason, which
   doubles as the instruction not to retry the network in a loop.
4. **Bounded tree.** 60 top-level entries, depth 2 (subdirectory
   entry counts), heavy directories (`node_modules`, `.git`,
   `target`, ...) collapsed without descent. Full fidelity would
   cost kilobytes of stable prefix; the model lists deeper with
   `exec`/`glob` when it needs to.
5. **Non-interactive defaults.** Added only widely supported,
   safely ignorable variables (`GIT_TERMINAL_PROMPT=0`,
   `DEBIAN_FRONTEND=noninteractive`, `APT_LISTCHANGES_FRONTEND=none`,
   `NEEDRESTART_MODE=a`). `EDITOR` deliberately untouched (would
   break commit flows); `PIP_NO_INPUT` not set (option support
   varies by pip version; the discipline lines teach `--no-input`
   instead).
6. **Builder hook precedence.** `DefaultBuilder.Budget` fills
   `Volatile.BudgetState` only when the snapshot leaves it empty, so
   tests and future callers can pin the line without the hook.

## 3. Live end-to-end evidence (2026-10-31, gpt-6-luna / low)

Binary from this branch (`excalibur exec`, operator credentials
file, temp dirs under `$HOME/w1-live`, no Terminal-Bench runs).
All three tasks `completed`, exit 0.

| Task | Records |
|---|---|
| eval, switches on: write `snapshot-ok` to `result.txt`, report `wc -c` | `logs-on/excalibur/model_calls.jsonl` seq 1: L2 1088 B, L3 719 B, volatile 44 B, 1215 input tokens |
| eval, `EXCALIBUR_ENV_SNAPSHOT=0 EXCALIBUR_TIME_BUDGET=0 EXCALIBUR_NONINTERACTIVE_ENV=0`: same task | `logs-off/...`: L2 176 B (-912 = snapshot), L3 334 B (-385 = discipline lines), volatile 26 B (-18 = budget line), 769 input tokens |
| default profile, `--max-steps 10 --timeout 300`: create and run `hello.py` printing `hello-default-ok` | `hello.py` runs, output `hello-default-ok`; run `completed` |

Both eval runs recovered from `exec unknown target` errors (the
target/schema misuse belongs to the tool-surface workstream) and
completed; the snapshot/interface seam for that fix is
`envsnap.EnvProvider`. Switching the snapshot off costs nothing when
the model already knows the environment, and saves ~450 input tokens
per call here when it does not.

## 4. Verification

- `internal/envsnap` tests: bounded/total pass, truncation, sort
  order, heavy-dir collapse, deterministic capped render, switch
  on/off, once-per-run stability.
- `internal/engine/timebudget_test.go`: 90% math and precedence,
  garbage/negative env, `FormatRemaining`, `BudgetLine` segments,
  hook-vs-snapshot precedence, closing-then-exhausted at the wall.
- `internal/cli/exec_envtime_test.go`: snapshot in L2
  byte-identical across steps, budget line plus discipline lines by
  default, both silent with their switches off, Harbor override
  resolution, `probeHost` units.
- Quality bar: `gofmt -l .`, `go vet ./...`, `staticcheck ./...`,
  `go test ./... -race`, `GOOS=darwin`/`GOOS=windows` builds.
- Live check: `excalibur exec` on small terminal tasks with
  `gpt-6-luna` (effort `low`) confirming the snapshot and budget
  line in the run records. No Terminal-Bench runs were launched.
