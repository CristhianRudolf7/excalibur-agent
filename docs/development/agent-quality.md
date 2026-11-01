# Agent quality: harness levers for small models

This file records the agent-quality workstreams: every new mechanism
ships with a config switch (default ON when it helps) so it can be
measured on and off. Each section names its owner workstream, its
switches, and its contract/backend tests. Evaluation records are
standard evaluation and auditing (Section 23.2, Appendix F).

## W1: agent loop and prompting for small models (Sections 7.3, 11.1, 18.3, 19)

Owner: agent loop workstream. Packages: `internal/engine` (step loop,
`quality.go`), `internal/ctxcompiler` (`method.go`, prompt layers).
The model gateway wire protocol, the providers, the tool surface and
the bench invocation are not touched; sibling workstreams plug
precise behaviour through the hooks below.

### Problem evidence (read-only trial records, current version)

From `analyze.py` over the frozen full run (57 trials, 26.3% pass):

- ALL failures end as `completed`: the agent declares success while
  the result is wrong. Average 14.0 tool calls on failures (8.9 on
  passes): shallow work that stops early.
- 17% of `exec` calls fail because the model fills a `target`
  parameter (`container`, `dev`, `host`, `/app`, ...) that does not
  exist (`unknown target`).
- `load_skill`, `search_skills`, `subagent`, `task_db`,
  `ack_directive` answer `not wired` in exec/eval; `search_tools`
  fails with `query is required`; the model keeps calling them.
- The embedded run carries almost no working method: two fixed
  instructions only, no family adaptation, terse loop notes, no
  pinned facts, and an unwired gate that passes trivially.

### Mechanisms (all default ON)

1. Working method in L0 (`ctxcompiler.WorkingMethod`, rendered by
   `renderCore`): understand, explore, plan, implement in small
   steps, test, verify, finish; thoroughness and no-early-stop
   rules; error-recovery and tool-parameter-error rules.
2. Model-family adaptation (`DetectFamily`, `FamilyNote`,
   selected by `Snapshot.ModelID`, default GPT): prompt/tool-format
   variants per family (GPT, Claude, Gemini, DeepSeek, Muse, Qwen);
   unknown ids use the GPT default.
3. Actionable loop nudges (`engine.nudgeFor`, `escalateNote`):
   every loop detection (Section 18.3 rules) renders the pattern
   plus a concrete recovery strategy; stage-2 escalations add a
   strategy-switch note. Legacy prefixes (`loop:`, `expert:`,
   `loop escalated:`) are unchanged for telemetry.
4. Context engineering: pinned working-notes block rendered in L3
   (`TaskView.WorkingNotes`, most-recent-last, capped); the engine
   pins failures and touched file paths per tool outcome
   (`factForToolCall`, deduplicated, capped). L0/L1/L2 stay stable
   inside a run, so the input cache prefix never churns.
5. Finish guard (`engine.guardFinish`): when the model answers
   without tools while no gate is wired and tools were attempted
   without a single success, the run is sent back with a
   verification reminder (at most `MaxGuardTrips` times). Runs that
   never attempted a tool, runs with a success, and any wired gate,
   gate input, or hook stand the guard down.

### Hooks for sibling workstreams

- `engine.Deps.FinishGuard` (`FinishGuardFunc`): the verification
  workstream plugs real criteria here without touching the loop.
- `engine.Deps.ErrorHint` (`ErrorHintFunc`): the tool-surface
  workstream plugs precise per-tool recovery hints (at most one
  volatile note per step); nil disables.
- `engine.DefaultBuilder.ModelID`: whoever resolves the model for
  the run sets the family id (empty means the GPT default);
  reasoning effort handling is untouched.

### Switches

| Switch | Type | Default | Off restores |
|---|---|---|---|
| `ctxcompiler.Config.MethodPrompt` | field | true | legacy L0 |
| `ctxcompiler.Config.FamilyAdapt` | field | true | family-independent L0 |
| `ctxcompiler.Config.WorkingNotesMax` | field (0 disables) | 12 | no notes section |
| `engine.QualityConfig.WorkingMethod` | field / `EXCALIBUR_Q_METHOD` | on | legacy L0 |
| `engine.QualityConfig.FamilyAdapt` | field / `EXCALIBUR_Q_FAMILY` | on | family-independent L0 |
| `engine.QualityConfig.WorkingNotes` | field / `EXCALIBUR_Q_NOTES` | on | no pinning/rendering |
| `engine.QualityConfig.LoopNudges` | field / `EXCALIBUR_Q_NUDGES` | on | legacy terse notes, no hints |
| `engine.QualityConfig.FinishGuard` | field / `EXCALIBUR_Q_GUARD` | on | no guard trips |

`EXCALIBUR_Q_*` accepts `1/true/yes/on` and `0/false/no/off`;
unset keeps the configured value. Caps: `MaxGuardTrips` (2),
`MaxNotes` (24).

### Research sources

- LangChain, "Improving Deep Agents with harness engineering"
  (Feb 2026; fetched full text): 52.8 to 66.5 on TB2 with the model
  fixed. Levers adopted: system-prompt working method
  (plan/discovery, build, verify, fix), pre-completion verification
  checklist (our finish guard), environment onboarding context, loop
  detection middleware with reconsider nudges, model-specific
  prompt tailoring.
- Meta-Harness, arXiv 2603.28052 (fetched abstract): optimizing the
  harness code (what to store, retrieve, present) beats hand design
  and transfers across models; motivates keeping the context small
  and the facts pinned rather than growing the prompt.
- Agentic Harness Engineering, arXiv 2604.25850 (cited, not
  fetched): same direction; no claim beyond the shared premise.
- Open-source harnesses (general engineering knowledge, no task
  content used): SWE-agent's short system prompt with explicit test
  running, mini-swe-agent's minimal loop, OpenHands loop detection
  (repeated calls+errors), Codex/OpenCode `tool_choice: none`
  closing mode and JSON repair, Aider's architect/editor split
  behind the expert-escalation pattern.
- The architecture document's own evidence refs ([E][Bucles],
  [E][Caching], [E][SkillsBench], [E][ComplexityTrap]) for
  thresholds kept as specified (R1-R3 counts, masking batches,
  prefix stability).

### Contract and backend tests

- `internal/ctxcompiler/method_test.go`: family detection table,
  method content, distinct per-family notes, unknown-to-GPT
  default, adapt on/off byte behaviour, notes rendering and cap,
  empty-notes byte stability, switch-off legacy bytes.
- `internal/engine/quality_test.go`: defaults, env overlay, nudge
  text per rule with legacy fallback, on-detection nudge and
  pinning, thin-work guard trips and stand-downs (wired gate,
  successful work), guard hook override, notes extraction and hint
  injection, fact helpers (dedupe, cap, path parsing).
- `internal/engine/builder_quality_test.go`: method/family/notes
  rendering through `DefaultBuilder`, ablation-off legacy bytes,
  snapshot `ModelID` precedence, reasoning effort passthrough.
- Existing suites unchanged and green: engine lifecycle/e2e/crash,
  ctxcompiler determinism/cache/compaction/masking
  (`go test -race ./internal/engine/ ./internal/ctxcompiler/`).

## W1: Environment snapshot and time management

Owner: `environment-and-time` workstream (branch `agent-quality`).

### W1.1 Proactive environment snapshot (Section 13.5)

New package `internal/envsnap` (stdlib plus `x/sys` and
`internal/safego` only): at task start the harness collects OS and
distro (`/etc/os-release`), architecture, CPU count, total RAM
(`/proc/meminfo`), free disk on the workdir filesystem, usable shells
and default (bash preferred), package managers, language and compiler
versions (`--version` probes with 800 ms timeouts, fanned out and
bounded by a 4 s total budget), PATH-checked tools, a TCP-dial-only
network reachability verdict (no payload, no paid API), a bounded
workdir tree summary (60 entries, depth 2, heavy directories such as
`node_modules`/`.git`/`target` collapsed to entry counts), listening
local TCP ports from `/proc/net/tcp*` (Linux; stubbed elsewhere), and
renders it once into the stable L2 scope layer
(`projectContext`) via a once-per-run `Cached` provider, so the bytes
are identical on every step and never break the L0-L2 cache prefix
(Section 11.1 rule 1: the collection timestamp is deliberately not
rendered). Contract: collection never fails the run; every probe
degrades to absence.

The tool-surface workstream owns the `exec` tool itself; it programs
against the `envsnap.EnvProvider` interface (`Snapshot`/`RenderCompact`)
so both sides share one snapshot shape without an import cycle.

Backend tests: `internal/envsnap` (bounded/total collection, tree
cap, sorting, heavy-dir collapse, deterministic capped render,
switch, once-per-run stability, version shortening);
`internal/cli/exec_envtime_test.go` (snapshot rides L2 byte-identical
across steps; switch-off restores the old prompt).

### W1.2 Time awareness and closing mode (Sections 7.3, 18.4, 23.2)

Harbor never forwards the task timeout into the container (verified in
`docs/decisions/0037`, Harbor 0.25.0 kills host-side), so the harness
reads the operator override `HARBOR_AGENT_TIMEOUT_SEC` /
`EXCALIBUR_TIMEOUT_SEC` and applies the Section 23.2 90% closing
budget when no explicit `--timeout` is set
(`engine.ResolveMaxSeconds`, `ClosingBudgetShare = 0.9`). The result
becomes the engine wall budget (`Tracker.Limit.MaxSeconds`), so the
existing tracker trips closing mode below 10% remainder and pauses
with the `budget` motive at zero; an operator-derived budget without
an explicit flag additionally wraps the run context so the wall edge
holds even mid-step. Every model call carries the compact remainder
in L5 (`Volatile.BudgetState` via the new `DefaultBuilder.Budget`
hook, e.g. `steps 12/25, tools 8, time 42m left of 1h0m`), and the
run's fixed instructions teach the model to spend it well: start
closing when little remains, run long builds/servers/watchers in
background terminal sessions polled with `exec_io wait_for`, never
busy-poll, and never wait on interactive prompts (non-interactive
flags). The builder hook never overrides a snapshot-carried
`BudgetState`, so callers can pin the line in tests.

Backend tests: `internal/engine/timebudget_test.go` (override
precedence and 90% math, garbage/negative handling, compact
formatting, line segments, hook vs snapshot precedence, closing then
exhausted at the wall budget); `internal/cli/exec_envtime_test.go`
(line and discipline text present by default, silent with the switch
off).

### W1.3 Non-interactive process environment (Section 13.5)

Tool processes already forced `PAGER=cat`, `GIT_PAGER=cat`; the
defaults now also set `GIT_TERMINAL_PROMPT=0`,
`DEBIAN_FRONTEND=noninteractive`, `APT_LISTCHANGES_FRONTEND=none`
and `NEEDRESTART_MODE=a` in both pipe and PTY sessions, so package
installs and git operations cannot hang on a prompt. `EDITOR` is
deliberately left alone (overriding it breaks commit flows that need
an editor). Backend test: `TestPipeEnvAndShellDetection`.

### Ablation switches (all default ON)

| Switch | Mechanism | Off behaviour |
|---|---|---|
| `EXCALIBUR_ENV_SNAPSHOT=0` | W1.1 snapshot injection | Old prompt, model rediscovers the environment with tools |
| `EXCALIBUR_ENV_SNAPSHOT_TIMEOUT=<dur>` | W1.1 collection budget (default 4s) | Shorter/longer pass, same shape |
| `EXCALIBUR_TIME_BUDGET=0` | W1.2 override plumbing, engine wall budget, L5 remainder line, discipline instructions | Pre-existing behaviour: context deadline only, no budget line |
| `EXCALIBUR_NONINTERACTIVE_ENV=0` | W1.3 extra env defaults | Pager-only env (`PAGER`, `GIT_PAGER`, `TERM`) as before |

Measure each switch off against on with the same model, effort and
task set (Appendix F conditions); keep a mechanism only if it does
not regress the sample.

### Research sources

Fetched live during W1 (2026-10-31):

- LangChain, "Improving Deep Agents with harness engineering"
  (`blog.langchain.com`, 2026-02-17): 52.8 to 66.5 on TB2 by harness
  changes only. Adopted: proactive environment context injection
  (`LocalContextMiddleware`: cwd mapping, tool discovery) so the
  model stops searching; time-budget warnings; self-verification
  prompting; loop-detection notes. The build/verify loop and
  pre-completion checklist belong to the verification workstream, not
  W1.
- SWE-agent/mini-swe-agent (`github.com/SWE-agent/mini-swe-agent`,
  MIT): minimal bash-only agent, stateless subprocess actions,
  linear history. Informs keeping the snapshot a plain-text L2
  block rather than a new tool, and the background-session guidance
  for long commands.
- OpenAI Codex CLI (`github.com/openai/codex`, Apache-2.0): yield
  windows (250 ms-30 s), group-kill semantics, head/tail output
  truncation, `toolChoice: none` closing without breaking the cache
  prefix. Matches Sections 13.4/13.5 values already in the tree;
  W1 keeps them and only adds the time-budget line that makes
  closing mode actually reachable.

Cited from the architecture document's own reference base (Section
28, not re-fetched in W1):

- Meta-Harness (arXiv 2603.28052): proactive environment snapshot
  removes wasted actions, larger gains on weaker models.
- Agentic Harness Engineering (arXiv 2604.25850).
- Terminus 2 (Harbor reference harness): single-tool tmux loop;
  the Section 23.2 analysis notes its missing-binary failure mode
  (24.1% of command failures), which W1.1 attacks directly.
- OpenHands, SWE-agent, Codex CLI, OpenCode, Aider: bounded
  viewers, summarized search, JSON repair and loop/stopping
  criteria, per the ablation notes in Sections 13.4, 13.6 and 18.3.

Documentation describes standard evaluation and auditing records
(Section 23.2, Appendix F); nothing here tunes the agent against
benchmark failures or derives prompts from task contents.

## Measurement tooling (w1-measurement)

Owner: measurement workstream. Packages: `internal/eval/**` (except
the Harbor invocation), the `bench` command's report side
(`report`, `compare`), `excalibur eval quick`, `tests/agentquality/`.

### Mechanism switches (measurement-owned)

| Switch | Where | Default | Effect when OFF |
|---|---|---|---|
| `--arm <name>` | `bench tb21\|tb40 run`, `eval quick` | `""` (default arm) | No arm recorded; run hashes to the frozen profile hash |
| `--set key=value` (repeatable) | `bench tb21\|tb40 run`, `eval quick` | none | No switch forwarded, no arm hash binding |
| `--bar F` | `bench compare` | `0.03` | Different practical bar for the accept/reject verdict |
| `--reps N --seed S` | `bench compare`, `eval report` path | `10000` / `7` | Different bootstrap precision/determinism |
| `--skip-docker`, `--filter`, `--concurrency`, `--timeout` | `eval quick` | off/none/4/180s | Suite runs everything serially with no skips |

No other measurement behavior is gated: peak indexing is always
max-based (a zero row never hides a peak), and the frozen eval profile
hash never moves for the default arm.

### 1. Named arms

`excalibur bench tb21 run --arm <name> --set key=value ...`

- Switches are validated (`[A-Za-z0-9_.-]+`, single-line values,
  no duplicates) and forwarded to the trial container verbatim as
  environment (`--ae key=value`, additive: no switch means a
  byte-identical Harbor command). By convention mechanism switches
  use `EXCALIBUR_` names, e.g. `EXCALIBUR_SELF_VERIFY=off`.
- Values for keys mentioning `KEY/SECRET/TOKEN/PASSWORD` print
  redacted in dry-run echoes; mechanism switches print verbatim so
  arms stay auditable. Keys never land in logs, ATIF or metrics.
- The run manifest records `arm`, `switches`, the frozen
  `profile_hash` and the combined `arm_profile_hash`
  (`eval.ArmProfileHash`: `sha256(profile_hash + arm + sorted
  switches)`). The default arm hashes exactly to the frozen profile
  hash, so pre-arm runs stay comparable. The frozen profile itself
  never changes per arm: same binary, tool table and skill catalog
  everywhere (Appendix F).
- `excalibur eval quick` accepts the same `--arm/--set` pair and
  records the same manifest fields, so cheap and paid runs ablate
  identically.
- Tests: `internal/eval/arms_test.go` (validation, hash stability,
  manifest round trip).

### 2. Paired comparison

`excalibur bench compare <runA> <runB> [--out DIR] [--k N] [--reps N] [--seed N] [--bar F]`

Works over bench jobs and quick dirs alike (`trials.jsonl` preferred,
`trial.json` fallback; missing `manifest.json` tolerated).

- Only tasks present in BOTH runs enter the paired statistics;
  one-sided tasks are listed as unpaired and excluded, so silently
  different task sets can never pass as an arm effect.
- Statistics (§23.7): per-task success rates with bootstrap CI per
  arm, paired bootstrap over per-task diffs
  (`eval.PairedDiffCI`, same seed convention as `eval Aggregate`),
  pass@k / pass^k per arm and their deltas, tokens and cost per
  success computed over the paired tasks only, and failure-category
  counts per arm.
- Verdict: `ACCEPT` iff the paired interval excludes 0 AND the mean
  gain clears the practical bar (default 0.03, the 3-point TB rule);
  otherwise `REJECT`. No declared difference (empty switch diff) is
  reported as measuring noise.
- Output: `compare.json`, `compare.md` (configuration diff first,
  verdict second, per-task and failure evidence after), and a
  one-line stdout summary. The switch diff (`arm` plus every changed
  key, `<absent>` for one-sided keys) is the ablation attribution.
- Tests: `internal/eval/compare_test.go` (clear win accepted,
  tie rejected, unpaired handling, failure split).

### 3. Local quick suite

`excalibur eval quick [--dir DIR] [--concurrency 4] [--timeout 180] [--filter SUB] [--list] [--skip-docker] [--arm N] [--set k=v]`

25 own small terminal tasks, written from scratch for this harness
(general engineering only — file edits, builds, data wrangling,
local services, scripting; never derived from any benchmark's
tasks, tests or solutions):

- scripting (5): hello-file, csv-sum, json-names, log-errors, top-word
- fix (5): py-syntax, shell-spaces, go-build, makefile-tab, json-config
- build (4): c-program, py-import, report-html, unpack
- data (4): sqlite-avg, dedup, checksums, merge-sort
- services (2): http-ping (verifier injects a free port via
  `PING_PORT`), run-twice
- terminal misc (4): git-commit, replace, big-files, today
- docker (1): docker-build-run (skipped without a daemon)

Each task runs `exec --profile eval` in an isolated work directory
with the task prompt, then a deterministic Go verifier checks the
work directory. Tools listed in `Needs` skip the task instead of
failing it. Concurrency is bounded (default 4) through
`safego.Go`. Results land as one `trial.json` per task plus
`trials.jsonl`, `manifest.json` (arm, switches, profile hashes)
and `report.json`, so `bench compare` and `eval report` consume
quick dirs exactly like bench jobs. A full stub pass runs in well
under a minute; a live pass costs a few minutes of model time.

- Tests: `internal/eval/quick_test.go` (all 25 verifiers accept the
  reference solution and reject an empty directory, fake-exec
  runner paths, cross-arm quick compare),
  `tests/agentquality/measurement_test.go` (public-API gate).

### 4. Resource sampler fix (peak RAM/CPU recorded in containers)

Observed failure: every pilot `trials.jsonl` row carried peak 0 and
CPU 0. Root causes, all in the record path rather than the benchmark:

1. The engine sampler wrote `resources.jsonl` rows but never fed the
   task metrics, so `metrics.json` held zeros.
2. The failure-finalizer wrote a second zero `final` row after the
   engine's real final row, and the index took the LAST row.
3. The metrics accumulator summed cumulative `/proc` CPU counters
   across ticks (inflating) instead of keeping the max.

Fix (additive, all covered by tests):

- `telemetry.TaskMetrics.ObserveDaemonPeak` (new method, max
  semantics for cumulative RSS/CPU) replaces the lossy path; the
  delta-based `ObserveDaemon` is untouched.
- The engine sampler takes a synchronous first sample (short trials
  get a peak), feeds every tick plus the final sample into the
  metrics peak, and still appends the incremental JSONL rows.
- `finishRun` feeds a live sample into the metrics before finishing,
  so stub/short/failed runs record a real peak; its closing JSONL
  row keeps the max of snapshot and live sample.
- The index (`eval.IndexTrial`) folds ALL `resources.jsonl` rows to
  peaks (max harness RSS, max CPU, max child peak) on top of the
  snapshot peaks: a trailing zero row can no longer hide a peak,
  while an all-zero record stays honestly zero. In the installed
  route the harness process IS the container workload, so the
  harness peak is the container signal (kept as separate
  `HarnessPeakRSS`/`PeakRSS` fields for the external route, where
  they differ).
- Remaining recommendation (not this workstream's package):
  populate `ChildPeakRSS` from the cgroup v2 `memory.peak` counter
  so tool-subtree peaks separate from harness peaks; the index
  already consumes that field with max semantics.
- Tests: `internal/telemetry/peak_test.go`,
  `internal/cli/resource_peak_test.go`,
  `internal/eval/resource_test.go` (pilot-shaped double-final
  records index the peak), plus the consumer-level
  `TestResourcePeakIndexed` in `tests/agentquality`.

### 5. Ablation reporting

Every comparison carries the arm identity, the full switch map per
side, both profile hashes, and the switch diff up front in
`compare.json`/`compare.md`; per-switch effects are read off by
comparing one-switch-apart arms. Quick dirs record the same
manifest fields, so the cheap loop (`eval quick` on arm A vs arm B,
then `bench compare`) attributes before any paid trial is spent.

### Research notes (levers surveyed before coding)

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026): fixed model (`gpt-5.2-codex`), 52.8 → 66.5 on TB2 by
  harness changes only. Levers that transferred into this
  workstream's measurement design: self-verification loop with a
  pre-completion checklist (measure gate attempts per trial —
  indexed), tracing at scale to find failure modes (rich per-trial
  records are the prerequisite), loop detection via per-file edit
  counts (loops indexed per trial), environment onboarding
  (environment profile recorded per trial), and the warning that
  single-task fixes overfit — hence paired per-task deltas and the
  unpaired-task guard in `compare`. Source:
  https://blog.langchain.com/improving-deep-agents-with-harness-engineering/
- Meta-Harness (Lee et al., arXiv 2603.28052, Mar 2026): outer-loop
  search over harness code with full access to prior candidates'
  source, scores and traces; discovered harnesses beat
  hand-engineered baselines on TerminalBench-2, with larger gains
  on weaker models. Takeaway for measurement: the outer loop is
  only as good as its trial records and its paired statistics —
  this workstream provides both, plus the arm hash that makes each
  candidate configuration addressable. Source:
  https://arxiv.org/abs/2603.28052
- Agentic Harness Engineering (arXiv 2604.25850): same direction —
  systematic harness search with standard evaluation and auditing
  records (Section 23.2, Appendix F) as the objective signal.
- Terminus 2 (Harbor reference harness): single tmux tool,
  JSON analysis/plan/keys, proactive context summary. Kept as the
  neutral baseline shape: our quick suite stays tool-agnostic
  (agents may solve with `exec` alone), and `compare` reports
  tool-call counts and tokens per success so terminal-only vs
  tool-rich arms can be ablated fairly.
- Open-source harnesses (OpenHands, SWE-agent, mini-swe-agent,
  Codex CLI, OpenCode, Aider): surveyed for prompt structure, tool
  surface minimalism (mini-swe-agent: bash-only tools can suffice —
  supports keeping `exec` central while ablating the rest),
  stopping criteria and trajectory shape. What we adopted is
  measurement-side only: per-tool call counts in the index,
  exit-code discipline in verifiers, and linear append-only JSONL
  records that survive kills.

## Self-verification and completion (W1)

Owner: self-verification workstream. Packages: `internal/tasks/**`
(gate, criteria, evidence) and the finish path of `internal/engine`
(completion/closing hooks only, via small hook interfaces).

### Problem

Measured on the current version: ~29% success; ALL failures end as
`completed` (success declared on wrong results); shallow work (avg
13 tool calls on failures, 8.6 on passes). Root cause: the Section
7.5 gate is strict over recorded criteria, but on paths where no
criteria are recorded it passes vacuously on the first final answer.
Related waste seen in records: `exec` calls with a filled `target`
parameter fail as `unknown target`; several tools answer "not wired"
in `exec`/`eval`; `search_tools` fails on empty queries. Those tool
surfaces belong to other workstreams; this section covers only the
completion decision.

### Mechanism

`internal/tasks/selfverify.go` (engine-independent) plus
`internal/engine/verify_hooks.go` (closing seam called from
`finishTurn`); no prompt or tool-surface changes.

1. Extraction: deterministic pass over the literal instruction to
   explicit checkable drafts (exact paths, file names, formats,
   outputs, commands, constraints) with a suggested executable check
   each, plus one verifier-role model pass that refines them
   (fallback: deterministic drafts).
2. Mandatory verification before `completed`: fresh executable
   evidence from the tool history (passing check after the last
   change; last result neither failed nor still running) plus
   contrast of the instruction against produced artifacts. Gaps
   return the agent to work with one capped note; returns bounded by
   `GateAttempts` (2), ending honest (`completed_with_pending` or
   `blocked`).
3. Independent verifier input for big tasks: fresh small context
   without executor reasoning (`BuildVerifierContext`, 8.7 shape);
   spawning and verdict recording stay with the orchestrator
   (`RunVerifier`/`RecordVerdicts` already exist).

The verifier never looks for the benchmark's own tests; it only
re-checks the agent's own checks.

### Ablation switches (all default ON)

| Switch | Mechanism |
|---|---|
| `EXCALIBUR_SELFVERIFY` | Master switch (off restores the legacy gate-only behavior) |
| `EXCALIBUR_SELFVERIFY_EXTRACTION` | Deterministic draft extraction |
| `EXCALIBUR_SELFVERIFY_VERIFICATION` | Mandatory verification phase |
| `EXCALIBUR_SELFVERIFY_CONTRAST` | Instruction-vs-artifact contrast |
| `EXCALIBUR_SELFVERIFY_INDEPENDENT` | Independent-verdict duty on big tasks |
| `EXCALIBUR_SELFVERIFY_REFINE` | Single model refine pass over drafts |
| `EXCALIBUR_SELFVERIFY_MIN_CHECKS` | Fresh checks required after the last change (default 1) |
| `EXCALIBUR_SELFVERIFY_MAX_RETURNS` | Bounds verification returns (default 2, mirrors `GateAttempts`) |
| `EXCALIBUR_SELFVERIFY_MAX_DRAFTS` | Cap on extracted drafts (default 6) |
| `EXCALIBUR_SELFVERIFY_BIG_TASK_STEPS` | Embedded big-task step threshold (default 20) |

### Sources

- LangChain, "Improving Deep Agents with harness engineering"
  (blog, 2026-02-17): build-verify loop discipline,
  `PreCompletionChecklist`-style middleware that intercepts the
  exit and forces a verification pass against the task spec, loop
  detection hooks, and the trace-analyzer improvement loop. Main
  prior art for the mandatory verification phase here.
- Open-source harness behavior studied as named in the round brief:
  SWE-agent / mini-swe-agent (submit only after tests pass),
  OpenHands (stop on repeated identical observations),
  OpenCode (`toolChoice: none` closing call, ask after repeated
  identical calls), Aider (architect/editor split), Codex (read
  parallelism discipline). No task material from any benchmark was
  used; only general harness behavior.
- Architecture sections implemented: 7.3 (closing mode, bounded
  gate attempts, honest states), 7.5 (gate rows), 7.6 (delivery
  report carries the pendings), 8.7 (independent verifier), 18.1
  (executable evidence outranks judgment; a cheap model is never
  the only judge of code), 19.1 (verifier role).

### Measurement

- Ablate each switch off/on; watch success rate, the share of
  failures ending `completed` vs `completed_with_pending`,
  verification returns per run, and extra cost (refine pass).
- Records to inspect: `gate_attempts.jsonl` (attempts went up when
  verification fires), `metrics.json` (`gate_attempts`), the ATIF
  trajectory (the injected `self-verify before finishing` note),
  `tool_calls.jsonl` (a fresh check follows the last change).

## W1 — skills library (reference content + discovery)

Owner: `internal/skills` (content and selection), with a minimal
additive seam in `internal/cli/exec_engine.go`.

Hypothesis: small models lack domain knowledge; a broader, more
detailed, easily discoverable skills library raises success.

What changed:

- Reference library: 193 -> 256 built-in skills in 32 groups
  (22 before, plus `terminal`, `packaging`, `build`,
  `languages`, `networking`, `containers`, `databases`, `data`,
  `ml`, `media`); every new skill carries when-to-use, procedure
  with concrete commands, pitfalls and verification steps, and
  `references/` depth files served on demand.
- Discovery: task-start auto-suggest from the instruction plus
  cheap environment signals (project files, PATH binaries) with a
  static synonym map on top of the unchanged BM25+vector ranking;
  `load_skill` serves focused `section`/`ref` slices; compact
  (names-only) catalog rendering; L0-L2 cache prefix untouched.
- Exec wiring: the programming L1 catalog and top suggestions
  ride the snapshot, the real skill tools replace the stubs, and
  every run writes `skills.json` plus a `skills_wired` event.

Switches (all read from the environment, no config-file changes):

| Switch | Values | Default | Effect |
|---|---|---|---|
| `EXCALIBUR_SKILLS` | `off`/`on`/`auto` | `auto` | `off`: no index, stubs stay, empty catalog, no suggestions; `on`: catalog+search+load, no injected suggestions; `auto`: `on` plus task-start auto-suggest |
| `EXCALIBUR_SKILLS_COMPACT` | `1`/`true` or unset | unset (full) | names-only L1 catalog lines for fully listed groups |

Measure: `off` vs `on` vs `on`+auto-suggest across the same task
set; `skills.json` in each run records mode, compact flag,
catalog hash and injected suggestions for the comparison.

Research sources for this section:

- LangChain, "Deep Agents" (blog, Jul 2025,
  https://blog.langchain.com/deep-agents/): depth comes from a
  detailed system prompt, a (no-op) planning tool, subagents and
  a filesystem — detailed reference content plus planning and
  delegation beats a bare tool loop.
- mini-SWE-agent FAQ (https://mini-swe-agent.com/latest/faq/):
  bash-only tool surface, linear history, stateless subprocess
  execution; 65% SWE-bench verified in ~100 lines — few
  well-designed tools and simple execution win on reliability.
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026):
  harness code (what to store, retrieve, present) moves results
  as much as weights; richer use of prior experience beats
  hand-tuned harnesses on Terminal-Bench-2.
- Agentic Harness Engineering, arXiv 2604.25850 (Lin et al., Apr
  2026): ten AHE iterations lift TB2 pass@1 69.7%→77.0%; gains
  localize to tools, middleware and long-term memory rather than
  the system prompt — factual harness structure transfers across
  model families, prose-level strategy does not.
- SWE-agent docs (https://swe-agent.com/latest/) and OpenHands
  docs (https://docs.openhands.dev/): tool-surface and sandbox
  design reference for background only (connectors frozen here).

## Tool surface for small models (`internal/tools`, `internal/wireengine`, `internal/cli/exec_engine.go`)

Evidence (read-only trial records, 57 trials / 26% pass): ALL failures end
as `completed` (the agent declares success while wrong); 17% of `exec`
calls fail on a model-filled `target` (`container`, `dev`, `/app` …);
`load_skill`, `search_skills`, `subagent`, `task_db`, `ack_directive`
answer "not wired"; `search_tools` fails on `{}`; `call_tool` fails on the
`input` alias and on `path` for `document_read`; `get_tool_output`
receives live `sess-N` ids; `edit_file` wildcard/`""` fingerprints loop.

What changed:

- Tolerant argument parsing (`internal/tools/surface.go` `NormalizeArgs`,
  wired into `Executor.Execute`/`ExecuteTuned`): common aliases (`cmd`,
  `cwd`, `limit`→`k`, `input`→`arguments`, `q`→`query`, `op`→`action`,
  `sid`→`session`, `path`→`pattern` …), scalar coercions (`"5"`→`5`,
  `"true"`→`true`), bare-string calls (`"echo hi"` → exec), single
  objects wrapped into `ops` arrays for `task_db`, model-internal keys
  (`target`, `owner`) dropped to internal defaults. Every validation
  failure appends a repair hint (`RepairHint`: expected shape, received
  keys). Empty `search_tools` lists popular deferred tools instead of
  erroring; empty `search_skills` lists the group; `call_tool` names
  `search_tools` on a missing name.
- Simplified model-visible `exec` schema: `command` (required), `workdir`,
  `timeout_s`/`yield_ms`, `tty`, `background`; `env` kept valid (vault
  `secret://` resolution depends on it) with shell-syntax guidance.
  Legacy `target`/`owner` validate nowhere and are ignored at runtime
  (embedded runs are always local) with a one-line note, turning the
  17% failure class into successes.
- Results built for small models: every `exec` result leads with
  `exit:`/`duration:`/`session:`/`alive:` plus the `exec_io` poll/read
  line; large outputs keep head+tail inline (8 KB) under the 40 KB
  pipeline budget; the truncation marker names `get_tool_output` with
  the exact ref; command-not-found appends package-manager guidance
  from `DetectEnv`; missing files point at `write_file`/`glob`/`ls`.
  Fixed a load-bearing wiring bug the enumeration test caught: the
  pipeline stored truncated blobs in the executor store while
  `get_tool_output` read its own store, so no blob ref ever resolved;
  `BuildEmbedded` now points the tool at the executor store.
- Shell-first design: `exec`/`call_tool`/`search_tools` descriptions
  steer toward chained Unix commands in one call; file tools keep
  rejecting genuinely broken edits (fingerprint mismatches still fail
  with re-read guidance) but accept an explicit `"expected_fp": "*"`
  wildcard as informed overwrite, and missing-file errors name the
  creation path.
- Embedded wiring (`internal/wireengine/embedded_surface.go`,
  `WireEmbeddedSurface`, built by default in `BuildEmbedded`):
  `task_db` plans against a run-scoped `MemoryStore` via
  `tasks.ApplyBatch`; `subagent` delegates to an embedded
  orchestrator with a master root agent (caller defaults to the run;
  spawn carries a single-run scope note); `ack_directive` acks pending
  root directives or reports none; `history_search` reads a run-local
  `HistoryLog` fed by a new `historyToolRunner` in `exec_engine.go`;
  `pin_observation`/`leave_continuation_note` keep run-local stores
  with reader seams for the context workstream;   `search_skills`/
  `load_skill` fall back to the embedded builtin index when no index is
  passed. `task_db` op names are normalized (`create_requirement` →
  `add_requirement`, `list` → `query`) and unknown ops name the valid
  list. Wiring fills stub providers only, so the core prefix hash
  never moves. `send_message` and other master/conversation tools stay
  daemon-owned stubs (H9 owns them); the eval surface never exposes
  them.
- Coverage: `TestExecSurfaceEveryToolWorks` (`internal/cli`)
  enumerates every eval-profile core tool and invokes it for real
  (plus deferred reachability through `search_tools`/`call_tool`);
  `TestEmbeddedSurfaceWired`/`Disabled` pin the wiring and the
  ablation switch; `surface_test.go` pins aliases, hints, prefixes and
  the wildcard.

Ablation switches (`tools.SurfaceTuning`, env `EXCALIBUR_TOOL_*=0` to
disable, default all ON; `Executor.Tune`, `EmbeddedOptions.Tune`,
`EmbeddedOptions.Surface` for tests):

- `EXCALIBUR_TOOL_TOLERANT_ARGS` — alias/coercion parsing + repair hints.
- `EXCALIBUR_TOOL_SIMPLE_EXEC` — simplified exec schema, ignore target/owner.
- `EXCALIBUR_TOOL_ACTIONABLE_ERRORS` — package-manager/missing-file hints.
- `EXCALIBUR_TOOL_HEAD_TAIL` — head+tail inline results with retrieval refs.
- `EXCALIBUR_TOOL_WIRE_EMBEDDED` — embedded task_db/subagent/ack/history/pin/note/skills wiring.
- `EXCALIBUR_TOOL_SHELL_FIRST` — shell-first tool descriptions (kept file-edit strictness).

## W2: automatic failure triage of evaluation trials

Owner: measurement workstream. Packages: `internal/eval/triage.go`
(classifier, judge client, writers) plus the `bench triage` side of
`cmd/excalibur` (`bench_triage.go` dispatch; the Harbor invocation is
untouched). Evaluation records are standard evaluation and auditing
(Section 23.2, Appendix F).

### Command

`excalibur bench triage <run-dir> [--model ID] [--out FILE]
[--credentials-file PATH] [--no-model]`

For every trial with reward 0, triage reads the agent's own records
(`agent/excalibur/*.jsonl`, final state, last tool outputs with blob
bodies for the last three calls, the instruction from the trial
trajectory) plus the pass/fail reward, and classifies the main cause
into the fixed taxonomy: `wrong-solution-declared-done`,
`incomplete-stopped-early`, `timeout`,
`environment/tooling-missing`, `tool-misuse/harness-friction`,
`loop/no-progress`, `context-loss`, `misunderstood-instruction`,
`knowledge-gap` (with a `domain` such as `R`, `python`, `sql`),
`other`. Each row carries a 1-2 sentence evidence quote and the step
where it went wrong. Output is JSONL (one row per failed trial,
sorted by trial) plus an aggregated Markdown table (counts per cause,
counts per domain for knowledge-gap, up to 3 examples per cause).
Default paths are `<run-dir>/triage.jsonl` and the sibling
`triage.md`. Passing trials (reward > 0) are counted and skipped, as
are trials with no readable reward (counted as `skipped`).

The triage never reads the verifier's tests, stdout, or reference
solutions: only `verifier/reward.txt` (or the rewards map of
`result.json`) is consulted, which carries pass/fail and nothing else.
Covered by `TestTriageNeverReadsVerifier`, which plants decoy markers
in verifier outputs and asserts they never reach the triage output.

Discovery accepts a single trial dir, a job dir of trials, or a run
root holding timestamped job dirs. No Terminal-Bench task contents,
tests, or solutions are inspected and no prompts or skills derive from
them: all signals are general harness behaviour (exit codes, timeout
strings, repeated commands, error strings, loop-detector rows,
missing-binary patterns, required-artifact checks).

### Method: deterministic first, one cheap model call when needed

1. Deterministic pass over ordered signals: Harbor/harness timeout
   strings, then environment/tooling-missing in the last five tool
   calls (late missing binaries the agent never recovered from;
   strong patterns such as `command not found` or `no such file or
   directory` fire on text alone because executor sessions can report
   exit 0 around a clearly failed command body),
   then harness friction (`unknown target`, `not wired`, `query is
   required`) when it persists late or dominates, then loop-detector
   rows (>= 2), then explicit context-loss strings, then
   misunderstood-instruction (an `/app/...` path or code file named
   in the instruction that no tool call ever touches), then
   knowledge-gap (>= 2 language-error outputs in the window, with a
   domain guessed from the instruction and commands), then bare
   repeated commands (>= 3 identical exec commands with no language
   errors, so retried broken code triages as knowledge-gap rather
   than a bare loop), then compaction-paired repeats, then
   incomplete (<= 4 tool calls, a background session still alive at
   the end, or a pkill after a hanging check), defaulting to
   `wrong-solution-declared-done`. A single language error, a missing
   instruction, or the ambiguous default marks the trial as needing
   the model.
2. Model fallback: one cheap low-effort call per ambiguous trial only
   (default the run's own model from the trial config, then the
   credentials file model, overridable with `--model`). The prompt
   carries only agent records and pass/fail, never verifier content,
   and the reply is strict JSON validated against the taxonomy.
   Transport is a minimal Chat Completions call with
   `reasoning_effort: low` and no temperature parameter (reasoning
   models such as `gpt-6-luna` reject anything but the default);
   on quota/rate-limit (429/5xx) it retries once with the credentials
   file `fallback_api_key` (bounded, same rule as live checks).
   Unknown causes or judge errors fall back to the deterministic
   verdict, and `DecidedBy` records which path decided each row.
   `TriageStats` tracks `judge_calls`/`judge_errors` (also printed by
   the command and rendered in the Markdown) so the deterministic-only
   ablation and judge health are measurable.

### Switches

| Switch | Type | Default | Off behaviour |
|---|---|---|---|
| `EXCALIBUR_TRIAGE_MODEL` | env (`0/false/no/off` disables) | on | deterministic only, never calls the judge |
| `--no-model` | CLI flag | off | same as the switch off for this run |

Measure the switch off against on over the same run directory:
deterministic-only coverage (share of rows decided without the
model), judge-changed share (rows where the model overrode the
deterministic guess), and per-cause distribution drift.

### Contract and backend tests

- `internal/eval/triage_test.go`: recorded-shape fixtures (Harbor
  trial dirs built in temp dirs: `result.json`,
  `verifier/reward.txt`, agent JSONL records, `agent/trajectory.json`
  instruction) cover timeout, environment/tooling-missing,
  tool-misuse/harness-friction, loop detector, bare repetition,
  incomplete (shallow), wrong-solution-declared-done,
   misunderstood-instruction (required `/app/ars.R` never touched),
   knowledge-gap with domain (`python`), exit-0 failure bodies (a
   traceback recorded with `exit_code` 0 still triages as
   environment/tooling-missing), pass skipping, no-reward skipping
   (`skipped` stat), Markdown aggregation, `FindTrialDirs` shapes
   (single trial, job dir, job-level `result.json` not shadowing
   trials, nested run root, empty error), and the
   `EXCALIBUR_TRIAGE_MODEL` switch.
 - Judge client against `httptest` servers: model override of an
   ambiguous trial, fallback-key retry after 429 (asserts the second
   `Authorization` header), rejection of an unknown cause and of a
   500 error back to the deterministic verdict (with `judge_calls` /
   `judge_errors` tracked), `reasoning_effort: low` on the wire and
   no temperature parameter.
- `TestTriageNeverReadsVerifier`: decoy markers in
  `verifier/test-stdout.txt`, `test-stderr.txt`, `ctrf.json`,
  solution and tests files never reach JSONL or Markdown output.
- Existing suites untouched: `go test -race ./internal/eval/` stays
  green; the bench Harbor invocation and all connectors are
  unmodified.

### Research sources

- LangChain, "Improving Deep Agents with harness engineering" (blog,
  2026-02-17, fetched full text 2026-10-31): 52.8 to 66.5 on TB2 by
  harness changes only. Triage mirrors its trace-analyzer loop:
  automate failure-mode analysis at scale from execution traces
  (here: per-trial records), aggregate into targeted harness
  changes, and warn against overfitting single-task fixes — hence
  the paired per-cause counts plus examples rather than per-task
  prescriptions. Deterministic-first ordering follows its finding
  that missing verification, environment onboarding gaps, and doom
  loops dominate failures. Source:
  https://blog.langchain.com/improving-deep-agents-with-harness-engineering/
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026; abstract
  fetched 2026-10-31): harness code (what to store, retrieve,
  present) moves results as much as weights; richer access to prior
  experience beats hand-tuned harnesses. Triage is the read side of
  that outer loop: it mines the same trial records the optimizer
  would consume, without tuning against task answers. Larger gains
  on weaker models motivate the deterministic-first design (cheap,
  model-independent signals before spending judge calls). Source:
  https://arxiv.org/abs/2603.28052
- Agentic Harness Engineering, arXiv 2604.25850 (cited, not
  fetched): same direction — systematic harness search with
  standard evaluation and auditing records as the objective signal.
- Terminus 2 (Harbor reference harness): single-tool tmux loop and
  the missing-binary failure mode noted in the Section 23.2
  analysis; triage classifies that mode explicitly as
  `environment/tooling-missing` from late `command not found`
  outputs.
- Open-source harnesses (general engineering knowledge, no task
  content used): OpenHands loop detection (repeated calls+errors),
  SWE-agent/mini-swe-agent submit-only-after-tests discipline
  (informs the wrong-solution vs incomplete split), Codex/OpenCode
  `tool_choice: none` closing shape (informs the alive-session and
  pkill-after-hang incomplete signals), Aider architect/editor split
  and OpenCode JSON repair (judge prompt keeps strict JSON with a
  deterministic fallback).

Documentation describes standard evaluation and auditing records
(Section 23.2, Appendix F); nothing here tunes the agent against
benchmark failures or derives prompts from task contents.

## W2: parallel attempts with self-selection (Section 18.2)

Owner: parallel-attempts workstream. Packages: new `internal/attempts`
(config, isolation, selection, records) plus a thin seam in
`internal/cli` (`attempts.go`, `ExecOptions.DisableAttempts`,
`ExecRun.ProjectDir`, `telemetry.TaskMetrics.MergeSnapshot`). The model
gateway wire protocol, the providers, the MCP/ACP servers, the bench
Harbor invocation and the eval profile table are untouched.

### Problem evidence (read-only trial records, frozen full run)

From `analyze.py` over the frozen full run (275 trials, 31.3% pass):

- ALL failures end as `completed`: the agent declares success while the
  result is wrong. Shallow work (avg 14.2 tool calls on failures, 9.5
  on passes).
- Single-shot execution has no recovery when the first plan is wrong;
  the self-verification gate can only return the same run to work, it
  cannot explore an alternative approach in parallel.

### Mechanisms (default OFF until measured)

`EXCALIBUR_ATTEMPTS=<N>` (1 means off, the default; clamps to 8).
When N>1 and the run uses the engine or a `Work` hook, the run fans
out to N independent attempts:

1. Isolation: one working copy per attempt — git worktree
   (`git worktree add --detach HEAD`) when the project is a git repo
   and the CLI is available, otherwise a filesystem overlay copy
   (`.git`/`.excalibur` skipped). Empty projects get N fresh dirs.
2. Budget sharing: each attempt receives `ceil(parentSteps/N)` steps
   and `parentUSD/N` cost under the shared wall-clock deadline;
   concurrency is bounded by `min(N, NumCPU)`.
3. Execution: each attempt runs the normal loop to its verification
   phase as a full sub-`RunExec` (own logs, metrics, ATIF under
   `<run>/attempts/attempt-i`), concurrently via `safego.Go`.
4. Self-selection (Section 18.2, never the benchmark verifier):
   completed outranks `completed_with_pending` outranks the rest;
   tie-breaks prefer more verified checks (successful tool results),
   then fewer tool errors, then a cheap model comparison of evidence
   (one `verifier`-role call, 16 tokens, 30 s cap; deterministic
   launch order when tied or without a gateway).
5. Apply: only the winner's changes overlay the real directory
   (new/modified copied, winner-deleted removed; `.git` never
   touched). Outer metrics sum all attempts (`MergeSnapshot`, max
   semantics for peaks) so cost per success reflects best-of-N.
6. Records: `attempts.json` (version `attempts@1`, N, winner, per
   attempt state/summary/tool counts/gate attempts/verified/errors/
   files/diff) lands in the run dir and, under eval, in
   `<logs>/excalibur/attempts.json`; per-attempt logs stay under
   `<run>/attempts/attempt-i` with their own metrics for the audit.

### Switch

| Switch | Type | Default | Off restores |
|---|---|---|---|
| `EXCALIBUR_ATTEMPTS` | env int 1–8 | 1 (off) | single attempt, no `attempts.json`, no isolation |

Measure `1` vs `2` vs `3` on the same model/effort/task set
(Appendix F); keep N>1 only if paired success gain clears the bar
after accounting for summed cost. Small step budgets starve quickly
(e.g. parent 15 steps with N=3 gives 5 per attempt, observed all
`paused`): scale steps with N when ablating.

### Research sources (fetched before coding, general engineering only)

- LangChain, "Improving Deep Agents with harness engineering"
  (Feb 2026, fetched full text): 52.8→66.5 on TB2 with the model
  fixed. Adopted: build-verify-fix loop with a pre-completion
  checklist (our verification phase per attempt), environment
  onboarding (snapshot stays), loop-detection reconsider notes, and
  the trace-analyzer loop that motivates per-attempt records as the
  outer-loop signal. No task content used.
- Meta-Harness (arXiv 2603.28052, fetched abstract): harness code
  (what to store, retrieve, present) moves results as much as
  weights, with larger gains on weaker models; motivates keeping
  attempts isolated and selection on executable evidence rather than
  prompt prose.
- Agentic Harness Engineering (arXiv 2604.25850, cited from the
  architecture refs): systematic harness search with standard
  evaluation and auditing records as the objective — attempts are
  one declared arm of that search (`--arm`/`--set` measure it).
- Terminus 2 (Harbor reference harness): single-tool tmux loop;
  Section 23.2 analysis notes its missing-binary failure mode —
  isolation here keeps the same container boundary per attempt.
- Open-source harnesses (general behavior only): SWE-agent submit
  only after tests pass, mini-swe-agent minimal bash loop, OpenHands
  loop detection, OpenCode/Codex `tool_choice: none` closing,
  Aider architect/editor split. Selection uses test execution, not
  a judge, per Section 18.2 (selection captures only 50–76% of the
  possible sampling gain).

Documentation describes standard evaluation and auditing records
(Section 23.2, Appendix F); nothing tunes prompts or skills from
benchmark tasks, tests or solutions.

### Contract and backend tests

- `internal/attempts`: config parse/clamp/division
  (`config_test.go`), deterministic selection order and compare-hook
  refinement (`outcome_test.go`), copy/apply/diff isolation and git
  worktree when available (`isolate_test.go`), concurrent order and
  error isolation (`runner_test.go`), record round trip
  (`record_test.go`).
- `internal/cli`: single stays single without `attempts.json`;
  N=2 selects the completed attempt and applies only its files;
  bogus env stays single; eval writes both `attempts.json` copies
  with per-attempt metrics (`attempts_test.go`).
- `internal/telemetry`: `MergeSnapshot` sums usage/cost/calls and
  keeps peak semantics (covered via attempts e2e metrics sums).
- Full quality bar green: `gofmt -l .`, `go vet ./...`,
  `staticcheck ./...`, `go test ./... -race`, `GOOS=darwin/windows
  go build ./...`, `scripts/check-no-bare-go.sh`.
- Live end-to-end (own small tasks, never Terminal-Bench): `excalibur
  exec --profile eval` with `EXCALIBUR_ATTEMPTS=2` on hello-file and
  csv-sum tasks completes and applies the winner (`hello.txt`,
  `numbers.csv`/`sum.txt` correct, `attempts.json` N=2 with tool
  counts summed in `metrics.json`); N=3 on a fix task shows the
  budget-share edge (5 steps each, all `paused`, winner still
  applied). Credentials via the operator file, `gpt-6-luna` low
   effort; no paid trial launched beyond these checks.

## W2 — exploration before acting (Sections 7.3, 11.1, 15.2 explorer)

Owner: repo-explorer workstream. Packages: `internal/repoexplore`
(deterministic collector), `internal/cli` (`exec_engine.go` wiring).
The connectors are untouched: no change to `internal/providers`,
the model gateway wire protocol (`internal/models`,
`internal/gateway`), `internal/api`, `packages/contracts`,
`integrations/**`, MCP/ACP servers, or the `bench` Harbor invocation.

### Problem evidence (read-only trial records, frozen full run)

From `analyze.py` over the frozen full run (275 trials, 31.3% pass):

- ALL failures end as `completed` while the result is wrong; shallow
  work (avg 14.2 tool calls on failures, 9.5 on passes).
- Early steps are spent discovering the repo: `ls`, `head` data
  files, `which python3/R`, probing build files before any real work.
  17% of `exec` calls in the older snapshot failed on a model-filled
  `target` parameter; the current tree already tolerates it, but the
  discovery waste remains: the model re-lists the same directory and
  re-probes versions the harness could state once.
- Architecture Sections 15.2-15.4 already define the answer: an
  `explorer` profile (read-only: no writes, no terminal execution)
  that summarizes code/docs for the parent without loading the parent
  context. This workstream makes that phase automatic and free: no
  extra model call, no gateway change.

### Mechanism (default OFF until measured)

New package `internal/repoexplore` (stdlib only): at task start a
bounded, read-only filesystem pass inspects the workdir relevant to
the instruction and returns a compact structured brief (<=60 lines):

- layout: top-level entries, sorted, heavy dirs (`node_modules`,
  `.git`, `target`, …) collapsed to entry counts;
- build files: `go.mod` (module + go line), `Cargo.toml`,
  `package.json` (scripts), `pyproject/setup/requirements`,
  `Makefile` (targets), `CMakeLists.txt`, gradle, maven;
- entry points: `main.go`, `cmd/*/main.go`, `main.py`/`app.py`,
  `src/index.*`, `Dockerfile` CMD hint; tests (`*_test.go`,
  `test_*.py`, `*.spec.*`, `tests/`) plus one run command
  (`go test ./...`, `cargo test`, `pytest`, `npm test`);
- configs (Dockerfile, compose, `.nvmrc`, `.python-version`,
  toolchain pins), data files with sizes and format hints (CSV
  header, JSON, SQLite `sqlite3 .tables`, Parquet), version pins read
  from files only (no `--version` execution; `envsnap` owns probes);
- instruction relevance: path-like tokens in the instruction resolve
  to `exists (size/dir)` vs `not found` (max 5);
- pitfalls (max 5): no build file, no tests, no README, collapsed
  dirs, large data, unclear entry point.

Budgets: 3s timeout (`EXCALIBUR_EXPLORE_TIMEOUT`), 120 files,
depth 3, 8KB per file read, 200KB total head budget. Collection never
fails the run; every probe degrades to absence. Fresh context: the
collector reads the filesystem directly and returns a self-contained
brief; the main history is untouched.

Wiring (`internal/cli/exec_engine.go`, `runEnginePath`): when enabled
and `ProjectDir != ""`, the collector runs once at task start; the
brief renders into one L3 working-note slot (`Task.WorkingNotes`,
single note with embedded newlines so the 12-note cap keeps it intact)
and stays byte-identical on every snapshot of the run (no timestamps,
sorted, capped) so the L0-L2 cache prefix never churns. The run also
writes `explore.json` (switch, workdir, line count, truncation, brief)
next to `skills.json` and emits an `explore_brief` event for the
ablation. Empty/missing workdir degrades to a two-line brief, never an
error.

Design choice (see `docs/decisions/0046-w2-repo-explorer.md`): a
deterministic collector instead of a model-driven subagent spawn. It
honours the same contract (explorer profile toolset, small budget,
fresh context, structured brief pinned to working notes) with zero
extra model calls, zero gateway changes, and byte-stable output that a
model explorer cannot guarantee. A future model-refine pass can layer
on top without changing the switch or the record shape.

### Switch

| Switch | Values | Default | Effect |
|---|---|---|---|
| `EXCALIBUR_EXPLORE` | `on`/`1`/`true`/`yes` enables | `off` | `off`/unset: no collection, no L3 change, no `explore.json`; `on`: collect once, pin brief to L3 working notes |

`EXCALIBUR_EXPLORE_TIMEOUT` overrides the 3s collection budget (Go
duration). Measure `off` vs `on` on the same task set; watch success
rate, early-step tool calls (`ls`/`head`/`which` before first real
action), and cost (explorer adds no model calls).

### Research sources for this section

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, fetched full text,
  https://blog.langchain.com/improving-deep-agents-with-harness-engineering/):
  52.8→66.5 on TB2 with the model fixed. Adopted: onboarding
  context (`LocalContextMiddleware` mapping cwd, tooling) so the
  model stops discovering the environment; plan/discovery before
  build; pre-completion verification. Our explorer is the onboarding
  half: layout/build/entry/tests up front, verification stays with
  the self-verification workstream.
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026, fetched
  abstract, https://arxiv.org/abs/2603.28052): harness code (what to
  store, retrieve, present) moves results as much as weights; richer
  use of prior experience transfers across models, with larger gains
  on weaker models. Motivates a deterministic, cached brief over
  prompt growth.
- Agentic Harness Engineering, arXiv 2604.25850 (cited, not fetched):
  same direction — systematic harness search with standard
  evaluation and auditing records (Section 23.2, Appendix F) as the
  objective.
- Terminus 2 (Harbor reference harness): single tmux tool with JSON
  analysis/plan/keys and a proactive context summary. Kept as design
  reference for the brief shape (what exists, where, how to run).
- Open-source harnesses (general engineering knowledge, no task
  material used): mini-SWE-agent FAQ (bash-only tools suffice,
  stateless subprocess execution,
  https://mini-swe-agent.com/latest/faq/); SWE-agent (short system
  prompt with explicit test running); OpenHands (loop detection on
  repeated calls+errors); Codex/OpenCode `tool_choice: none` closing
  mode and JSON repair; Aider (architect/editor split). What we took
  is behaviour-side only: read-only first pass, small stable context,
  explicit build/test commands, stop conditions that keep the brief
  advisory (verify-with-tools footer).

### Contract and backend tests

- `internal/repoexplore/explore_test.go`: section coverage
  (layout/build/entry/tests/data/versions), determinism and
  single-line cap, instruction-path exists/missing, empty-workdir
  degradation, heavy-dir collapse, CSV/SQLite hints, 60-line (and
  custom) cap with verify footer, default-off switch matrix.
- `internal/cli/exec_explore_test.go`: default off injects nothing
  (no brief, no `explore.json`, no event); switch on pins the brief
  into L3 working notes byte-identical across two model calls,
  writes `explore.json` (<=60 lines) plus the `explore_brief` event;
  empty workdir with the switch on stays silent and never fails.
- Existing suites unchanged and green: engine/ctxcompiler determinism
  (`go test -race ./internal/engine/ ./internal/ctxcompiler/`), exec
  env/time (`exec_envtime_test.go`).

## W3: automatic dependency provisioning (Sections 13.5, 18.1)

Owner: auto-provision workstream. Packages: `internal/tools`
(`autoprovision.go` detection/helper/policy, `exec_tools.go`
wiring). The connectors are untouched: no change to
`internal/providers/**`, the model gateway wire protocol
(`internal/models`, `internal/gateway`), `internal/api`,
`packages/contracts`, `integrations/**`, MCP/ACP servers, or the
`bench` Harbor invocation.

### Problem evidence (read-only trial records, never task answers)

From the automatic triage of 44 failed evaluation trials
(`triage-all-on.md`, general behaviour only): 12 fail as
`environment/tooling-missing` while a package manager and network
were available and the agent never installs the missing piece:
`python3: command not found`, `file: command not found`,
`ps`/`ss: command not found`, `No module named 'chess'`/`'cv2'`,
`ImportError: liblapack.so.3: cannot open shared object file`.
Tool errors had already fallen to ~0; the remaining failures are
dominance of discipline and verification gaps, so this workstream
removes the tooling-missing class at the harness level instead of
spending model steps rediscovering it.

### Mechanism (default ON)

1. Recognition (`DetectMissing`, pure function over the recorded
   `command` plus `output`, never the benchmark answers): shell
   `command not found` / `: not found`, Python `No module named`,
   Node `Cannot find module` (bare specifiers only; relative
   paths and `fs`/`path`/… builtins never provision), Ruby
   `cannot load such file`, R `there is no package called`,
   Perl `Can't locate ... in @INC`, Go `cannot find package`,
   Rust `can't find crate` / `no matching package` / `unresolved
   import`, Java `package ... does not exist`, shared libraries
   `.so: cannot open shared object file`. Python `os`/`json`/…,
   R `base`/…, Go stdlib (no dot) and generic `No such file or
   directory` for build artifacts never provision; install
   commands themselves never re-trigger (loop guard).
2. Provisioning helper (`PlanInstall`): maps each hit to
   install-only commands for the detected managers: apt with
   `DEBIAN_FRONTEND=noninteractive`, `-y --no-install-recommends`
   and update-once caching (the first apt plan in the process
   carries `apt-get update -qq`, later plans skip it), other
   managers best-effort (`dnf -y`, `apk add --no-cache`,
   `pacman -Sy`, `brew install`), pip/uv with the right
   interpreter (`python3 -m pip` preferred, `uv pip --python`
   when uv exists, `--break-system-packages` for pip on PEP 668
   systems, `--quiet`, never `--upgrade`), `npm install`
   (`yarn add`/`pnpm add` fallback), `gem install --no-document`,
   R `install.packages`, `cpanm --notest` / `cpan -T`,
   `go get`, `cargo add`. Curated `command→package` (`ps`→`procps`,
   `ss`→`iproute2`, `file`→`file`, …), `import→distribution`
   (`cv2`→`opencv-python-headless`, `chess`→`chess`,
   `PIL`→`Pillow`, `sklearn`→`scikit-learn`, …) and
   `.so→apt` (`liblapack.so.3`→`liblapack3`, …) maps; unknown
   commands fall back to the binary name. Every plan carries a
   `verify` step and never contains `upgrade`, `dist-upgrade`,
   `remove`, `purge`, `autoremove`, `uninstall` or `--upgrade`
   (enforced by `isForbiddenProvisionCommand`).
3. Policy (`Provisioner.MaybeProvision`, wired into
   `ExecTool.Invoke` after the actionable hint): in `auto` the
   harness runs the plan when a TCP probe (`8.8.8.8:53`,
   `1.1.1.1:53`, no payload, no paid API) succeeds and always
   appends the `provision:` report (detected, plan, update/install
   outcome with tail, re-run guidance); `suggest` reports the
   commands without running; `off` restores the pre-provisioning
   bytes. Java coordinates are always suggest-only (they need a
   build-file edit). Failures report the failing command plus the
   manual retry and verify step; successes tell the model to re-run
   and verify.

### Switch

| Switch | Values | Default | Effect |
|---|---|---|---|
| `EXCALIBUR_AUTOPROVISION` | `auto` / `suggest` / `off` (`1`/`on`/`true`/`yes` mean auto, `0`/`off`/`false`/`no` mean off) | `auto` | `auto`: detect, run when network is available, always report; `suggest`: detect and report commands, never run; `off`: no detection, hint only (pre-provisioning bytes) |

Measure `off` vs `suggest` vs `auto` on the same task set;
watch the `environment/tooling-missing` triage share, extra tool
calls to recovery (`provision:` lines in `tool_calls.jsonl`),
and cost (one apt update per process, one install per missing
dep). See `docs/decisions/0047-w3-auto-provision.md` for the
PEP 668, Java suggest-only and update-once choices.

### Research sources (fetched before coding, general engineering only)

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, fetched full text,
  https://blog.langchain.com/improving-deep-agents-with-harness-engineering/):
  52.8→66.5 on TB2 with the model fixed. Adopted: environment
  onboarding (`LocalContextMiddleware`) so the model stops
  discovering tooling, pre-completion verification, loop-detection
  reconsider notes. Auto-provision is the onboarding half for
  missing tooling: install instead of hinting when safe, always
  reported for the trace-analyzer loop.
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026, fetched
  abstract, https://arxiv.org/abs/2603.28052): harness code (what
  to store, retrieve, present) moves results as much as weights,
  with larger gains on weaker models. Motivates fixing the
  tooling-missing class in code rather than the prompt.
- Agentic Harness Engineering, arXiv 2604.25850 (cited, not
  fetched): same direction — systematic harness search with
  standard evaluation and auditing records (Section 23.2,
  Appendix F) as the objective signal.
- Terminus 2 (Harbor reference harness): single-tool tmux loop;
  the Section 23.2 analysis notes its missing-binary failure mode
  (24.1% of command failures) — the exact class auto-provision
  removes.
- Open-source harnesses (general behaviour only, no task material
  used): mini-SWE-agent FAQ (bash-only tools suffice,
  https://mini-swe-agent.com/latest/faq/ — stateless subprocess
  execution motivates running installs as plain shell commands);
  SWE-agent (short system prompt with explicit test running);
  OpenHands loop detection; Codex/OpenCode `tool_choice: none`
  closing; Aider architect/editor split. What we took is
  behaviour-side only: install-then-verify discipline, explicit
  verify steps, and never upgrade/remove.

## W3: loop breaking and tool noise (Sections 7.3, 11.1, 13.3, 18.3)

Owner: loops-and-noise workstream. Packages: `internal/tasks`
(`loops.go`: `near_repeat`/`no_info` rules), `internal/engine`
(`quality.go` recovery prompt, `engine.go` repeat block, novelty
tracking, R4 zero-state skip), `internal/tools` (`surface.go`,
`deferred.go`, `catalog.go`: call_tool tolerance and description),
`internal/cli` (`surface_hide.go`, `exec_engine.go`: prefix pruning).
The connectors are untouched: no change to `internal/providers`,
the model gateway wire protocol (`internal/models`,
`internal/gateway`), `internal/api`, `packages/contracts`,
`integrations/**`, MCP/ACP servers, or the `bench` Harbor invocation.

### Problem evidence (read-only trial records, never task contents)

From the automatic triage of 44 failed trials of the current
agent-quality build (tool errors near zero; failures dominated by
discipline and verification problems) plus `analyze.py` counts:

- 4 `loop/no-progress` failures: the same command repeated without
  progress (process polling, repeated rebuild-and-run). Exact-match
  R1/R2 rules never fired because outputs varied between repeats.
- `ack_directive` called 36 times with no directive pending
  (single-run scope never has any); `call_tool` called 11 times
  without a name (the model nests the name inside `arguments` or
  wraps the call as `{"toolname": {...}}`).
- Load-bearing bug found in this work: nothing in exec/eval scope
  ever installs a progress sampler (`SetProgressFunc` has no
  callers), so R4 hashed the zero state and fired spuriously every 8
  tool steps on every long run (note, escalate, block at 24 steps).

### Mechanisms (all default ON)

1. Near-repeat detection (`tasks.RuleNearRepeat`, `LoopNearRepeat`
   3, similarity >= 0.8 over whitespace-collapsed arguments): 3
   consecutive same-tool calls with near-identical arguments fire
   even when R1/R2 stay silent; a different tool or command restarts
   the run. Staged note/escalate/blocked like every rule.
2. No-information detection (`tasks.RuleNoInfo`, `LoopNoInfoSteps`
   6): the engine marks a step informative only on a successful call
   with non-empty output differing from that tool's previous
   success; 6 dull steps fire, then stage every 6.
3. Structured stuck-recovery prompt at stage 1 of both rules
   (`engine.recoveryPrompt`): keeps the `loop:` prefix, lists the
   last tried calls, forces "summarise in 2 lines / list 3 DIFFERENT
   strategies / pick one and execute its first step now".
4. Exact-repeat block: a single-call batch repeating the previous
   single call byte-for-byte with a deterministic outcome is
   rejected once per run and signature with guidance; it never
   executes and never pollutes the R1/R2 windows. Retryable
   outcomes (transient, timeout, canceled) are never blocked;
   multi-call batches pass through.
5. R4 zero-state skip: without a progress signal the legacy R4
   observation is skipped and novelty tracking replaces it in
   single-run scope.
6. Prefix pruning (prefix rebuilt every step, execution stays
   wired, frozen eval table untouched): `ack_directive` hidden while
   no directive is pending (a pending root directive restores it);
   `clock` hidden under the eval profile (zero productive calls;
   time already rides L5).
7. `call_tool` tolerance: the name promotes from inside arguments
   (`name/tool/tool_name`, case-insensitive) or from a single-key
   wrap (`{"toolname": {...}}`, wire-order leading key); consumed
   keys are removed so validation sees the canonical shape. Both
   visible descriptions carry a one-line example.

### Switches

| Switch | Mechanism | Off restores |
|---|---|---|
| `EXCALIBUR_Q_RECOVERY` | 1, 2, 3, 5 (recovery prompt, novelty tracking, R4 skip) | legacy nudges + legacy R4-on-zero-state |
| `EXCALIBUR_Q_REPEATBLOCK` | 4 (block exact repeats once) | all repeats execute |
| `EXCALIBUR_TOOL_HIDE_ACK` | 6 (hide idle ack_directive) | always shown |
| `EXCALIBUR_TOOL_HIDE_CLOCK` | 6 (hide clock in eval) | legacy eval prefix |
| `EXCALIBUR_TOOL_CALL_NAME` | 7 (call_tool name promotion) | strict name-required |

Measure each switch off against on with the same model, effort and
task set (Appendix F conditions); watch success rate, loop-note and
block counts (`Loop.Counts` in records), blocked-repeat counts in
history, and prefix tokens per model call.

### Research sources

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, fetched full text,
  https://blog.langchain.com/improving-deep-agents-with-harness-engineering/):
  52.8 to 66.5 on TB2 with the model fixed. Adopted: loop-detection
  middleware with reconsider notes (our recovery prompt is that
  intervention at stage 1 with a forced output shape), deterministic
  context injection for verification, environment onboarding, and the
  trace-analyzer warning against single-task overfitting (hence
  paired per-task statistics via the measurement workstream).
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026, fetched
  abstract, https://arxiv.org/abs/2603.28052): harness code (what to
  store, retrieve, present) moves results as much as weights, with
  larger gains on weaker models; motivates code-side mechanisms with
  no prompt growth.
- Agentic Harness Engineering, arXiv 2604.25850 (cited, not
  fetched): same direction — systematic harness search with
  standard evaluation and auditing records (Section 23.2, Appendix F)
  as the objective signal.
- Terminus 2 (Harbor reference harness): single-tool tmux loop;
  kept as the neutral baseline shape. Open-source harnesses
  (general engineering knowledge, no task content used): OpenHands
  (stop on repeated identical observations), mini-swe-agent
  (bash-only tools suffice — supports pruning idle tools),
  Codex/OpenCode (`tool_choice: none` closing — our block is the
  same instinct mid-loop), SWE-agent (short prompt, explicit test
  running), Aider (architect/editor split behind expert escalation).

Documentation describes standard evaluation and auditing records
(Section 23.2, Appendix F); nothing here tunes the agent against
benchmark failures or derives prompts from task contents.

### Contract and backend tests

- `internal/tools/autoprovision_test.go`: recorded-output
  detection table (triage quotes: `python3`/`file`/`ps`/`ss`
  not found, `chess`/`cv2` modules, `liblapack.so.3`, R/Ruby/
  Perl/Node cases, stdlib/relative/loop guards, generic
  `No such file` ignored), Go diagnostics, curated maps and flags
  (`--no-install-recommends`, `--break-system-packages`,
  `--no-document`), apt update-once (first plan carries update,
  second skips), never-upgrade/remove across all managers,
  policy matrix (off silent, suggest no-run, offline suggest,
  online success/failure with `provision:` lines),
  `EXCALIBUR_AUTOPROVISION` parsing, exec integration (suggest
  appends `provision:`, off is silent), real shell runner check
  (no docker, no network, no apt).
- Existing suites unchanged and green: `go test -race
  ./internal/tools/` plus the full `go test ./... -race`
  (one transient `internal/gateway` websocket timeout under
  parallel load with the evaluation running; passes isolated
  and with `-p 1`, no gateway code touched), `gofmt -l .`,
  `go vet ./...`, `staticcheck ./...`, `GOOS=darwin/windows
  go build ./...`, `scripts/check-no-bare-go.sh`.
- Live end-to-end (own small tasks, never Terminal-Bench,
  `excalibur exec --profile eval`, credentials file,
  `gpt-6-luna` low): `import chess` auto-installs via pip
  (`--break-system-packages`, exit 0, `1.11.2` on re-run, 2 tool
  calls); missing binary reports the apt plan (update ok,
  install fails cleanly with manual guidance); `hello.txt`
  shows no false positive. Records carry `provision:` lines in
  `tool_calls.jsonl`.
- `internal/tasks/loops_test.go`: near-repeat fires on same-command
  varying-result polls (note first), cosmetic tweaks match, tool or
  command changes restart the run, persisting stages to blocked;
  similarity table; no-info stages every K with reset on informative
  steps; `TestLegitimateWaitForNotStopped` pins the new contract
  (short polls silent, `wait_for` exemption untouched).
- `internal/engine/quality_test.go`: defaults and env overlay for
  both switches; recovery text shape (prefix, tried calls, three
  strategies, pick-one) and legacy fallback; `onDetection` routing
  with pinning; repeat block once (third identical runs), retryable
  exemption, multi-batch passthrough, switch off; no-info recovery
  through the loop; R4 zero-state skip on / legacy fire off.
- `internal/engine/lifecycle_test.go`:
  `TestLoopEscalationNoteExpertBlocked` runs 8 identical calls (one
  consumed by the block) to the same note/escalate/blocked stages
  with exactly one expert consult.
- `internal/tools/surface_test.go`, `deferred_test.go`: nested and
  wrapped name promotion, empty-name fallback, wrap-skip on known
  keys, flex-off strictness, description examples, `Invoke`
  fallback, `EXCALIBUR_TOOL_CALL_NAME` switch.
- `internal/cli/surface_hide_test.go`: ack pruned when idle (nil
  surface counts as idle), restored on pending directive, switch
  off; clock pruned in eval only, switch off.
- Existing suites unchanged and green: tasks, engine lifecycle/e2e,
  tools deferred/pipeline, cli exec surface enumeration (hidden
   tools stay executable), wireengine prefix stability.

