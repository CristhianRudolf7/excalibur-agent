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
   misunderstood-instruction (required `/app/statistics.R` never touched),
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

## W3: required-artifact completion gate (Sections 7.3, 7.5)

Owner: required-artifacts workstream. Packages: `internal/tasks`
(`artifacts.go`, engine-independent) plus the closing seam of
`internal/engine` (`verify_hooks.go` `applyArtifacts`, `Run.Workdir`,
`Deps.Artifacts/ArtifactStat/ArtifactRead`) and one workdir line in
`internal/cli/exec_engine.go`. The connectors are untouched: no
change to `internal/providers`, the model gateway wire protocol
(`internal/models`, `internal/gateway`), `internal/api`,
`packages/contracts`, `integrations/**`, MCP/ACP servers, or the
`bench` Harbor invocation.

### Problem evidence (read-only trial records, current version)

From the triage of 44 failed evaluation trials (all-on run,
2026-10-31; tool errors down to ~0, failures now dominated by
discipline and verification problems):

- 12 failures name files/paths/outputs in the instruction
  (backticked absolute paths, "put the function in a file called
  X", "install to Y") that the agent never created, then declare
  done.
- 2 more declare done while a background server/process the agent
  started is still starting (alive session, no verification).
- The Section 7.5 gate passes vacuously where no criteria are
  recorded, and the self-verification contrast only checks history
  mentions, never the filesystem: a wrong-location write, an empty
  file or a non-executable install slips through.

### Mechanism (default ON)

`internal/tasks/artifacts.go` (stdlib plus the package's existing
sentence/regex helpers only; no model calls, no network, no tool
execution):

1. Extraction: a deterministic pass over the literal instruction
   pulls out required artifacts — absolute and relative paths,
   file names, executables (`install to`, `compile`, `/bin/`
   locations), ports (`port 8080`, `:8080`), URLs
   (`http://localhost:8080/simple`), and output files with their
   stated content (a CSV header when the instruction names it).
   Example prose never counts: candidates after `for example`,
   `e.g.`, `such as`, pattern descriptions
   (`YYYY-MM-DD_<source>.log`), glob/bracket fragments
   (`[src][dst]`), and negated or as-is mentions are skipped; bare
   file names count only when quoted or in an imperative sentence.
2. Filesystem gate: every path-like requirement must exist
   (workdir-resolved for relative paths); outputs must be
   non-empty; executables must carry an execute bit; outputs with
   stated content must contain it. Ports/URLs pass only when the
   agent's own successful tool history exercised them (curl, client
   install, `exec_io` poll).
3. Background gate: a session that is still alive at finish blocks
   unless the instruction wants a persistent service (or the final
   report declares it intentionally persistent) AND the history
   shows the agent verified it. Unverified sessions ask for
   verify-then-stop; verified-but-undeclared sessions ask to stop
   or declare.

The engine seam (`applyArtifacts` in `verify_hooks.go`) merges the
pendings into the gate report before the honest-state logic, so
returns stay bounded by the Section 7.3 gate attempts and end
honest (`completed_with_pending`), never `completed`. `exec` sets
`Run.Workdir` to the project directory for relative resolution;
absolute paths are used as-is. Filesystem access is injected
(`Deps.ArtifactStat/ArtifactRead`, OS defaults) so tests run on
fakes.

### Switch

| Switch | Values | Default | Effect |
|---|---|---|---|
| `EXCALIBUR_ARTIFACT_GATE` | `on`/`1`/`true`/`yes` enables | `on` | `off` restores the pre-gate behavior (history contrast only, no filesystem or session checks) |

`EXCALIBUR_ARTIFACT_MAX` overrides the extraction cap (default 8).
Measure `off` vs `on` on the same task set; watch success rate, the
share of failures ending `completed` vs `completed_with_pending`,
gate returns per run, and false blocks (trials blocked on artifacts
the instruction only mentioned as examples).

### Research sources for this section

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, fetched full text,
  https://blog.langchain.com/improving-deep-agents-with-harness-engineering/):
  52.8→66.5 on TB2 with the model fixed. Adopted: onboarding the
  instruction's stated file paths as exact conformance targets
  ("task specs that mention file paths should be followed
  exactly"), the `PreCompletionChecklistMiddleware` exit
  interception (our finish-path merge), and loop-detection notes
  for doom loops. Nothing derived from any benchmark's tasks,
  tests or solutions; only general harness behaviour.
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026, abstract
  in the shared reference base): harness code (what to store,
  retrieve, present) moves results more than weights, with larger
  gains on weaker models. Motivates the deterministic,
  model-call-free extractor over prompt growth.
- Agentic Harness Engineering, arXiv 2604.25850 (cited from the
  architecture refs): systematic harness search with standard
  evaluation and auditing records (Section 23.2, Appendix F) as the
  objective — the gate's pendings and `gate_attempts.jsonl` are
  that signal for this lever.
- Terminus 2 (Harbor reference harness): single-tool tmux loop;
  kept as the neutral baseline shape. Our gate never learns task
  answers; it only re-checks the agent's own artifacts.
- Open-source harnesses (general engineering knowledge, no task
  material used): SWE-agent / mini-swe-agent (submit only after
  tests pass — our fresh-evidence plus filesystem rule),
  OpenHands (loop detection on repeated calls+errors),
  Codex/OpenCode `tool_choice: none` closing shape (bounded
  returns, honest terminal states), Aider (architect/editor
  split behind the expert-escalation pattern).

## W3: stronger verification before finishing (Sections 7.5, 8.7, 18.1)

Owner: strong-verification workstream. Packages: `internal/tasks`
(`strongverify.go` extends `selfverify.go`; the gate stays
tool-free) and the finish path of `internal/engine`
(`verify_hooks.go` closing hook only). The connectors are untouched:
no change to `internal/providers`, the model gateway wire protocol
(`internal/models`, `internal/gateway`), `internal/api`,
`packages/contracts`, `integrations/**`, MCP/ACP servers, or the
`bench` Harbor invocation.

### Problem evidence (read-only trial records, current version)

Automatic triage of 44 failed evaluation trials of the current
agent-quality build (tool errors down to ~0; failures now dominated
by discipline and verification):

- 13 `wrong-solution-declared-done`: the agent declares success with
  wrong results (for example an illegal move written to a file;
  generated artifacts never validated against the requested outputs).
- The W1 self-verification phase is gameable: any passing tool result
  counts as fresh evidence, so `ls -l` after a write satisfies it
  without ever running the deliverable; runs whose artifacts are
  created by `exec` itself (redirections, builds, downloads, with no
  `write_file` marker) pass vacuously.

### Mechanisms (all default ON)

`internal/tasks/strongverify.go`, wired into `ApplySelfVerify` and
the engine closing hook (`selfVerify` runs it only when the
deterministic checks would otherwise let the run complete):

1. Exercise the deliverable (deterministic): after the last change —
   a write tool or an `exec` whose arguments show a mutation
   (redirection into a file, move/copy, build, install, download;
   null-sink and stderr merges excluded) — the history must hold
   passing checks that actually exercise each produced artifact: run
   the program/CLI with representative inputs, import the module and
   call it, query the service, or parse the output file and check
   the stated format. Mere observations (`ls`, `test -e`, bare
   `cat`, `read_file`) and writes never count, however successful.
   Every command draft's artifact must be named by at least one
   exercising call (full key, basename, or module-stem word such as
   `import filter`); every stated output-format draft must be parsed
   or validated by one (not just mentioned in prose). Gaps return
   the agent to work with the suggested checks. Count and
   constraint drafts keep Section 18.1 inspection semantics: the
   contrast check and the independent verifier judge them, since no
   deterministic rule can tell a real semantic check from a
   coincidental keyword mention.
2. Independent verifier (one cheap verifier-role call per finish
   attempt): a fresh small context — literal instruction, drafts,
   commands run, short evidence lines, change diff, final-answer
   excerpt, never executor reasoning — asks for the verifier's own
   small checks plus a per-draft PASS/FAIL verdict with reasons. The
   prompt forbids looking for, naming, or running any hidden test
   suite or grading script: the verdict judges ONLY the instruction
   text. A FAIL verdict appends `independent verifier disagreement:
   ...` and returns the agent to work; anything else (pass, model
   error, unparseable reply) falls back to the deterministic outcome
   so the verifier can never block on format alone. Attempts stay
   bounded by the same gate attempts as every other pending
   (`GateAttempts`); disagreement ends in the honest `blocked` state
   per Section 7.3. The daemon big-task path keeps its Section 8.7
   orchestrator-spawned subagent: the embedded pass stands down when
   the gate input already requires that verdict, and stays silent
   when there are no drafts (nothing checkable, no extra call).
3. Suggested checks now exercise by default: path/file draft
   suggestions run script artifacts with their interpreter
   (`python3`, `node`, `bash`, `Rscript`, ...) instead of only
   probing existence; unknown artifacts keep the existence probe.

### Switches

| Switch | Mechanism | Off restores |
|---|---|---|
| `EXCALIBUR_STRONG_VERIFY` | Master switch for both mechanisms below | W1 self-verification behavior exactly |
| `EXCALIBUR_STRONG_VERIFY_EXERCISE` | Deterministic exercise-the-deliverable layer | Freshness-only checks (`ls` counts again) |
| `EXCALIBUR_STRONG_VERIFY_VERIFIER` | Independent verifier pass on finish | Deterministic pass completes the run |

`EXCALIBUR_STRONG_VERIFY` accepts `1/true/yes/on` and
`0/false/no/off`; unset keeps the configured default (all ON).

### Contract and backend tests

- `internal/tasks/strongverify_test.go`: exercising-call table (run,
  import, test suites, direct exec, curl, jq, grep-checks, builds
  count; `ls`/`test -e`/`stat`/bare `cat`/`echo`/`read_file`/writes/
  failures/empty/still-running do not), shallow-after-write blocked,
  exec-mutation hole closed (legacy vacuous pass documented, strong
  blocks, validating run passes), format-draft validation required,
  import-stem coverage, master-off legacy restore, prompt carries
  instruction+drafts+commands with the hidden-tests ban and no
  executor reasoning, verdict parser (pass/fail/gibberish-fallback/
  bare-FAIL), env overrides, runner suggestions.
- `internal/engine/strongverify_test.go`: finish blocks a bare `ls`
  after a write (note demands exercising, bounded 2 calls);
  verifier disagreement returns to work with reasons then ends
  `blocked` after two rounds (4 calls: final+verifier each);
  gibberish verifier reply falls back to pass; daemon-owned verdict
  spends no extra call; master off restores shallow completion.
- Updated to the new contract: `verify_test.go` passing and refine
  cases now include the verifier call (final+verifier, final+refine+
  verifier); no other suite changed — full `go test ./... -race`
  green, cross builds (`GOOS=darwin/windows`) green, no bare `go`.

### Measurement

- Ablate each switch off/on over the same task set (Appendix F):
  success rate, share of failures ending `completed` (wrong success
  should move to `completed_with_pending`/`blocked` with pendings
  naming the gap), verification returns per run, extra cost (at most
  one verifier call per passing finish attempt, none when no drafts).
- Records to inspect: `gate_attempts.jsonl` (pendings naming
  `not exercised` / `independent verifier disagreement`),
  `metrics.json` (`gate_attempts`), the ATIF trajectory (the
  injected `self-verify before finishing` note), `tool_calls.jsonl`
  (an exercising run/import/query/parse after the last change).

### Research sources

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, fetched full text,
  https://blog.langchain.com/improving-deep-agents-with-harness-engineering/):
  52.8→66.5 on TB2 with the model fixed. Adopted: the
  build-verify-fix loop with a `PreCompletionChecklistMiddleware`
  that intercepts the exit and forces a verification pass against
  the task spec; testing over self-reading ("agents confirm their
  code looks ok and stop"); time-budget warnings. Our exercise
  layer is the deterministic half of that checklist (the check must
  run the deliverable, not just observe it); the verifier pass is
  the checklist's second opinion. No task content used.
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026, fetched
  abstract, https://arxiv.org/abs/2603.28052): harness code (what
  to store, retrieve, present) moves results as much as weights,
  with larger gains on weaker models. Motivates keeping the
  verifier context small and fresh (instruction + artifacts only)
  instead of growing the prompt.
- Agentic Harness Engineering, arXiv 2604.25850 (cited, not
  fetched): same direction — systematic harness search with
  standard evaluation and auditing records (Section 23.2, Appendix
  F) as the objective signal; pendings text stays greppable so the
  triage loop can mine it.
- Architecture evidence refs: [E][JudgeBench] (a cheap model is
  never the only judge of code — the deterministic exercise layer
  carries the gate, the verifier only adds disagreement reasons),
  [E][MuseCode-SDK] (a checker that rejects "done" claims without
  proof — the exercise rule), [E][Selección] (candidate selection
  by test execution, never by a judge — the verifier writes checks,
  it does not grade prose).
- Open-source harnesses (general engineering knowledge, no task
  material used): SWE-agent/mini-swe-agent submit-only-after-tests
  discipline (informs the fresh-evidence-after-last-change rule);
  OpenHands loop detection on repeated calls+errors; Codex/OpenCode
  `tool_choice: none` closing shape (verifier speaks last, inside
  the existing closing hook, without changing the tool protocol);
  Aider architect/editor split (verifier never sees executor
  reasoning).

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

- `internal/tasks/artifacts_test.go`: extraction across many
  instruction styles (install paths with executable kind,
  called-file without import-usage leakage, move-file with example
  moves skipped, server port+URL, CSV output with header,
  pattern/example prose skipped, relative and bare files,
  imperative-only bare names, dedupe and cap, out-of-range ports);
  filesystem checks (missing, empty, unexecutable, content
  mismatch, workdir-relative resolution, pending cap); port/URL
  history evidence; live-session lifecycle (close settles,
  earlier-alive still blocks); verified persistent servers pass
  (instruction intent or final declaration); the off switch and
  the legacy empty-input passthrough; env overrides.
- `internal/engine/artifact_verify_test.go`: missing file blocks
  and returns bounded (2 model calls, honest pendings naming the
  artifact); existing file passes; switch off passes missing;
  unexecutable install blocks while `chmod +x` passes; an earlier
  live session blocks even when the last result settled; closed
  sessions pass; an untouched port blocks.
- Existing suites unchanged and green: self-verification
  (`selfverify_test.go`), engine lifecycle/e2e/crash
    (`go test -race ./internal/engine/`), CLI exec surface
    (`internal/cli`).

## W4: attempts fix — full budgets, merged records, honest pause (Section 18.2, owner decision 0052)

Owner: parallel-attempts fix workstream. Packages: `internal/attempts`
(config, full-budget switches), `internal/cli` (`attempts.go` fan-out,
`exec.go` pause reason, `attempts_test.go` end-to-end),
`internal/eval` (`TrialRecorder` attempt tags). The connectors are
untouched: no change to `internal/providers`, the model gateway wire
protocol, `internal/gateway`, `internal/api`, `packages/contracts`,
`integrations` (Harbor adapter, client templates), the MCP and ACP
servers, or the `bench` Harbor invocation.

### Problem evidence (read-only trial records, attempts3 run)

From `analyze.py` over the attempts3 run (74 trials, 25.7% pass) and
direct inspection of per-trial records:

- About half the failures end `paused` (`paused [best of 3 attempts]`,
  exit 4) instead of completing: every attempt hit its sliced step cap
  (`DivideSteps(25,3)=9`) after 9 model calls and paused before
  verification, so the winner was an arbitrary paused attempt.
- The outer `excalibur/tool_calls.jsonl` and `model_calls.jsonl` were
  missing (all calls lived only under `attempts/attempt-i/excalibur/`),
  so standard evaluation and auditing read zero work from the outer
  record.
- The paused winner carried an empty reason and the bare summary
  `paused`, propagated as `paused [best of 3 attempts]` with no reason
  in `final.json`.

### Mechanisms (default ON)

- Full per-attempt budgets: each attempt gets the full parent steps and
  explicit USD cap so it runs to its own verification; the wall clock
  stays shared via the parent context. The legacy `ceil(parent/N)`
  split starved attempts and is kept only behind the switch.
- Merged outer records: after the fan-out joins, every attempt's
  `model_calls.jsonl` and `tool_calls.jsonl` rows are appended into the
  outer `excalibur/` files with an explicit `attempt` index (0 stays
  explicit), blobs are copied, and gate checks gain `fanout_attempt`.
- Honest pause: the engine-budget pause path sets reason `budget` with
  summary `paused: budget exhausted: ...`, and a bare paused winner
  maps to reason `budget` and summary
  `paused: budget exhausted [best of N attempts]`.

### Switches (attempts fix)

| Switch | Type | Default | Off restores |
|---|---|---|---|
| `EXCALIBUR_ATTEMPTS_FULL_BUDGET` | env 0/1 | 1 (ON) | legacy `ceil(parent/N)` step/budget split |
| `EXCALIBUR_ATTEMPTS_MERGE_RECORDS` | env 0/1 | 1 (ON) | legacy outer-only records (no merge) |

`EXCALIBUR_ATTEMPTS` itself stays default 1 (off) until measured.

### Research sources (attempts fix; fetched before coding, general engineering only)

- Prior workstream notes in this file (W2 attempts): LangChain harness
  engineering (self-verification, loop detection), Meta-Harness
  (environment snapshot, larger gains on weaker models), Agentic
  Harness Engineering, Terminus 2, and open-source harnesses
  (OpenHands, SWE-agent, mini-swe-agent, Codex CLI, OpenCode, Aider)
  for prompts, tool design and stopping criteria. No Terminal-Bench
  task contents, tests or solutions were used.

Documentation describes standard evaluation and auditing records
(Section 23.2, Appendix F); nothing tunes prompts or skills from
benchmark tasks, tests or solutions.

### Contract and backend tests (attempts fix)

- `internal/attempts`: `StepsForAttempt`/`BudgetForAttempt` full vs
  legacy split, switch on/off (`config_test.go` via
  `internal/cli/attempts_test.go` budget test).
- `internal/cli`: container-like end-to-end
  `TestAttemptsFullBudgetCompletesWhereSplitWouldPause`
  (`EXCALIBUR_ATTEMPTS=3`, `MaxSteps=6`, 3-step write/read/done script:
  completes, winner applied, outer records tagged 0/1/2) and
  `TestAttemptsLegacySplitPauses` (same setup with the full-budget
  switch off ends paused with reason `budget`).
- `internal/eval`: additive `Attempt`/`FanoutAttempt` fields; old rows
  without them still parse.
- Full quality bar green: `gofmt -l .`, `go vet ./...`,
  `staticcheck ./...`, `go test ./... -race`, `GOOS=darwin/windows
  go build ./...`, `scripts/check-no-bare-go.sh`.
- Live end-to-end (own small tasks, never Terminal-Bench): `excalibur
  exec --profile eval` with `EXCALIBUR_ATTEMPTS=2` on hello-file and
  csv-sum tasks completes with merged attempt-tagged records.

## W4: full model context window (Section 11.4, owner decision 0051)

Owner: context-window workstream. Packages: `internal/ctxcompiler`
(limits table, trigger, controller), `internal/config` (layered
defaults + `EXCALIBUR_CONTEXT_CAP` alias), `internal/engine`
(builder wiring, monotonic Seq, run compaction state),
`internal/cli` (`ctxConfigForRun`, effective-window records),
`internal/eval` (profile window row + hash). The model gateway wire
protocol, `internal/providers/**`, `internal/gateway`,
`internal/api`, `packages/contracts`, `integrations/**`, the MCP/ACP
servers and the bench Harbor invocation are untouched. Evaluation
records are standard evaluation and auditing (Section 23.2,
Appendix F).

### Problem evidence (read-only trial records, current version)

- The 500 000-token ceiling compacts far earlier than the reference
  model requires (`gpt-6-luna`: context 1 050 000, max input
  922 000, max output 128 000, source models.dev).
- `CompactionThreshold` / `ShouldCompact` are called only in tests;
  the embedded `exec`/`eval` path never invokes masking or
  compaction (only synthetic `final` / `engine history length`
  markers land in `masking.jsonl` / `compaction.jsonl`).

### Mechanisms (all default ON)

1. Limits table with per-model overrides: shipped `gpt-6-luna` row,
   conservative fallback (200 000 / 128 000 / 16 384) for unknown
   models, `[context.model_limits]` + `EXCALIBUR_CONTEXT__*` env
   overrides by lowercased id.
2. Trigger = max input - (reserve + safety): reserve
   (`CompactionReserveTokens`, default 16 000) plus 10% safety
   margin; the old ceiling is now the optional cap
   (`CompactionCeilingTokens` 0 = model maximum,
   `EXCALIBUR_CONTEXT_CAP` alias, smallest positive wins).
3. Embedded masking (11.3) before the first compile and state-only
   compaction (11.4, no model call to compact) after it, inside
   `engine.DefaultBuilder.BuildRequest`. Only activity + L3 block
   change; L0-L2 stay byte-identical. History Seq stays monotonic.
4. Profile hash and `profile.json` report the effective window;
   reference runs keep the frozen hash, unknown models move it to
   the    conservative row.

### Switches (context window)

| Switch | Type | Default | Off restores |
|---|---|---|---|
| `EXCALIBUR_CONTEXT_FULL_WINDOW` | env `0/false/no/off` disables | on | legacy `min(500 000, 0.85*window-reserve)` |
| `EXCALIBUR_CONTEXT_MASK` | env | on | no embedded masking |
| `EXCALIBUR_CONTEXT_COMPACT` | env | on | no embedded compaction |
| `EXCALIBUR_CONTEXT_CAP` | env int / `context.compaction_ceiling_tokens` | unset (model maximum) | n/a (cap only lowers) |

Measure each switch off against on with the same model, effort and
task set (Appendix F); keep a mechanism only if it does not regress
the sample. Research: LangChain harness engineering (self-verify,
tracing, loop detection, context engineering), Meta-Harness (arXiv
2603.28052: snapshot removes wasted actions, larger gains on weaker
models), Agentic Harness Engineering (arXiv 2604.25850), Terminus 2,
OpenHands / SWE-agent / mini-swe-agent / Codex CLI / OpenCode /
Aider (prompts, tool design, stopping criteria). No benchmark task
contents, tests or solutions were used.

### Contract and backend tests (context window)

- `internal/ctxcompiler/limits_test.go`: reference row, fallback,
  overrides, per-model thresholds, env cap, default-ON switches.
- `internal/ctxcompiler/controller_test.go`: synthetic 40-result
  run crosses masking + compaction, recompiled total stays under
  max input, L0-L2 prefix stable.
- `internal/ctxcompiler/compaction_test.go`: updated full-window
  trigger (`821608` for 922K/8192, `111200` for 128K/4000, cap,
  clamp, margined estimates).
- `internal/engine/builder_context_test.go`: `BuildRequest`
  invokes masking + compaction with hooks and persists run state;
  off switches fire nothing.
- `internal/config/context_cap_test.go`: `EXCALIBUR_CONTEXT_CAP`
  alias, uncapped default, bad values ignored.
- `internal/eval/profile_window_test.go`: profile window row, hash
  moves with window, `profile.json` records, compiler cross-check.
- `internal/cli/exec_test.go`: `TestExecArtifacts` expects the
  effective (stub-conservative) hash and `profile.json` window row.
- Existing suites green: `go test -race ./internal/ctxcompiler/
  ./internal/config/ ./internal/eval/ ./internal/engine/
  ./internal/cli/`; cross builds `GOOS=darwin/windows`.

## W5: long-horizon persistence (Sections 7.3, 18.4, P1-P10)

Owner: long-horizon workstream. Packages: `internal/tasks`
(`longhorizon.go`, engine-independent) plus the finish/step seam of
`internal/engine` (`longhorizon.go`, `Run` milestone fields,
`Deps.LongHorizon`, `finishTurn`/`step`/`harvestBatch` hooks) and the
L5 pacing/discipline wiring in `internal/cli/exec_engine.go`. The
connectors are untouched: no change to `internal/providers/**`, the
model gateway wire protocol (`internal/models`, `internal/gateway`),
`internal/api`, `packages/contracts`, `integrations/**`, MCP/ACP
servers, or the `bench` Harbor invocation. Evaluation records are
standard evaluation and auditing (Section 23.2, Appendix F).

### Problem evidence (read-only trial records, never task contents)

From `analyze.py` over the frozen full run (275 trials, 31.3% pass)
and the current agent-quality build:

- On hour-scale tasks the agent averages 11-23 tool calls and declares
  `completed`; on 2.1 failures it averages ~13 calls. Paired
  measurements show earlier harness changes did not make the agent work
  longer or deeper: ALL failures still end as `completed` while the
  result is wrong.
- The existing self-verification and artifact gates block completions
  without fresh executable evidence, but they pass vacuously when no
  criterion was ever extracted and they give the model no sense of how
  much budget is left or what remains to be done.
- Long commands block the run or end it instead of backgrounding and
  polling: the `exec` tool supports `background` plus `exec_io
  wait_for`, but nothing steers the model to use it or blocks a thin
  finish while a session is still alive and unverified.

### Mechanisms (all default ON)

`internal/tasks/longhorizon.go` (stdlib plus the package's existing
helpers only; no model calls, no network, no tool execution) with the
engine seam in `internal/engine/longhorizon.go`:

1. Time- and evidence-aware continuation: when the agent tries to
   finish while significant budget remains (wall time when capped,
   steps otherwise) and any extracted draft lacks fresh executable
   evidence, or the deliverable was never exercised end to end after
   a change, the run continues with a concise "what is still unproven"
   message. Returns stay bounded by `MaxContinuations` (3); after the
   bound the run enters closing mode (`tool_choice: none`) instead of
   continuing. Fires both on a vacuous pass and when the honest end
   would land while time remains; legacy runs without an instruction
   stay on the pre-long-horizon path.
2. Milestone plan for large tasks: large means long literal
   instructions (`>=400` chars) or enough drafts (`>=3`). The plan is
   explore, one implement milestone per draft (capped), verify, report
   (generic 4-phase fallback without drafts), kept in task state
   (`Run.lhMilestones`), pinned to the working notes, with one initial
   "work the plan in order" note and a "milestone plan done/total; next
   milestone Mx: ..." nudge every 8 steps while a milestone is still
   pending.
3. Budget pacing: a compact `pacing: elapsed ..., steps used/max
   (... left), time ... left of ... (...% left), milestones done/total`
   line in L5 every call (elapsed always renders, other segments only
   when capped), combined with the existing BudgetState line. Fixed
   instructions teach the model to pace itself against it and not to
   finish while significant budget remains and evidence is thin.
4. Long-command discipline: `exec` calls with long patterns (builds,
   test suites, servers, installers, training, sleeps) launched without
   `background:true` earn one volatile note per step naming the
   background+poll rerun (`background:true` plus `exec_io
   wait_for`/`read`); finish while a session is still alive and
   unverified keeps blocking through the artifact gate, now with the
   time-aware continuation naming the poll.

### Switches

| Switch | Values | Default | Effect |
|---|---|---|---|
| `EXCALIBUR_LONG_HORIZON` | `on`/`1`/`true`/`yes` enables | `on` | master: `off` restores the pre-long-horizon finish path, no pacing, no milestones, no background nudges (legacy L5 bytes) |
| `EXCALIBUR_LONG_HORIZON_CONTINUATION` | `on`/`off` | `on` | continuation before `completed` |
| `EXCALIBUR_LONG_HORIZON_MILESTONES` | `on`/`off` | `on` | milestone plan plus progress checks |
| `EXCALIBUR_LONG_HORIZON_PACING` | `on`/`off` | `on` | pacing line in L5 |
| `EXCALIBUR_LONG_HORIZON_BACKGROUND` | `on`/`off` | `on` | background+poll nudges |

Tuning: `EXCALIBUR_LONG_HORIZON_MAX_CONT` (default 3),
`EXCALIBUR_LONG_HORIZON_EVERY` (default 8),
`EXCALIBUR_LONG_HORIZON_SIGNIFICANT_FRAC` (default 0.30),
`EXCALIBUR_LONG_HORIZON_MIN_STEPS` (default 5),
`EXCALIBUR_LONG_HORIZON_LARGE_*` thresholds. Measure `off` vs `on`
on the same task set; watch success rate, tool calls per run (must
grow on long tasks), share of failures ending `completed` vs honest
states, continuation trips per run, and cost. See
`docs/decisions/0053-long-horizon-persistence.md` for the vacuous-pass
and read-only-task choices.

### Research sources (fetched before coding, general engineering only)

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, fetched full text,
  https://blog.langchain.com/improving-deep-agents-with-harness-engineering/):
  52.8→66.5 on TB2 with the model fixed. Adopted: build-verify loop
  discipline with a `PreCompletionChecklist`-style exit interception
  (our continuation is that checklist with a time-aware bound),
  deterministic context injection for verification, environment
  onboarding, loop-detection reconsider notes, time-budget warnings,
  and the trace-analyzer warning against single-task overfitting
  (hence paired per-task statistics via the measurement workstream).
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026, fetched
  abstract, https://arxiv.org/abs/2603.28052): harness code (what to
  store, retrieve, present) moves results as much as weights, with
  larger gains on weaker models. Motivates the milestone plan kept in
  task state and the pacing line (small stable context, explicit
  budgets) over prompt growth.
- Agentic Harness Engineering, arXiv 2604.25850 (cited, not fetched):
  same direction — systematic harness search with standard evaluation
  and auditing records (Section 23.2, Appendix F) as the objective
  signal; continuation trips and milestone progress are that signal
  for this lever.
- Terminus 2 (Harbor reference harness): single-tool tmux loop and
  the missing-binary failure mode noted in the Section 23.2 analysis;
  kept as the neutral baseline shape. Our background+poll discipline
  is the same instinct made explicit (background long commands, poll
  instead of busy-polling, never finish while alive and unverified).
- Open-source harnesses (general engineering knowledge, no task
  material used): mini-SWE-agent FAQ
  (https://mini-swe-agent.com/latest/faq/ — bash-only tools suffice,
  stateless subprocess execution motivates background sessions over
  blocking calls); SWE-agent (short system prompt with explicit test
  running — informs the verify-each-milestone rule); OpenHands loop
  detection (repeated calls+errors); Codex/OpenCode `tool_choice:
  none` closing (our post-bound closing mode); Aider architect/editor
  split. What we took is behaviour-side only: plan-then-verify
  discipline, explicit pacing, and bounded returns with honest
  terminal states.

## W6: explicit finish protocol (Sections 7.3, 7.5)

Owner: explicit-finish workstream (branch `explicit-finish`).
Packages: `internal/engine` (`explicit_finish.go`, step loop),
`internal/tools` (`finish.go` control tool), `internal/cli`
(`exec_engine.go`, `exec.go` wiring and summary). The connectors are
untouched: no change to `internal/providers/**`, the model gateway
wire protocol (`internal/models`, `internal/gateway`),
`internal/api`, `packages/contracts`, `integrations/**` (Harbor
adapter, client templates), the MCP and ACP servers, or the `bench`
Harbor invocation. Evaluation records are standard evaluation and
auditing (Section 23.2, Appendix F).

### Problem evidence (read-only trial records, current version)

From the director's analysis of the current build: ~29% success;
ALL failures end as `completed` (success declared on wrong results);
the run ends when the model emits an assistant turn without tool
calls (e.g. the agent ran its own check, got `exit: 1` with an
ImportError, and the very next turn ended as `completed`). The final
model text is not even recorded: the trajectory shows a
harness-written `completed: <instruction text>`.

### Mechanism (default ON)

1. Runs end ONLY through `finish` (`summary`, `evidence`,
   `status: completed|blocked`, `reason` when blocked) in
   non-interactive profiles (every `exec` run: `default`,
   `delegate`, `eval`). A text-only turn never ends the run: the
   engine stores the raw text, injects `No tool call. Continue
   working with tools, or call finish with evidence if everything is
   verified.`, and counts the streak; after 3 in a row it enters
   closing mode (tools stay available so `finish` remains callable).
2. `finish` alone per turn (mixed batches refused without executing).
   Validation: argument shape, then the agent's own last non-finish
   command must not have errored, then the full verification (gate +
   self-verify + required artifacts + strong verifier, same checks
   as the old finish path) must pass. Refusals inject the reason and
   are unbounded by `GateAttempts`; only the time/step budget bounds
   them.
3. Confirmation round: the first accepted `finish` arms the confirm
   (`Re-read the instruction and confirm each requirement with
   evidence, or continue working. Call finish again to end the
   run.`); the second accepted `finish` completes (Running ->
   Verifying -> terminal, honest states preserved).
4. `blocked` only via `finish` (`status: blocked` + concrete reason);
   paths in the reason (via the artifact extractor) are stat'ed and
   the claim is refused when any exists.
5. Records: `Run.Summary` is `finish.summary` (never the instruction
   echo); `explicit_finish.json` (switch, finish calls, refusals,
   streak, summary, evidence, status, reason, raw args, final text)
   lands next to `skills.json`/`explore.json` plus an
   `explicit_finish` event.
6. `finish` is a harness control tool, not a benchmark tool: the
   definition rides the model prefix when the switch is on and the
   eval gate always allows it, but the frozen Section 23.2 table
   (`DefaultEvalProfile`, `EffectiveTools`) and its hash never move
   (see `docs/decisions/0055-explicit-finish.md`).

### Switch

| Switch | Values | Default | Effect |
|---|---|---|---|
| `EXCALIBUR_EXPLICIT_FINISH` | `on`/`1`/`true`/`yes` enables | `on` | `off` restores the legacy text-finish behaviour (gate on text-only turns, `completed: <instruction>` echo, no `finish` in the prefix) |

Measure `off` vs `on` on the same task set; watch success rate, the
share of failures ending `completed` vs honest states, finish
refusals per run, and text-only streaks in the records.

### Research sources for this section

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, https://blog.langchain.com/improving-deep-agents-with-harness-engineering/):
  52.8 to 66.5 on TB2 with the model fixed. Adopted: the
  pre-completion verification checklist (our refusal checks are that
  checklist enforced at the step loop), loop-detection reconsider
  notes, and the trace-analyzer warning against single-task
  overfitting (hence paired per-task statistics via the measurement
  workstream, not tuned prompts).
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026,
  https://arxiv.org/abs/2603.28052): harness code (what to store,
  retrieve, present) moves results as much as weights, with larger
  gains on weaker models. Motivates the code-side finish gate over
  prompt growth.
- Agentic Harness Engineering, arXiv 2604.25850: same direction —
  systematic harness search with standard evaluation and auditing
  records (Section 23.2, Appendix F) as the objective signal.
- Terminus 2 (Harbor reference harness): single-tool tmux loop with
  a `task_complete`-style confirmation round. Our two-turn
  confirmation mirrors that shape.
- Open-source harnesses (general engineering knowledge, no task
  material used): SWE-agent / mini-swe-agent explicit `submit` only
  after tests pass (our `finish` with evidence), OpenHands loop
  detection on repeated calls+errors, Codex/OpenCode `tool_choice:
  none` closing shape (we keep tools available in closing so
  `finish` remains callable), Aider architect/editor split behind
  expert escalation.

Documentation describes standard evaluation and auditing records
(Section 23.2, Appendix F); nothing here tunes the agent against
benchmark failures or derives prompts from task contents.

### Contract and backend tests

- `internal/tasks/longhorizon_test.go`: defaults and env overlay,
  large-task heuristic, plan shape (explore/impl/verify/report,
  draft-free fallback), continuation gaps (empty history, write
  without check, exercised pass), significant-budget thresholds,
  pacing format (capped and uncapped), long-command table and hint,
  unproven-note shape, milestone progress and nudge.
- `internal/engine/longhorizon_test.go`: shallow finish with time
  left continues past the gate bound (trips recorded, note names the
  gap); little time left stands down; master off restores the legacy
  2-call bound; large tasks pin the plan to working notes while small
  tasks stay plan-free; progress checks nudge every N steps; pacing
  renders with milestone counts and stays silent with the switch off;
  long commands nudge background+poll while short/backgrounded calls
  stay silent; bound then closing forces `tool_choice: none`.
- `internal/cli/exec_longhorizon_test.go`: pacing plus discipline
  lines ride L5 by default; master off stays silent; large requests
  carry pacing.
- Existing suites unchanged in contract and green:
  `go test -race ./internal/engine/ ./internal/tasks/
  ./internal/cli/` (the `exec_envtime` silent-baseline test now
  disables both time and long-horizon switches; e2e call counts
  unchanged because draft-free read-verified work no longer counts as
  unproven).
- `internal/engine/explicit_finish_test.go`: text-only never
  completes (continuation + streak + forced closing, summary from
  `finish`), failed-check-then-completed refused then recovered,
  confirmation round (second summary wins), `finish`-alone
  enforcement (mixed batch executes nothing), blocked completes as
  `blocked/missing_data`, existing-path blocked refused, missing
  artifact refused until the step budget pauses (unbounded refusals),
  switch OFF restores single-final completion, invalid args refused.
- `internal/cli/explicit_finish_test.go`: exec summary is
  `finish.summary` (never the instruction echo),
  `explicit_finish.json` pins the switch and arguments, `finish`
  rides the prefix when on and hides when off, switch OFF restores
  the legacy echo, failed-check then completed end to end through
  the real pipeline.
- Updated to the new contract: `exec_engine_test.go` (write/read,
  surface clamp, deferred, skills now end with the two-turn finish),
  `exec_envtime_test.go` / `exec_explore_test.go` (clock/brief turns
  plus finish), `exec_retry_test.go` (transient retries plus the
  finish confirmation), `attempts_test.go` (4-step
  write/read/finish/finish per attempt), `acp_test.go` (summary
  presence without the echo).
- Existing suites unchanged and green: full `go test ./... -race`,
  `gofmt -l .`, `go vet ./...`, `staticcheck ./...`,
  `GOOS=darwin/windows go build ./...`,
  `scripts/check-no-bare-go.sh`.
- Live end-to-end (own small tasks, never Terminal-Bench):
  `excalibur exec --profile eval` on hello-file and csv-sum style
  tasks completes with `finish` in the prefix and
  `explicit_finish.json` recording the accepted arguments
  (credentials file, `gpt-6-luna` low effort; no paid trial beyond
  these checks).

## W5: skills that help small models (cards, not catalogs)

Owner: skills workstream (branch `skills-redesign`). Packages:
`internal/skills` (`mode.go`, `cards.go`, `skilltools.go`,
`builtin/...`) plus the existing minimal additive seam in
`internal/cli` (`exec_engine.go`, `exec_cards.go`). Connectors,
gateway, API, bench invocation and the eval profile table are
untouched. Decisions: `docs/decisions/0054-skills-cards-redesign.md`;
contract: `docs/development/skills.md` (rationale section).

### Problem evidence (read-only trial records, paired runs)

All-on (74 trials, 40.5% pass) vs skills-off (74 trials, 47.3%):
enabling the expanded library with auto-suggest lowers success by
about 7-10 points. Triage of failures found knowledge gaps in only
1 of 44 failures; the dominant causes are discipline and
verification. Four mechanisms explain the drag:

1. Prompt bloat on every call: the L1 catalog (~13K chars) plus 3
   suggestions ride the stable prefix (first-call input ~4552 vs
   ~1698 tokens; ~43K extra input tokens per trial).
2. Almost nobody pulls: 11 of 74 on-trials called
   search/load at all; 63 paid the catalog cost untouched.
3. Suggestions mislead: lexical neighbours beat task neighbours
   (chess got `docker-containers`; an R task got
   `go-modules-toolchain`).
4. On-demand use wastes steps: bare `search_skills {}` dumped all
   256 skills inline; half the loads errored or loaded an
   irrelevant browser skill.

### Mechanisms (default ON)

1. Cards mode (`EXCALIBUR_SKILLS=cards`, the new default): no
   catalog and no suggestions injected in any prompt layer; the
   only skills text is a one-line on-demand pointer (<400 chars)
   on L1. `search_skills`/`load_skill` stay wired (pull-only).
2. Error-triggered retrieval: a failed tool result is matched
   against a deterministic precision-first table (concrete error
   strings, missing-binary names; at most one card per failure)
   and gains a compact excerpt (title, when-to-use line,
   Procedure/Pitfalls/Verification, capped at 40 lines / 4000
   chars, with a `load_skill` pointer). Clean outputs, running
   sessions and runner errors never fire. `exec` failures are read
   off the `exit:` header because exec always reports StatusOK.
3. Search-dump guard (all modes): unscoped empty `search_skills`
   returns short guidance with the group list instead of the full
   catalog (StatusOK preserved).
4. Content audit: all 42 trigger-reachable skills verified
   card-grade (exact commands, pitfalls, verification; one
   template fix: `systematic-debugging` gains `## Verification`);
   advisory skills are pruned by default (never injected, never
   error-fired) rather than rewritten.

### Switches

| Switch | Type | Default | Off restores |
|---|---|---|---|
| `EXCALIBUR_SKILLS` | env `off`/`on`/`auto`/`cards` | `cards` | `off`: no index; `on`: catalog, no suggestions; `auto`: catalog + task-start suggestions (legacy bytes) |
| `EXCALIBUR_SKILLS_ERROR_CARDS` | env `0/false/no/off` disables | on | no excerpts appended (cards mode otherwise unchanged) |
| `EXCALIBUR_SKILLS_COMPACT` | env `1`/`true` | unset (full) | unchanged |

Measure `cards` vs `off` vs `on` vs `auto` on the same task set
(Appendix F): success rate, first-call input tokens, share of
trials with any skill call, error-card fire rate (the
`[skill card — error-triggered]` marker in `tool_calls.jsonl`
blobs), per-cause triage drift. Records: `skills.json` gains
`error_cards` (mode, compact, hash, count, suggestions kept).

### Contract and backend tests

- `internal/skills/cards_test.go`: trigger skills resolve, are not
  archived, carry Procedure/Pitfalls/Verification; error patterns
  fire exactly one card and clean output fires none; excerpts
  within budget with commands/pitfalls/verification/full-skill
  pointer; unknown skills build nothing; pointer brevity;
  unscoped empty search guides without dumping, group listing
  works.
- `internal/skills/quality_test.go`: default cards, unknown falls
  back to cards, gating per mode (cards: no catalog/suggestions,
  search/load available), error-cards env parsing.
- `internal/cli/exec_cards_test.go`: runner fires only on
  failures (never success/running/runner-error), byte-identical
  passthrough otherwise, switch silences; scripted `exec` run in
  cards mode injects only the pointer and records the fired card
  (`make-builds`) in ATIF plus `skills.json` (mode cards,
  error_cards on, no suggestions).
- Existing suites green: `go test -race ./...`, cross builds
  `GOOS=darwin/windows`.

### Research sources

- LangChain, "Improving Deep Agents with harness engineering"
  (Feb 2026, fetched in W1): prompt bloat dilutes the working
  method; verification checklists beat more context — motivates
  removing the injected catalog and keeping the verification
  pointer minimal.
- Meta-Harness, arXiv 2603.28052 (abstract fetched in W1): what
  the harness presents matters as much as weights, with larger
  gains on weaker models — motivates the no-inject default for the
  small reference model.
- mini-SWE-agent (surveyed in W1): minimal bash-only surface; few
  well-designed tools beat many advertised ones — motivates the
  one-line pull pointer over the 256-line catalog.
- Terminus 2 (Harbor reference harness): single-tool terminal loop
  as the neutral baseline our exec records already follow.
- Agentic Harness Engineering (arXiv 2604.25850, cited from the
  architecture refs): same direction; standard evaluation and
  auditing records as the objective signal.

Documentation describes standard evaluation and auditing records
(Section 23.2, Appendix F); nothing here tunes the agent against
benchmark failures or derives prompts from task contents.

## W7: arm comparison and interim estimates (Section 23.7, Appendix F)

Owner: arm-diff workstream (branch `agent-quality`). Packages:
`internal/eval` (`diff.go`, `interim.go`, report side only),
`cmd/excalibur` (`bench_diff.go` dispatch plus the `diff`/`interim`
branches in `bench.go`). The connectors are untouched: no change to
`internal/providers/**`, the model gateway wire protocol
(`internal/models`, `internal/gateway`), `internal/api`,
`packages/contracts`, `integrations/**` (Harbor adapter, client
templates), the MCP and ACP servers, or the `bench` Harbor
invocation. Evaluation records are standard evaluation and auditing
(Section 23.2, Appendix F).

### Problem evidence (read-only trial records, never task contents)

Paired `bench compare` answers "did arm B beat arm A?" but the arm
loop then asks two follow-ups the compare output does not answer:
which tasks flipped and why (the triage cause and evidence quote for
the failing side), and whether a still-running arm is already out of
reach (the paired estimate on tasks completed so far). Both must work
from finished or partial run directories without spending paid
trials, reusing the Section 23.7 paired bootstrap already in the
tree.

### Mechanisms

1. `excalibur bench diff <runA> <runB> [--base baseline.json]`:
   pairs two finished runs per task exactly like `bench compare`
   (only shared tasks enter the paired statistics; one-sided tasks
   are listed unpaired), reusing `BootstrapCI`, `PairedDiffCI` and
   `AcceptImprovement`. On top it reports the per-task pass-rate
   table (with the optional baseline rate per task for context), the
   flipped tasks in both directions under the same all-or-nothing
   flip convention as `compare` (`FlippedToKO`/`FlippedToOK`: one
   side passes at least once while the other never passes), with
   every failed trial on the failing side carrying its triage cause
   and evidence quote, plus the mean step (model calls) / tool-call
   / wall-time / token differences over the paired trials.
   Output: `diff.json`, `diff.md`, and a one-line stdout summary.
2. Flip triage: triage rows come from each run's `triage.jsonl`
   when present, otherwise from a lazy deterministic pass
   (`TriageRun` with `NoModel: true`, bounded by a 5-minute
   budget) so `bench diff` stays offline and credential-free.
   Trials that still have no triage row render `(no triage)`.
3. `excalibur bench interim <run> --against <run|baseline.json>`:
   the paired estimate on the tasks completed so far. The reference
   is either another run directory (paired on the completed-task
   intersection) or a baseline file
   `{"baseline_rate": {task: rate}}` shared with `diff --base`.
   Partial Harbor jobs still executing are supported:
   `LoadPartialRunTrials` reads `trials.jsonl` when present, merges
   any Harbor trial directories finished since (skipping rows
   without a readable reward, i.e. still executing), and falls back
   to `trial.json` files for quick/smoke partial dirs.
4. Early-stop verdict: STOP when the paired difference CI upper
   bound sits below the reference by more than the practical bar
   (`Diff.High < -bar`; even the optimistic end of the interval is
   worse by more than the bar), CONTINUE otherwise. Output:
   `interim.json`, `interim.md` (per-task estimate table plus the
   verdict), and a one-line stdout summary.

### Switches

| Switch | Type | Default | Off restores |
|---|---|---|---|
| `EXCALIBUR_DIFF_TRIAGE` | env (`0/false/no/off` disables) / `--no-triage` flag | on | flip rows without cause/evidence (`(no triage)`) |
| `EXCALIBUR_INTERIM_BAR` | env fraction, `--bar F` flag wins | `0.03` | different practical bar for the STOP/CONTINUE verdict |

Measure each switch off against on over the same pair of runs:
triage coverage (share of flipped trials with a cause), and the
verdict at several bars for the interim estimate.

### Contract and backend tests

- `internal/eval/diff_test.go`: clear win with flips in both
  directions (failing-side trial lists), triage attached from
  `triage.jsonl`, lazy deterministic triage from Harbor-shaped
  trial dirs (timeout cause with an evidence quote, no model),
  `NoTriage` and `EXCALIBUR_DIFF_TRIAGE=0` suppression, baseline
  file columns plus out-of-range/missing errors, no-shared-tasks
  and empty-run errors, `diff.json` round trip and Markdown
  sections.
- `internal/eval/interim_test.go`: CONTINUE on a matching
  reference run, STOP on an all-failing current run against a
  passing baseline (degenerate CI at -1), partial overlap (only
  completed shared tasks paired), Harbor partial scan (finished
  `result.json` dirs picked up without `trials.jsonl`,
  still-executing dirs ignored), missing-reference/empty-run/no-
  overlap errors, `EXCALIBUR_INTERIM_BAR` precedence (env used when
  the flag is unset, flag wins).
- `cmd/excalibur/bench_diff_test.go`: `bench diff` and
  `bench interim` (run and baseline references) end to end through
  `run()`, writing `diff.json`/`diff.md` and
  `interim.json`/`interim.md`; arity and missing-`--against` usage
  errors.
- Existing suites unchanged and green:
  `go test -race ./internal/eval/ ./cmd/excalibur/`, plus the full
  quality bar.

### Research sources (surveyed before coding, general engineering only)

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, https://blog.langchain.com/improving-deep-agents-with-harness-engineering/):
  52.8 to 66.5 on TB2 with the model fixed. Adopted: the
  trace-analyzer loop (aggregate per-trial records into targeted
  harness changes; diff flips with triage evidence are that loop's
  read side), per-task paired deltas with an unpaired-task guard,
  and the warning against overfitting single-task fixes.
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026,
  https://arxiv.org/abs/2603.28052): harness code moves results as
  much as weights, with larger gains on weaker models. Takeaway:
  the outer loop is only as good as its paired statistics and its
  early-stop signal — diff provides the former, interim the latter.
- Agentic Harness Engineering, arXiv 2604.25850: same direction —
  systematic harness search with standard evaluation and auditing
  records (Section 23.2, Appendix F) as the objective signal.
- Terminus 2 (Harbor reference harness): single-tool tmux loop
  kept as the neutral baseline shape; interim supports partial
  Harbor jobs without touching the invocation.
- Open-source harnesses (general engineering knowledge, no task
  material used): OpenHands/SWE-agent loop and stopping criteria,
  mini-SWE-agent minimal loop, Codex CLI/OpenCode `tool_choice:
  none` closing and JSON repair, Aider architect/editor split —
  behaviour-side only: paired statistics, deterministic-first
  triage, and append-only records that stay valid for killed or
  still-running trials.
## Muse family adaptation (Meta Muse models)

Owner: Muse-family workstream (branch `agent-quality`).
Packages: `internal/ctxcompiler` (`method.go` `PromptMuse`,
`DetectFamily`, `FamilyNote`, `AdaptToolDescription`, `renderCore`),
`internal/engine` (`builder.go` Muse tool-definition rewrite).
Decision record: `docs/decisions/0057-muse-family-adaptation.md`.
The connectors stay untouched (`internal/providers/**`, the model
gateway wire protocol, `internal/gateway`, `internal/api`,
`packages/contracts`, `integrations/**`, MCP/ACP servers, the bench
Harbor invocation): the variant changes prompt bytes and
model-visible tool descriptions only.

### Mechanism (default ON)

Model ids starting with `muse-` or `meta-models/` (plus
`muse-spark`/`muse-glimmer` substrings, case-insensitive) select
`PromptMuse`; empty or unrecognized ids keep the GPT default, and
the GPT reference bytes never move. For Muse runs, two things
change:

1. Working-method note (`FamilyNote`): exact-JSON arguments with no
   invented fields or target names (copy allowed values from the
   definition or the error); every parallel call answered with its
   own result in call order; never invent results; finish only via
   the `finish` tool with evidence from executed checks. It never
   sets a `Reasoning strength:` line (that would override the
   request effort) and never touches the wire protocol.
2. Tool descriptions (`AdaptToolDescription`): only `exec` and
   `finish` gain the Meta Model API discipline above; every other
   tool is byte-identical across families. The rewrite applies to
   the L0 core-tool lines (`renderCore`) and to the provider tool
   definitions (`DefaultBuilder`), only when family adaptation is
   on and the resolved model id is Muse.

### Switch

| Switch | Type | Default | Off restores |
|---|---|---|---|
| `ctxcompiler.Config.FamilyAdapt` / `engine.QualityConfig.FamilyAdapt` (`EXCALIBUR_Q_FAMILY`) | field / env | on | family-independent L0 and untouched tool definitions for every family, including Muse |

Ablate `EXCALIBUR_Q_FAMILY=0` against the default on the same
model/effort/task set (Appendix F); keep the variant only if it
does not regress Muse runs and never moves GPT bytes.

### Contract and backend tests

- `internal/ctxcompiler/method_test.go`: Muse id detection
  (`muse-spark-1.3`, case-insensitive, `meta-models/...` open
  weights), Muse note in the distinct-per-family set, Muse note
  content (exact JSON, call order, no invented results,
  finish-only-via-tool, no reasoning-strength line),
  GPT-reference byte stability (exact note literal, no Muse
  wording), `AdaptToolDescription` identity for every non-Muse
  family and for non-exec/finish tools, adapted L0 tool lines for
  Muse only, adapt-off L0 identical across GPT and Muse.
- `internal/engine/builder_quality_test.go`:
  `TestBuilderMuseFamilyWiring` (Muse id selects the Muse L0 note
  and the adapted exec/finish definitions, other tools
  unchanged; GPT definitions byte-identical with adaptation on;
  adapt-off restores identical definitions and L0 for Muse).
- Existing suites unchanged and green: ctxcompiler determinism /
  cache / compaction / masking, engine lifecycle / e2e / crash
  (`go test -race ./internal/ctxcompiler/ ./internal/engine/`).

### Research sources (fetched 2026-11-01, general engineering only)

- Meta Model API, Muse Spark Open prompting guide
  (`dev.meta.ai/docs/muse-spark-open/prompting`): roles, template
  order (defaults, system prompt, tool definitions, numeric
  reasoning strength, recipients `self`/`commentary`/tool/`user`);
  rules-first system prompts; native tool calls with parallel
  calls answered each in call order; `<|eom|>` vs `<|eot|>`
  (never stop on `<|eom|>`); reasoning always on
  (`reasoning_effort` low/medium/high/xhigh only).
- Meta Model API, recommended settings
  (`dev.meta.ai/docs/muse-spark-open/settings`): temperature 1.0,
  top_p 1.0; strength low=8 (short tool loops), medium=32,
  high=256, xhigh=512 default; `muse` reasoning/tool-call
  parsers; streaming for long generations.
- Meta Model API, tool calling (`dev.meta.ai/docs/tool-calling`):
  Responses API recommended (reasoning preserved across tool
  turns); Chat Completions supported; `tool_choice` must be
  `"auto"` (`"none"` 400s — recorded as a proposal, wire
  untouched); match `tool_call_id`s; always validate arguments
  locally.
- Hugging Face `meta-models/Muse-Spark-Open-816B-A42B` model card:
  816B/42B MoE, 1M context, agentic training (planning, sequential
  tools, failure recovery, long horizon); high/xhigh reasoning for
  coding/agentic work; TB2.1 76.4 (Terminus 2).
- Meta build-with-Muse-Spark-Open post: runtimes, hosted
  providers, early tool-calling evidence.
- LangChain harness-engineering post (Feb 2026): model-specific
  prompt tailoring; shared working-method prior art.
- Meta-Harness (arXiv 2603.28052) and Agentic Harness Engineering
  (arXiv 2604.25850): model-specific harness optima, cross-family
  transfer, gains in tools/middleware/memory.

## W8: time-based run budgets and a strict blocked policy (Sections 7.3, 18.4)

Owner: budget-and-blocked workstream (branch `budget-and-blocked`).
Packages: `internal/engine` (`timebudgetonly.go`, `strictblocked.go`,
step/pause/closing), `internal/cli` (`budgetsonly.go`, `exec.go`,
`exec_engine.go`, `attempts.go` records), `internal/eval` records only
(profile hash via `profile.sha256`, no frozen-table change). The
connectors are untouched: no change to `internal/providers/**`, the
model gateway wire protocol (`internal/models`, `internal/gateway`),
`internal/api`, `packages/contracts`, `integrations/**`, MCP/ACP
servers, or the `bench` Harbor invocation. Decision:
`docs/decisions/0058-w8-time-budget-strict-blocked.md`.

### Problem evidence (read-only trial records, never task contents)

From the director's analysis of the current agent-quality build:

- 25/74 runs end `paused: budget exhausted` (Muse: 13/13), e.g. after
  ~2 minutes and ~200k tokens on tasks with a 15-minute-8-hour limit;
  11 of those paused runs still passed, so work was cut while on track.
  Root cause: the embedded eval limit is `MaxSteps: 25` with no wall
  budget when Harbor is unknown, so the step cap binds far before the
  task deadline.
- 21-22/74 runs end `blocked` after a single failed validation
  ("validation failed, deliverable not complete") with most of the time
  unused; only 1 passed. Root cause: `finish status=blocked` was
  accepted on any reason naming a missing path, including the agent's
  own validation failure.

### Mechanisms (all default ON in eval)

1. Wall-clock-only binding (`EXCALIBUR_TIME_BUDGET_ONLY`, default ON
   in eval, OFF elsewhere; forced OFF when `EXCALIBUR_TIME_BUDGET=0`):
   the binding budget is the task wall clock (explicit `--timeout`,
   else the Harbor 90% override, else the configured arm fallback
   `EXCALIBUR_EVAL_WALL_SECS`/`EXCALIBUR_ARM_TIMEOUT_SEC`/
   `EXCALIBUR_ARM_WALL_SECS`, default 3600s) with closing at 90% (10%
   left). Token and step limits are safety caps that cannot bind
   before the wall (steps 2000, cost `EXCALIBUR_COST_CAP_USD` default
   20, tokens/tool-calls uncapped); per-call input stays bounded by the
   model's max input. The engine pauses only on the wall clock; safety
   exhaustion forces closing with a one-time note instead of pausing,
   and closing injects the one-time wall note (leave the best
   deliverable, call `finish`). Fixed instructions carry the closing
   and blocked-policy discipline. Attempts get full safety caps with
   the shared wall clock.
2. Strict blocked (`EXCALIBUR_STRICT_BLOCKED`, default ON):
   `finish status=blocked` while time remains is accepted only with
   deterministic external evidence in the agent's own history (network
   unreachable/404 after retries, missing file/tool plus a failed
   install or dead network, permission denial). Validation-failure
   claims are refused with a structured recovery prompt (failing check
   and its output, three materially different approaches, pick one,
   continue); operator-missing-data without validation language stays
   accepted. Refusals count in `FinishRefusals` plus
   `StrictBlockedRefusals` in `explicit_finish.json`.
3. Records: `budgets.json` (mode, wall/steps/cost, sources) next to the
   other artifacts and mirrored under `excalibur/`; `profile.json`
   carries `wall_secs`, `wall_source`, `max_steps`, `cost_cap_usd`,
   `cost_source`, `time_budget_only`, `strict_blocked` so
   `profile.sha256` moves with the budgets; `explicit_finish.json`
   pins `time_budget_only`, `strict_blocked`, `strict_blocked_refusals`
   and closing/safety notes; `budgets` event for the ablation.

### Switches

| Switch | Values | Default | Effect |
|---|---|---|---|
| `EXCALIBUR_TIME_BUDGET_ONLY` | `on`/`1`/`true`/`yes` enables | `on` in eval, `off` elsewhere (forced `off` when `EXCALIBUR_TIME_BUDGET=0`) | `off` restores legacy binding (steps 25, explicit `--max-steps` wins, no cost cap) |
| `EXCALIBUR_COST_CAP_USD` | float > 0 | `20` | safety cost cap per trial in time-only mode |
| `EXCALIBUR_EVAL_WALL_SECS` (`EXCALIBUR_ARM_TIMEOUT_SEC`, `EXCALIBUR_ARM_WALL_SECS` aliases) | int seconds | `3600` | arm wall fallback when no explicit/Harbor wall is known |
| `EXCALIBUR_STRICT_BLOCKED` | `on`/`1`/`true`/`yes` enables | `on` | `off` restores the legacy path-exists-only blocked check |

Measure each switch off against on with the same model, effort and
task set (Appendix F conditions); watch success rate, share of runs
ending `paused` (must be wall-only in eval) vs `blocked` vs honest
`completed`, finish refusals per run, and cost. See
`docs/decisions/0058-w8-time-budget-strict-blocked.md` for the arm
fallback, operator-missing-data compatibility, and profile-hash
choices.

### Research sources for this section

- LangChain, "Improving Deep Agents with harness engineering" (Feb
  2026, fetched full text,
  https://blog.langchain.com/improving-deep-agents-with-harness-engineering/):
  52.8-66.5 on TB2 with the model fixed. Adopted: time-budget
  warnings and closing discipline, pre-completion verification
  checklist (our finish refusals are that checklist enforced at the
  step loop), loop-detection reconsider notes, and the trace-analyzer
  warning against single-task overfitting (hence paired per-task
  statistics via the measurement workstream, not tuned prompts).
- Meta-Harness, arXiv 2603.28052 (Lee et al., Mar 2026, fetched
  abstract, https://arxiv.org/abs/2603.28052): harness code (what to
  store, retrieve, present) moves results as much as weights, with
  larger gains on weaker models. Motivates fixing the budget class in
  code (wall binding, safety caps) rather than prompt growth, and the
  small stable context plus explicit budgets over long-horizon growth.
- Agentic Harness Engineering, arXiv 2604.25850 (cited, not fetched):
  same direction - systematic harness search with standard evaluation
  and auditing records (Section 23.2, Appendix F) as the objective
  signal; continuation/refusal trips and budget records are that
  signal for this lever.
- Terminus 2 (Harbor reference harness): single-tool tmux loop with
  JSON analysis/plan/keys and a proactive context summary. Kept as
  the neutral baseline shape; our closing note mirrors its
  task-complete confirmation shape (leave deliverable, call finish).
- Open-source harnesses (general engineering knowledge, no task
  material used): SWE-agent / mini-swe-agent explicit submit only
  after tests pass (our exercising checks plus strict blocked),
  OpenHands stop on repeated identical observations, Codex/OpenCode
  `tool_choice: none` closing shape (we keep tools available in
  closing so `finish` remains callable), Aider architect/editor split
  behind expert escalation.

Documentation describes standard evaluation and auditing records
(Section 23.2, Appendix F); nothing here tunes the agent against
benchmark failures or derives prompts from task contents.

### Contract and backend tests

- `internal/engine/timebudgetonly_test.go`: switch defaults (eval ON,
  non-eval OFF, master time switch forces OFF), cost-cap parsing and
  fallback, wall sources (explicit/harbor/arm fallback), safety caps
  never pause (legacy tracker exhausts on steps while time-only stays
  OK), wall binds and closes with honest `time` reason and closing
  copy, one-time closing note through the real step loop.
- `internal/engine/strictblocked_test.go`: switch defaults, refusal of
  the generic statistics validation failure with the three-approaches
  recovery shape, acceptance of network-after-retries / permission /
  missing-plus-failed-install / operator-missing-data / no-time-left,
  switch-off acceptance, end-to-end refusal counting
  (`FinishRefusals` + `StrictBlockedRefusals`) ending `blocked`.
- `internal/cli/budgetsonly_test.go`: effective budgets (eval
  time-only wall/steps/cost sources, explicit/harbor/env overrides,
  large explicit steps kept; legacy off restores 25/uncapped and
  non-eval defaults off), `profile.json`/`budgets.json` mirroring and
  `profile.sha256` movement, `explicit_finish.json` pins, past-25
  survival (30 alternating tool turns complete instead of pausing on
  the legacy cap, with closing/blocked discipline in the prompt),
  strict refusal-then-completion plus switch-off legacy acceptance.
- Updated to the new contract: `exec_envtime_test.go` (legacy
  25-step line pinned with time-only off), `attempts_test.go`
  (legacy split pinned with time-only off), `exec_cards_test.go`
  (quick-pause path pinned with time-only off).
- Existing suites unchanged and green: full `go test ./... -race`,
  `gofmt -l .`, `go vet ./...`, `staticcheck ./...`,
  `GOOS=darwin/windows go build ./...`,
  `scripts/check-no-bare-go.sh`.
- Live end-to-end (own small tasks, never Terminal-Bench):
  `excalibur exec --profile eval` on hello-file (`hello.txt` exact,
  `budgets.json` wall 3600 arm_default steps 2000 cost 20, completed)
   and csv-sum (`numbers.csv`/`sum.txt` verified, 1 verification
   refusal, completed) with `gpt-6-luna` low effort; no paid trial
   beyond these checks.
## W9: Automatic provisioning from terminal observations

Contract: Section 13 terminal observations detect explicit missing executables
and supported language modules in initial exec output and session read, wait,
and list output. Compound commands and heredoc contents do not suppress recovery.
The sole switch remains `EXCALIBUR_AUTOPROVISION=auto|suggest|off` (default auto).
Suggest runs nothing; off adds no provisioning report.

Auto serializes provisioning, updates apt once after a successful update,
waits up to 60 seconds for dpkg locks, and uses noninteractive install flags.
Python installs and import checks select the interpreter present in the original
command/diagnostic and preserve the supplied task directory. Missing pip gets a
bounded bootstrap and retry. The output records installation failures, executable
verification failures, and the exact original command to retry explicitly.

Read-only auditing of v6-default tool diagnostics found 16 reported install
failures: these included mistaken executable identities, two dpkg lock failures,
unavailable distributions, and a wheel compilation failure. The existing
first-command fallback explains attempts to install rm, mkdir, cat, set, and a
timeout number. Delayed session observations previously had no provisioning
hook. The apt cache previously advanced on suggestions and failed updates.
No task definitions, tests, or solutions were used.

Backend coverage: `autoprovision_w9_test.go` exercises compound/heredoc
diagnostics, false positives, multiple missing dependencies, failed-update retry,
interpreter paths, task directories, pip bootstrap, and delayed session reads,
waits, and lists. `TestProvisionMinimalDebian` is opt-in through
`EXCALIBUR_PROVISION_CONTAINER=1`: it starts debian:bookworm-slim without
python3, observes a real failure, installs python3-minimal through the provisioner,
then executes Python successfully. Ordinary tests use fake installers.

Research reviewed for context (no additional mechanisms introduced):
[LangChain harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering)
emphasizes executable verification and useful environment observations;
[Meta-Harness](https://arxiv.org/abs/2603.28052) studies harness code and context
selection; [Agentic Harness Engineering](https://arxiv.org/abs/2604.25850)
describes observability and attribution.
[Terminus 2](https://harborframework.com/docs/agents/terminus-2) provides the
terminal harness reference.
[mini-swe-agent's configuration](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/config/default.yaml)
makes shell observation and explicit completion central; [Aider's prompts](https://github.com/Aider-AI/aider/blob/main/aider/prompts.py)
retain command and output together. These sources support reporting concrete
execution evidence; the implementation follows the director's specified repair.

Status session tails are covered as well. With sessions and auto provisioning,
status declares execution effects and uses the execution timeout; off/suggest or
a status tool without sessions retain the fast read-only definition.
The regression suite checks both the provisioning report and this effect metadata.

Additional reference review:
[SWE-agent's default configuration](https://github.com/SWE-agent/SWE-agent/blob/main/config/default.yaml)
requires reproduction and executable checks before submission;
[OpenCode tools](https://opencode.ai/docs/tools/) documents the shell/file tool
surface. Neither introduces additional changes in this workstream.

Validation evidence: the opt-in minimal Debian check passed (apt update,
python3-minimal install, command discovery, and Python output 42). Live checks
used gpt-6-luna with low effort and an isolated virtual environment. The
pyfiglet rendering task completed with an exact output comparison. A humanize
recovery check recorded successful installation and import verification after
removal from that isolated environment, but reached its eight-step limit before
explicit retry. A fresh boltons task recorded the initial import failure,
automatic installation using the exact virtual-environment interpreter,
successful import verification, explicit retry, file assertion, and completion.
Records remain outside the repository under /tmp/w9-live-eval-records-one,
/tmp/w9-live-eval-records-two, and /tmp/w9-live-records-three.

[OpenHands' agent implementation](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/agent/agent.py)
separates action execution, observations, and explicit finish transitions.
[Codex CLI's base instructions](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/prompts/base_instructions/default.md)
describe focused implementation and executable validation. This review did not
change completion policy, prompts, or connector behavior.

Full-output audit clarification: all 26 exec observations containing missing
python3/file diagnostics actually recorded successful installation. Three
shortened inline previews omitted the provisioning report, which remained in
the full output blob. Two status observations exposed stale missing-tool tails.
These observations do not establish that 13 trials failed because installation
never fired. Missing-tool text in a session remains historical after recovery.
Provisioning reports now precede command output in exec and session read/wait
responses so prefix-limited previews retain the recovery result.
A large-output regression asserts that the report appears near the beginning.

The Node missing-module recognizer also rejects printed source-code fixtures:
a field containing an example error string is not an actual Node diagnostic.
A dedicated regression covers this distinction.

Repository-wide test isolation: the existing walkthrough evaluation test invokes
RunExec without a fake provider. On a workstation with operator credentials,
plain tests can therefore resolve a real provider. Full-suite verification for
this workstream explicitly sets EXCALIBUR_PROVIDER=stub; targeted provisioning
tests still inject their own installers and the opt-in live checks remain
separate. The walkthrough isolation gap is reported to the director.

Final verification passed: gofmt produced no paths; go vet ./... and
staticcheck ./... passed; EXCALIBUR_PROVIDER=stub go test -race -p 2 ./...
passed across the repository; GOOS=darwin go build ./... and
GOOS=windows go build ./... passed. The no-bare-go check passed.
No new Go dependencies were added.

## W9 integration verification

Merged `autoprovision-fix` into `agent-quality`, preserving the W7, Muse,
and W8 documentation alongside the provisioning repair. The provisioning
choice is recorded in [decision 0059](../decisions/0059-autoprovision-output-repair.md);
the incoming number 0056 was already assigned to arm comparison.
Shared domain types, API schemas, core interfaces, connectors, and Harbor
invocation are unchanged. The synchronous status effect departure remains
explicit in that decision.

The Section 26.8 walkthrough now explicitly selects the stub provider so
ordinary repository verification cannot resolve operator credentials through
that test. This closes the test-isolation issue reported by the workstream;
production provider selection is unchanged.

Fresh integrated-binary checks used `gpt-6-luna` with low effort in two
isolated temporary virtual environments. Both the boltons unique-number task
and the humanize integer-formatting task completed with exit zero. Each
`tool_calls.jsonl` contains missing-import detection, automatic installation,
and successful import verification before explicit retry. Independent exact
file-content assertions passed. Records are outside the repository at
`/tmp/w9-integrate-live-one/records` and `/tmp/w9-integrate-live-two/records`.

Integrated-tree quality checks passed: empty `gofmt -l .`, `go vet ./...`,
`staticcheck ./...`, `go test -race -p 2 ./...` with no global provider
override, Darwin and Windows cross builds, and the no-bare-go check.

## W10: run-end services and strict blocked closure (Sections 7.3, 13.5)

Owner: w10-run-end-services workstream (branch `w10-run-end-services`).
Preserves required background services and refuses external-blocker claims
caused by stopping a required service or missing a reference implementation. Decision: `docs/decisions/0060-w10-run-end-services.md`.
Connectors untouched.

### Mechanisms (all default ON)

1. Detached background services (`EXCALIBUR_DETACH_BACKGROUND`, default
   ON): `exec background:true` (non-PTY) detaches into its own session
   (`setsid`, no `Pdeathsig`, stdin `/dev/null`, log under `/tmp`) so
   required servers survive `exec` exit; foreground keeps
   `Setpgid`+`Pdeathsig`. Tailed logs keep `exec_io` polling working.
   Surviving `(pid, command)` recorded in `services.json` (+ `services`
   event, mirrored under `excalibur/` in eval). The `finish`
   description carries `keep_running` guidance; fixed instructions
   teach detach survival and that killing a required process is a
   failure.
2. Strict blocked closure (existing `EXCALIBUR_STRICT_BLOCKED`, default
   ON): blocked while time remains needs deterministic external
   evidence; only the empty-history operator case stays accepted.
   Reference-solution and self-caused (kill/stop) claims are never
   external and are refused. Closing mode never accepts blocked: the
   run must end `finish status=completed` with the best deliverable.
   This intentionally changes W8's no-time-left acceptance (see the
   decision).

### Switches

| Switch | Values | Default | Effect |
|---|---|---|---|
| `EXCALIBUR_DETACH_BACKGROUND` | `on`/`1`/`true`/`yes` enables | `on` | `off` runs background sessions in the foreground process group (pre-detach behaviour, children die with the agent) |
| `EXCALIBUR_STRICT_BLOCKED` | `on`/`1`/`true`/`yes` enables | `on` | `off` restores the legacy path-exists-only blocked check (also disables the W10 reference/self-kill/closing refusals) |

Measure detach on against off with the same model/effort/task set;
watch service survival (`services.json` count, post-run curl), share
of `blocked` vs `completed`, and strict refusals.

### Research sources for this section

General engineering knowledge only (no task material used); the task
brief already cites the prior art. Implementation follows the
director's specified repair: survival via session detach (standard
Unix `setsid` daemon semantics), strict evidence gating, and explicit
`keep_running` completion discipline. Evaluation records are standard
evaluation and auditing (Section 23.2, Appendix F).

### Contract and backend tests

- `internal/tools/terminal_detach_test.go`: detach default ON, log
  under `/tmp`, pid recorded, `Surviving` lists it, tailed output
  pollable, switch-off fallback, `background:true` routes to detached
  with survival wording, `finish` description carries `keep_running`
  and the no-kill rule; `TestDetachedSurvivesParentExit` proves a
  detached HTTP server answers after its launcher exits.
- `internal/cli/run_end_services_test.go`: `TestRunEndBackgroundSurvives`
  (background server answers after `RunExec` returns, `services.json`
  records the survivor) and prompt teaching (`keep_running`, killing
  rule).
- `internal/engine/strictblocked_test.go`: W10 regressions
  `TestStrictBlockedRefusesSelfStoppedVM` (self-stopped VM service) and `TestStrictBlockedRefusesMissingReference` (missing reference claim); closing (`TestStrictBlockedAcceptsWhenNoTimeRemains`) now
  refuses blocked and requires completed.
- Existing suites unchanged and green (see final verification below).
## W11: Verification of generalisation and explicit output properties

The strong-verification closing hook now asks its existing independent reviewer
to assess generated-input evidence and every explicit instruction property.
`EXCALIBUR_GENERALIZE_CHECKS=on|off` defaults **on**, under the existing
`EXCALIBUR_SELFVERIFY`, `EXCALIBUR_STRONG_VERIFY` and
`EXCALIBUR_STRONG_VERIFY_VERIFIER` master switches. Off restores the previous
review prompt and fallback. No second verifier or tool-execution path is added.

For functions, CLIs, extractors, converters, models and solvers applied to inputs,
the reviewer must identify an executed check on at least one agent-generated
fresh input, such as a changed seed/size, random instance or edge case. Visible
reference values alone cannot establish generalisation. The reviewer receives
the literal instruction and must list its explicit properties individually,
including types, shapes, file formats, field names and ordering, even when the
bounded draft criteria omit them. Each PROPERTY entry cites its actual check;
a proposed command cannot satisfy it. The executor runs missing checks through
its normal tools. Hidden benchmark tests and reference solutions remain
explicitly forbidden.

The same call returns FRESH_INPUTS and OUTPUT_CONTRACT assessments and PROPERTY
checklist entries. Missing assessments, any failed property and verifier errors
return actionable pendings through the existing completion gate and budgets.
NA is permitted only when the relevant requirement does not apply. The reviewer
judges applicability, input novelty and coverage semantics; the harness does
not claim to prove arbitrary program correctness by matching shell keywords.
It preserves up to ten recent command/result excerpts, identifies the last
mutation boundary, and requires evidence after that boundary. The output
allowance is 1536 tokens when enabled, 512 when off. Observational
`generalization_check` entries in `journal.jsonl` preserve the review and its
pending checks without changing recovery behavior. See
[decision 0061](../decisions/0061-generalizing-verification.md).

Backend tests in `internal/tasks/generalize_test.go` exercise visible-only
reports, generated-input coverage, wrong output properties, missing checklists,
proposed checks without execution, non-applicability and both switch values.
`internal/engine/generalize_test.go` exercises the real closing-hook seam with
simulated verifier responses, checks that failures return actionable reasons,
confirms one reused verifier call, and asserts the journal report. Existing
strong-verification tests retain the legacy malformed-response fallback with
the new mechanism explicitly off. Ordinary tests never call paid APIs.

Research reviewed before implementation:

- [LangChain, Improving Deep Agents with harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering)
  reports gains from self-verification, context engineering and tracing. This
  change uses the existing verifier and evidence seam.
- [Meta-Harness](https://arxiv.org/abs/2603.28052) studies harness code and
  information presentation across applications and held-out models. Its broader
  harness search is outside this implementation's scope.
- [Agentic Harness Engineering](https://arxiv.org/abs/2604.25850) motivates
  component, experience and decision observability; the coverage report here is
  an evaluation and audit record under Section 23.2 and Appendix F.
- [Terminus 2](https://github.com/harbor-framework/harbor/blob/main/src/harbor/agents/terminus_2/terminus_2.py)
  provides terminal execution and explicit completion control; existing finish
  control remains intact.
- [SWE-agent](https://github.com/SWE-agent/SWE-agent/blob/main/config/default.yaml)
  and [mini-swe-agent](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/config/default.yaml)
  recommend reproduction followed by edge-case checks.
- [OpenHands](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/agent/agent.py),
  [Codex CLI](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/prompts/base_instructions/default.md)
  and [Aider](https://github.com/Aider-AI/aider/blob/main/aider/coders/editblock_prompts.py)
  illustrate explicit action/observation handling, focused validation and precise
  edits. No tool protocol or editing mechanism is changed here. Two attempted
  OpenCode prompt-source URLs returned 404 and supplied no implementation basis.

The supplied v9 behavior descriptions were used only to characterize shallow
verification. The external summary script found zero trials at the supplied
v9-default root on this workstation, so it supplied no additional evidence.
No benchmark task contents, tests or solutions were read or run.

A method with executable evidence is also reviewed when deterministic draft
extraction finds no paths or format keywords; its literal instruction still
supplies the properties. An engine regression covers this bypass case.

Live end-to-end checks used operator credentials through the existing gateway,
`gpt-6-luna`, low effort and `EXCALIBUR_GENERALIZE_CHECKS=on`, in three independent
temporary work directories, without Terminal-Bench runs:

- `/tmp/w11-live-two/records/excalibur`: normalization function completed with
  exit zero. The journal records an initial FRESH_INPUTS failure, followed by
  2,000 generated cases and passing property coverage.
- `/tmp/w11-live-three/records/excalibur`: row-total function completed with exit
  zero. The journal records rejection for missing fresh inputs and explicit
  list/integer type assertions. The executor then checked generated rows against
  an independent iterative accumulator, including order and empty cases, and
  the coverage report passed. The final recorded check covered 2,000 generated cases.
- `/tmp/w11-live-one/records/excalibur`: JSON statistics CLI produced correct
  normal/empty outputs but did not complete within its 180-second budget;
  final.json reports exit one and state running. This first run preceded the
  journal coverage export. Its model-call records include verifier calls; it
  is not counted as a successful end-to-end run. No unrelated timeout or tool
  routing behavior was changed.

Independent post-run assertions passed 60 additional normalization cases using
an enumeration oracle, 60 row-total cases using iterative accumulation, and
four statistics CLI inputs checking exact field order, integer types, one-line
JSON and values. The records above remain outside the repository.
The draft-free backstop recognizes general method/input vocabulary and requires
existing executable evidence before spending the review call; plain greetings,
build-only requests and text-file actions keep their existing behavior. The
reviewer still decides whether generated inputs apply. A regression covers the
method vocabulary independently of evidence-coverage judgment.

Final verification passed on the completed tree: empty `gofmt -l .`,
`go vet ./...`, `staticcheck ./...`, `go test -race -p 2 ./...`,
`GOOS=darwin go build -p 2 ./...`, `GOOS=windows go build -p 2 ./...`,
and `scripts/check-no-bare-go.sh`. Commands ran with low process priority.
No Go dependencies or connector files changed.
## W12: actionable finish refusals

The explicit finish gate returns a compact JSON checklist: `id`, `missing`,
and `check` per unmet item. Extracted artifacts use their `A` IDs and draft
criteria their `D` IDs. Other gate conditions receive stable content-derived
IDs. Each repeat contains only changed entries, resolved IDs and a concrete
next action; an attempt number distinguishes continued refusals. Reasons are
retained even when omitted from the model-visible delta.

After three refusals for the same extracted item, the verifier role reviews
its interpretation of the literal instruction. A false-requirement verdict
requires a reason and an exact instruction quote. The item is then excluded
from subsequent extraction checks for that instruction. Core gate obligations
are never eligible. Reviews fail closed on errors or malformed output and run
at most once per extracted item per instruction. Instruction changes invalidate
withdrawals. Decision 0060 records the departure from one-time refinement.

The switch is `EXCALIBUR_ACTIONABLE_REFUSALS=on|off`, default **on**. It controls
checklists, delta responses, extraction reviews and per-item records. Off keeps
the existing refusal protocol. The finish tool's history result carries the
refusal, and `finish_refusal` journal entries preserve all items and reviews
immediately. `explicit_finish.json` records `actionable_refusals` and the
`refusal_items` history for evaluation and auditing (Section 23.2, Appendix F).
No provider, gateway, API, contract, integration or benchmark invocation changes
are included.

Backend coverage in `internal/engine/actionable_refusals_test.go` replays the
observed refusal streak lengths (20, 9, 9) with invented artifacts, rather than
benchmark task material. It checks delta responses, complete item retention,
exact path matching, instruction changes, one review per item, false requirement
withdrawal, real requirement retention, invalid quotes, malformed/error fallback,
tool-result delivery, immediate journal records and the off switch. Existing
explicit-finish, artifact and verification tests remain regression coverage.

Research reviewed before implementation:

- [LangChain harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering):
  verification feedback and loop detection support returning an actionable next
  step when a completion attempt lacks evidence.
- [Meta-Harness](https://arxiv.org/abs/2603.28052): context preparation and
  environment information motivate concise, relevant guidance. This change
  implements only the director's refusal design.
- [Agentic Harness Engineering](https://arxiv.org/abs/2604.25850): observability
  motivates preserving per-item decisions for auditing; no automatic harness
  evolution is implemented.
- [Terminus 2](https://github.com/laude-institute/harbor/blob/main/src/harbor/agents/terminus_2/terminus_2.py):
  explicit completion confirmation remains the surrounding stopping protocol.
- [mini-swe-agent](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/agents/default.py)
  and [SWE-agent](https://github.com/SWE-agent/SWE-agent/blob/main/sweagent/agent/agents.py):
  execution feedback and bounded control flow inform concise corrective checks.
- [Aider](https://github.com/Aider-AI/aider/blob/main/aider/coders/base_coder.py):
  lint/test outcomes and bounded reflection reinforce execution-backed feedback.
- [Codex CLI guidance](https://github.com/openai/codex/blob/main/codex-rs/core/templates/agents/orchestrator.md):
  concrete action and explicit validation are consistent with actionable refusal
  guidance. No prompts or examples were copied.
- [OpenCode recovery controls](https://opencode.ai/docs/agents/) distinguish
  repeated tool loops; this mechanism uses per-item refusal counts instead.
- [OpenHands SDK](https://github.com/OpenHands/software-agent-sdk) separates
  agents, tools and conversation state; refusal bookkeeping likewise belongs to
  the engine rather than provider adapters.

Refusal counters, review outcomes and withdrawals are reconstructed from the
journal when an execution resumes. Audit entries carry an instruction hash so
an earlier revision cannot withdraw a requirement in a later revision. Replay
coverage checks the third-refusal threshold across restart and persists both
the withdrawal and its rationale.

Live checks on 2026-11-01 used `excalibur exec --profile eval` in two temporary
Git repositories with `gpt-6-luna`, effort `low`: a Python greeting program and
an integer-sum CLI. Both completed with two finish calls and zero refusals.
Their output was independently executed and checked, and each
`explicit_finish.json` recorded `actionable_refusals: true`. Records are outside
the repository at `/tmp/w12-actionable-final-live/logs1` and `logs2`.
An earlier smoke run exercised per-item reasons, delta feedback and third-item
reviews; genuine deliverables were retained. It exposed an existence probe
being suggested for an exercise failure, now covered by
`TestExerciseRefusalUsesExecutableCheck`. No benchmark runs were launched.

Validation: `gofmt -l .` (empty), `go vet ./...`, `staticcheck ./...`,
`go test -race -p 2 ./...`, `GOOS=darwin go build ./...`,
`GOOS=windows go build ./...` and `scripts/check-no-bare-go.sh` passed.
Heavy checks ran with `nice -n 19`. Initial deadline failures in daemon wiring
tests under shared machine load passed on retry and in the final full run.
## Final records and verified finish guidance (W12)

Exec writes `final.json` for ordinary profiles as well as the eval recorder.
SIGINT, SIGTERM and context expiry produce `stopped_by_timeout`, exit 4,
metrics and a closing ATIF marker. Cancellation snapshots are independent of
the worker and use synchronized telemetry only. The command bounds shutdown
at two seconds. SIGKILL and failed/unresponsive storage remain outside the
record guarantee. Decision 0060 documents the exec record-state departure from
Sections 6.5 and 7.2; shared engine states remain unchanged.

`EXCALIBUR_PROMPT_FINISH=on|off` defaults **on**. Before the next model call,
existing deterministic verification and artifact checks plus evidence for every
extracted criterion enable a short finish nudge. The ordinary finish gate,
confirmation and independent verification still apply. Repeating the same
successful check with unchanged output and artifacts adds a re-verification
warning. Tool-recorded mutations and artifact size/mtime changes invalidate
repeat tracking. No new verification tools or model calls run.
`prompt_finish.json`, next to the custom trajectory, records `enabled`,
`first_all_checks_pass_at` and `repeated_verification` incrementally. The first
pass time is never reset, even if later work invalidates current checks. It is
an agent-check milestone, not an external evaluation result. Record reliability
is unconditional; only advisory completion guidance has an ablation switch.

Backend coverage includes criterion omissions, failed checks, repeated evidence,
immutable timestamps, switch-off behavior, cancellation-resistant work, and real
SIGTERM/SIGINT subprocess tests. Existing timeout tests cover the new record
state and unchanged exit 4. Validation and small live terminal checks are
recorded below after execution.

Research consulted before implementation:

- [LangChain harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering)
  motivates bounded verification and short, timely stopping guidance.
- [Meta-Harness](https://arxiv.org/abs/2603.28052) describes harness state and
  context engineering; no automatic harness modification is introduced here.
- [Agentic Harness Engineering](https://arxiv.org/abs/2604.25850) motivates
  observable, attributable records. These files support standard evaluation
  and auditing under Section 23.2 and Appendix F.
- [Terminus 2](https://github.com/laude-institute/harbor/blob/main/src/harbor/agents/terminus_2/terminus_2.py)
  and [mini-swe-agent](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/agents/default.py)
  provide explicit stopping and trajectory lifecycle examples.
- [SWE-agent](https://github.com/SWE-agent/SWE-agent/blob/main/config/default.yaml)
  requires rechecking changed work and a submit confirmation; this implementation
  retains Excalibur's own finish gate.
- [Aider](https://github.com/Aider-AI/aider/blob/main/aider/prompts.py)
  preserves command results as context; this change likewise reuses recorded
  evidence instead of issuing duplicate checks.

Read-only inspection of v9 final-state metadata confirmed several interrupted
trials retained `running`; no task contents, tests, solutions or verifier files
were read.

Additional source inspection:
[OpenHands SDK](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/agent/agent.py)
keeps finish handling in the agent lifecycle;
[OpenCode](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/session/prompt.ts)
separates tool completion from assistant finish state; and
[Codex CLI](https://github.com/openai/codex/blob/main/codex-rs/core/templates/agents/orchestrator.md)
keeps task execution and concise user delivery connected. These are comparative
references, not imported prompts or code.

Small live terminal checks on 2026-11-01 used the operator credentials file,
`gpt-6-luna`, effort `low`, eval profile, fresh temporary project/log directories,
and a 90-second bound per task. Both exited 0 with `completed` records:

| Own task | Independent artifact check | First all-checks-pass (UTC) |
|---|---|---|
| Create and check a greeting text file | Exact bytes equal `hello\n` | 2026-11-01 11:15:52.483844651 |
| Create an integer-summing Python CLI | Positive inputs, mixed signs, and empty input produce the expected sums | 2026-11-01 11:16:21.421383899 |

Records remain outside the repository at `/tmp/excalibur-w12-live-jLTbhl/`.
Both `prompt_finish.json` files show `enabled: true` and a nonempty first-pass
time; model call records confirm the model and effort. These checks establish
activation and basic end-to-end behavior, not a benchmark success estimate.
No Terminal-Bench run was launched.

A third live task used the final interruption/terminal-envelope implementation:
a shell filter lowercasing stdin. It also exited 0 with `completed`, and
independent checks covered mixed case, alphanumeric text and empty input.
`normalize-records/excalibur/prompt_finish.json` records its first passing moment
at 2026-11-01 11:28:28.548239750 UTC with guidance enabled. Repeat detection is
additionally tested with different terminal session IDs and alternating checks;
only exit status and actual output contribute to the evidence signature.

Final verification passed on 2026-11-01: `gofmt -l .` produced no output,
`go vet ./...`, `staticcheck ./...`, `go test ./... -race`,
`GOOS=darwin go build ./...`, and `GOOS=windows go build ./...` all exited 0.
The final sweep used `GOFLAGS=-p=1` and low process priority on the shared
machine. `scripts/check-no-bare-go.sh` also passed. Earlier concurrent runs
encountered the existing five-second daemon review setup deadline; its isolated
recheck and the final complete suite passed without changing that test.
## W12: compatible tool routing

The contract follows Sections 13, 15, 22 and 23.2, with the explicit compatibility
changes in decision 0060. `call_tool` executes registered core or deferred tools
through the same validation, timeout, output and journal pipeline. Its name may
be top level or nested in `arguments`, including an inner `arguments` envelope.
Recursive invocation is rejected. Eval also validates wrapped targets and task
operations against its profile. `exec_io` is visible in non-interactive core
catalogs, and `get_tool_output` accepts a known terminal session reference with
the same offset/limit paging as stored output. Unknown sessions still receive
the existing retrieval hint. Subagents infer spawn from a card and otherwise
status from a run reference; explicit actions take precedence. Dispatch errors
include one-line spawn/status examples.

| Switch | Default | Disabled behavior |
|---|---|---|
| `EXCALIBUR_TOOL_FRICTION_REPAIR` | ON | Restores deferred-only calls, explicit subagent actions, blob-only retrieval and the prior non-interactive catalog |
| `EXCALIBUR_TOOL_CALL_NAME` (existing) | ON | Disables nested name promotion |

Use `0`, `false`, `off` or `no` to disable the repair switch. Trial records always
include protocol-error audit fields: `friction_error` on matching failed calls,
and total/category counts in `final.json`. Categories cover calling a core tool
through the legacy deferred wrapper, missing tool name, missing subagent action
or run, session-as-blob, and missing search query. Ordinary command failures do
not count. Counts rebuild from JSONL, including across recorder restarts and
merged attempts; partial records remain available if a trial is interrupted.

Read-only inspection of the v9-default tool records found 20 deferred-wrapper
errors, seven missing-name errors, three missing-action errors, two session/blob
errors and one missing-run error. Only protocol metadata and argument key names
were inspected; task contents, tests and solutions were not accessed.

Research consulted for this scoped implementation:

- [LangChain's harness engineering report](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering)
  discusses verification, tracing and context delivery with a fixed model.
- [Meta-Harness](https://arxiv.org/html/2603.28052v1) describes environment
  bootstrapping and state construction. No new snapshot mechanism is added here.
- [Agentic Harness Engineering](https://arxiv.org/html/2604.25850v1) describes
  component, experience and decision observability; this change adds explicit
  protocol-error categories for ordinary evaluation and auditing.
- [Terminus 2 source](https://github.com/harbor-framework/harbor/blob/main/src/harbor/agents/terminus_2/terminus_2.py)
  uses terminal sessions and structured command parsing.
- [mini-swe-agent configuration](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/config/default.yaml)
  and [SWE-agent configuration](https://github.com/SWE-agent/SWE-agent/blob/main/config/default.yaml)
  make execution and verification concrete in the tool workflow.
- [OpenHands](https://github.com/OpenHands/OpenHands) documents its execution
  backends; [Aider's prompts](https://github.com/Aider-AI/aider/blob/main/aider/coders/base_prompts.py)
  distinguish editable content and repository context. Neither changes this
  task's director-specified design.

The [Codex CLI core catalog](https://github.com/openai/codex/tree/main/codex-rs/core)
and [OpenCode prompt catalog](https://github.com/anomalyco/opencode/tree/dev/packages/opencode/src/session/prompt)
were also reviewed as reference entry points. Their previously published raw
prompt paths returned 404, so no claims or implementation changes rely on those
missing files. Prompt and stopping-policy changes remain outside this task.

Backend regression coverage lives in `internal/tools/friction_test.go`,
`internal/orchestrator/subagent_test.go`, `internal/eval/friction_test.go` and
`internal/cli/exec_engine_test.go`: reported call shapes, nested envelopes,
standard executor validation, switch-off restoration, inferred spawn/status,
real-error hints, terminal output paging, category counts across reopen, catalog
visibility, and wrapped profile restrictions. Existing deferred-tool tests now
expect core invocations to succeed.

Two live `excalibur exec` checks used `gpt-6-luna`, effort `low`, with records
outside the repository at `/tmp/w12-live/log{1,2}/excalibur`. The first executed
`call_tool` targeting `exec_io` and produced `greeting.txt` with exact bytes
`hello\n`. The second retrieved session output through `get_tool_output` and
produced `sum.txt` with exact bytes `55\n`. Both artifacts passed shell assertions.
The final friction totals were zero and one respectively; the latter was a
reference to an older session after many later commands had evicted it. Earlier
session retrievals succeeded. Both overall runs exited 1 with final state
`running` after repeated verification; these are successful tool compatibility
checks, not successful completion-state checks. Completion behavior is outside
this task's design and is reported to the director separately. No Terminal-Bench
runs were launched.

Verification completed: `gofmt -l .` was empty; `go vet ./...`,
`staticcheck ./...`, both Darwin and Windows cross-builds, and the bare-goroutine
check passed. The complete race suite passed with
`GOMAXPROCS=2 go test -race -p 1 ./...` at low process priority. Earlier
overlapping runs encountered deadline failures in unchanged daemon/gateway tests;
the serial run passed those packages and every integration suite. Targeted race
tests also passed with `-p 2`. Verification logs are outside the repository at
`/tmp/tool-friction-serial-verification.log` and
`/tmp/tool-friction-{vet-all,staticcheck,darwin,windows}-final.log`.

### W12 integration

The integrated tree retains actionable finish refusals, interruption records and
advisory finish guidance, and compatible tool routing/output retrieval. Decisions
0062, 0063 and 0064 replace the wave branches' colliding 0060 filenames; decision
0060 remains the run-end service contract and 0061 remains generalisation checks.
No shared contract, connector, MCP/ACP server or Harbor invocation changed.

The combined default-on finish guidance and actionable artifact gate initially
attempted to acquire the run mutex twice. Finish guidance now releases its
snapshot lock before invoking the shared artifact gate. A regression exercises
both switches with a real artifact and executable evidence, and checks that the
passing milestone is recorded. The wave-11 generalisation verifier and wave-12
artifact-change helper are both retained.

Two retained-record live checks of the integrated binary completed with exit
zero using the operator credential file, `gpt-6-luna` and low effort. Independent
byte assertions verified `greeting.txt` (`hello\n`) and `farewell.txt`
(`goodbye\n`). Records are under
`/tmp/integrate-w12-live/task{3,4}/.excalibur/run-*/`: both final records say
`completed`; both prompt-finish records have enabled=true and a first-passing
check timestamp; both explicit-finish records have actionable_refusals=true.
The greeting run recovered from one refusal and completed on its third finish
call; the farewell run completed on its second finish call without refusal.
Earlier ephemeral smoke attempts produced the requested greeting and total
files, but one returned `running` after repeated verification. Those removed
their records by design and are not counted as retained-record validation.
No Terminal-Bench tasks were launched or inspected.
Both retained profile records include `exec_io` in `enabled_tools`.

Final integration verification passed: empty `gofmt -l .`, `go vet -p 2 ./...`,
`staticcheck ./...`, full `go test -race -p 2 ./...`, Darwin and Windows
`go build -p 2 ./...`, `git diff --check` and the no-bare-go check. Heavy
commands ran with `nice -n 19`. The race-test log is
`/tmp/integrate-w12-race-final.log`. An initial pre-fix sweep was stopped and
replaced by this passing full run; no tests were weakened.

## W13: provider overload waits

Section 19.5 overload handling lives in the model gateway. Rate-limited calls
and HTTP 5xx responses retry until they succeed or the caller's wall-clock
budget expires. Waits remain inside one model turn and consume no extra engine
steps. Retry-After (seconds or HTTP dates), x-ratelimit-reset-* (durations,
seconds, or Unix seconds), and provider text such as "try again in 48ms" supply
the delay, with positive jitter. Multiple reset headers use the latest delay.
Without a usable server delay, exponential backoff is capped at 30 seconds.
A configured distinct fallback credential alternates with the primary on
successive gateway attempts. Keys and response headers are not recorded.

Switches: GatewayConfig.DisableBudgetRetries defaults to false. The operator
switch EXCALIBUR_BUDGET_RETRIES defaults to enabled; set it to 0 to restore
bounded overload retries and the legacy registry construction. This switch
covers delay observation and fallback alternation as part of the same policy.

Each model_calls.jsonl row now includes retry_wait_count and retry_wait_seconds
(actual elapsed wait time, including an interrupted final wait), alongside
retries. Summing the fields yields trial totals. Calls with no waits record
zero. Ordinary transport errors retain bounded retries; authentication and
input errors fail immediately. See decision 0065 for the deliberate Section
19.5 policy extension and the unchanged streaming behavior.

Backend coverage: TestOverloadServerWaits uses httptest to verify recovery after
four failures for Retry-After seconds/date, token and request reset headers,
message delays in milliseconds/seconds, and HTTP 503, including alternating
credentials and wait metadata. TestOverloadBudgetAndSwitch checks deadline
termination and the bounded comparison switch. TestRetryDelayBounds checks the
30-second cap, uncapped server waits, and simultaneous reset headers.
TestExecRateLimitWaitRecords checks recovery within a two-step budget and trial
records; TestExecRateLimitsExpireWallBudget checks paused expiration rather
than provider failure. Existing gateway tests retain transport, breaker,
content, input, authentication, routing, and accounting coverage.

Research consulted before implementation:

- [LangChain harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering)
  reports fixed-model gains from tracing, verification and context engineering.
  This change addresses infrastructure reliability only, without new prompts.
- [Meta-Harness](https://arxiv.org/abs/2603.28052) studies harness code and
  experience access. Its broader optimization mechanisms are outside W13.
- [Agentic Harness Engineering](https://arxiv.org/abs/2604.25850) motivates
  component, experience and decision observability; explicit wait fields make
  provider availability separately measurable in standard evaluation records.
- [Terminus 2 documentation](https://harborframework.com/docs/agents/terminus-2)
  and [mini-swe-agent retry code](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/models/utils/retry.py)
  were consulted as reference harness material. The director's retry design,
  rather than a borrowed stopping policy, defines this implementation.

No benchmark task content, tests, or solutions were inspected. No new dependency
was added.

W13 live smoke checks used `excalibur exec` with gpt-6-luna, low effort, and the
operator credentials file, on two original tasks in temporary directories. A
local forwarding proxy injected four initial 429 responses with a 10ms token
reset before forwarding to the real model. Both runs exited 0, with artifacts
independently checked: sum three integers into a file, and repair a Python
function with assertions for zero, positive and negative inputs. Trial one
recorded 13 model calls and five waits totaling 60.405 seconds (including an
additional upstream wait); trial two recorded nine model calls and four waits
totaling 0.045 seconds. Local records are under
`/tmp/w13-live-2f0914rm/logs1/excalibur/` and
`/tmp/w13-live-2f0914rm/logs2/excalibur/`. These are smoke checks, not benchmark
results. Read-only provider error counters from v9-default confirmed ten
rate_limited model-call records; task contents were not read.

A third smoke check on the final rebuilt binary created and independently
verified an exact text file. It exited 0 with 12 model calls and five waits
(total 60.584 seconds), again including an upstream wait after the four injected
429 responses. Records: `/tmp/w13-live-final-vdp3eln4/logs1/excalibur/`.

Final verification passed: empty `gofmt -l .`, `go vet ./...`,
`staticcheck ./...`, `go test -race -p 2 ./...`, Darwin and Windows
`go build -p 2 ./...`, and the no-bare-goroutine check. The first full race run
hit the existing daemon review-loop test's five-second deadline under shared
machine load; its isolated rerun and the subsequent full-suite rerun passed
without changes to that package.

### W13 integration

Merged `w13-rate-limit-retry` into `agent-quality`, preserving the W10–W12
documentation alongside the W13 section. The retry decision is numbered 0065
because 0060 already describes run-end services. Shared core interfaces,
provider connectors, protocol schemas, MCP/ACP servers and Harbor invocation
remain unchanged.

The initial full race sweep found a conflicting test expectation: W13 expected
the CLI timeout outcome to be paused, while W12 intentionally exposes
`stopped_by_timeout`. The regression now verifies the external timeout outcome
and exit code 4 together with a journaled engine pause for budget exhaustion.
Both behaviors are retained, as documented in decisions 0063 and 0065.

Two original live terminal tasks ran against the integrated binary with
`gpt-6-luna`, low effort and four injected 429 responses per task. Both produced
independently verified artifacts and recorded four retry waits. The sum-file
task used 18 model calls and 0.072 seconds of retry waits, but exited 1 with
state `running`; it is a completion failure, not a passing end-to-end result.
The Python repair task used 13 model calls and 0.047 seconds of retry waits,
completed with exit 0, and passed independent assertions for zero, positive and
negative inputs. Records are under
`/tmp/integrate-w13-live-3dabxik3/logs{1,2}/excalibur/`. The unresolved sum-task
closure behavior is reported for the next wave; no stopping policy was changed
in this integration. No Terminal-Bench trial was launched or inspected.

Under shared machine load, the existing daemon review-loop and WebSocket tests expired
their contexts during fixture initialization. Their deadlines now start
after daemon/database setup; the review tick and event delivery retain their
original limits and assertions. Production daemon behavior is unchanged.

Final integration verification passed: empty `gofmt -l .`, `go vet -p 2 ./...`,
`staticcheck ./...`, full `GOMAXPROCS=2 go test -race -p 1 ./...`, Darwin and
Windows `go build -p 2 ./...`, `git diff --check` and the no-bare-go check.
Heavy commands ran with `nice -n 19`. The final race log is
`/tmp/integrate-w13-race-pass.log`; earlier sweeps exposed the reconciled CLI
expectation and fixture setup deadlines described above.
## W16: exhausted provider credentials

The Section 19.5 model-layer observer permanently quarantines credentials on
explicit insufficient-quota, billing or credit exhaustion (including HTTP 402).
A transient 429 remains retryable. If the alternating fallback is exhausted,
the same gateway attempt returns to the working primary; later attempts and
new provider instances skip the quarantined key. Quarantine lasts until
process exit, is concurrency safe, and logs a fixed warning once per key.
Neither keys nor error bodies are logged. Streaming can recover before any
output but never replays emitted content. Decision 0066 records the policy.

Switch: `EXCALIBUR_EXHAUSTED_KEYS` defaults ON; `0` restores W13 alternation.
The parent `EXCALIBUR_BUDGET_RETRIES=0` bypasses the observer entirely and
restores legacy bounded retries. All-key exhaustion returns an infrastructure
error: the engine waits on capability availability, `final.json` records
`state=waiting`, `reason=provider_quota_exhausted`, and model-call records and
evaluation indexing retain the same error category rather than `failed/work`.

Backend tests: `TestExhaustedCredentialRecovery` covers fallback exhaustion,
primary exhaustion, all-key exhaustion, code-only errors, process persistence
across provider construction, and the switch-off comparison using httptest.
`TestQuotaStreamQuarantine` covers payment errors and subsequent streaming
calls skipping the key. `TestPermanentQuotaMarkers` separates permanent credit
failures from transient quota windows. `TestExecQuotaExhaustionIsInfrastructure`
checks the complete exec path and final/model-call records;
`TestQuotaInfrastructureCategory` checks evaluation classification.

Research sources consulted: [LangChain harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering),
[Meta-Harness](https://arxiv.org/abs/2603.28052),
[Agentic Harness Engineering](https://arxiv.org/abs/2604.25850),
[Terminus 2](https://harborframework.com/docs/agents/terminus-2), and
[mini-swe-agent retry utility](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/models/utils/retry.py).
These provide context on runtime state, observability and retry exclusions;
the director's specified credential policy determines this change. No prompts,
skills or stopping criteria were changed. No benchmark task contents, tests or
solutions were read, and no benchmark runs were launched.

Live verification used two original tasks with `gpt-6-luna`, low effort, and the
operator credentials file: create an exact text file and repair a Python
multiplication function with assertions for zero, positive and negative inputs.
A local proxy injected an initial primary rate limit and a permanent quota
error for a synthetic fallback credential, then forwarded primary requests to
the real model. Both runs exited 0 and independent artifact checks passed.
The first made 12 primary requests, the second 10; each contacted the exhausted
fallback exactly once, logged exactly one quarantine warning, and recorded one
retry wait. Records: `/tmp/w16-live-ny55qzdb/logs1/excalibur/` and
`/tmp/w16-live-ny55qzdb/logs2/excalibur/`. No real fallback key was sent upstream
by these smoke checks. `TestCredentialQuarantineLogsOnce` additionally verifies
concurrent quarantine produces a single warning without exposing credentials.

Full verification also exercised W13's deadline boundary under load. The engine
now pauses on an expired wall-clock deadline both between steps and during a
model wait. `TestExpiredDeadlinePausesRunningEngine` covers the former directly;
the exec overload-expiration test covers the latter with an isolated temporary
project and a five-second budget so setup cannot consume its old one-second
window before the first model request.

Verification on the final code: empty `gofmt -l .`; `go vet -p 2 ./...`;
`staticcheck ./...`; Darwin and Windows `go build -p 2 ./...`; and the
no-bare-goroutine check all passed. The full `go test -race -p 2 ./...` run
passed every package except `internal/store`: `TestPowerCutBlobInvariant`
reported zero storm rows before its fixed 1.5-second child kill. Its isolated
rerun without an extra CPU cap failed with the same result. Storage and its
test dependencies are unchanged by W16. Consequently the repository-wide
race quality gate is not yet green. Final check logs are
`/tmp/w16-final-*.log`; the isolated failure is
`/tmp/w16-store-isolated.log`. All W16 tests, the complete CLI/engine/models
test packages, and both live task checks passed. No storage change was made
outside this workstream's scope.

W16 iteration 2 verification: the unchanged implementation passed the complete
`nice -n 19 go test -race -p 2 ./...` suite, including the storage crash test.
The earlier storage timing failure did not recur. Formatting,
`go vet -p 2 ./...`, `staticcheck ./...`, Darwin and Windows cross builds,
and the no-bare-goroutine check also passed again. Full race output is recorded
in `/tmp/w16-iteration2-race.log`. No storage changes were needed.

### W16 integration

Merged `w16-exhausted-key` into `agent-quality`. The existing credential retry
counter, HTTP 5xx status enrichment, registered-provider handling and bounded
deadline persistence remain intact. Quota quarantine and infrastructure wait
reporting compose with those mechanisms. Shared core interfaces, connector
code, model wire contracts, servers and Harbor invocation are unchanged.
Decision 0066 records quarantine; the duplicate W13 decision is consolidated
into existing decision 0065.

The source branch's status was available at
`/home/cristhian/excalibur-imp/status/w16-exhausted-key.status`, rather than the
specified orchestrator status directory. Its final iteration reports all gates
passing after an earlier storage startup timing failure. Integration likewise
passed the full low-priority `go test -race -p 2 ./...`, including storage,
plus empty `gofmt -l .`, vet, staticcheck, Darwin/Windows cross builds,
`git diff --check` and the no-bare-goroutine check. The final deadline helper
also passed a separate engine race run. Logs are `/tmp/w16-race.log` and
`/tmp/w16-engine-final.log`. No W16 feature was dropped; process-lifetime
quarantine and restart after restoring credits are deliberate policy limits.

Two integrated-binary live checks passed with `gpt-6-luna`, low effort:
an exact text file and a repaired Python multiplication function. Independent
artifact assertions passed and both exec processes exited zero. A local proxy
injected one transient primary rate limit and permanent exhaustion for a
synthetic fallback; each run contacted that fallback exactly once and logged
one quarantine warning. Model-call records show one and two retry waits,
respectively (the latter includes an upstream wait). Records remain outside
the repository at `/tmp/w16-live-b9ebwoxj/logs1/excalibur/` and
`logs2/excalibur/`; aggregate results are
`/tmp/w16-integrated-live.jsonl`. No Terminal-Bench run was launched.
## W17: nested arguments and accidental self-kill protection

The executor decodes JSON strings only where the tool schema expects an object
or array, traverses declared object properties and array items, and clamps
numbers to declared inclusive bounds. Notes name each repaired field and its
conversion or old/new numeric value. Existing aliases receive a second schema
normalization pass. Malformed nested JSON, wrong types, missing action fields,
and non-schema constraints still produce ordinary validation errors.
`EXCALIBUR_TOOL_TOLERANT_ARGS=0` disables these repairs (default on).

`EXCALIBUR_SELF_KILL_GUARD=0` disables both terminal signal checks and the Linux
exec argv transfer (default on). Normal, PTY and detached terminal launches
check literal `kill`, `pkill`, and `killall` calls, including absolute paths,
common command wrappers and nested literal shell strings. Signals matching the
harness, its parent or members of their process groups are refused. Unsupported
selectors and dynamic signal operands are refused with guidance to use explicit
PIDs outside those groups. Ordinary unrelated cleanup remains available.
Linux exec replaces its own process image once, preserving PID and exit status,
with a placeholder positional request; an environment transfer supplies the
original request and is cleared before tool execution.

This prevents accidental signals through the inspected utilities; it is not a
sandbox for arbitrary programs or dynamically generated scripts. Process
selection is a preflight snapshot and cannot eliminate every process race.
See decision 0067 for the conservative selector policy.

Backend coverage: `TestNestedSchemaNormalization` covers object/array decoding,
recursive arrays/properties, high and low numeric bounds, wrong container types,
malformed strings and switch-off behavior. `TestPipelineNormalizationNotes`
checks actual invocation and journaled result notes. `TestSignalSelectors`
reproduces task-text pattern matching, parent matches, numeric PID/group targets,
broadcasts and unrelated cleanup. `TestGuardSignals` checks live process
inspection and wrappers. `TestSessionRefusesSelfKill` verifies normal and detached
launches refuse an actual self PID without executing it.

Research consulted before implementation:

- [LangChain harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering): concrete tool feedback and traceable runtime behavior.
- [Meta-Harness](https://arxiv.org/abs/2603.28052): deterministic context and harness behavior affect model effectiveness.
- [Agentic Harness Engineering](https://arxiv.org/abs/2604.25850): component and decision observability support auditable changes.
- [Terminus 2 implementation](https://github.com/harbor-framework/harbor/blob/main/src/harbor/agents/terminus_2/terminus_2.py): structured command parsing and terminal observation boundaries.
- [mini-SWE-agent implementation](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/agents/default.py): bounded format-error handling and explicit termination signals.

These sources inform implementation boundaries only; this change adds no new
planning, stopping, or verification policy. Evaluation records remain standard
audit evidence under Section 23.2 and Appendix F.

Live smoke evidence (2026-11-01, operator credentials, `gpt-6-luna`, effort
`low`; temporary projects only, no benchmark runs):

- `/tmp/w17-live/d/records/excalibur/tool_calls.jsonl`, sequence 1: the
  requested `kill -TERM $PPID` was refused before launch; subsequent shell
  script execution printed `hello-w17`. The finish was accepted, but the
  existing final-state issue reported `running` / exit 1. This is mechanism
  evidence, not a claim of successful final-state reporting.
- `/tmp/w17-live/e/records/excalibur/tool_calls.jsonl`, sequence 1:
  `call_tool` invoked `exec` with `yield_ms:60000`; the result contains
  `argument normalization: $.yield_ms: clamped 60000 to 30000` and
  `nested-ok`. The run later reached its 90-second timeout after unrelated
  completion-gate demands. The repair was exercised six times in this record.
- `/tmp/w17-live/argv-observation.json`: while the second run was active,
  `/proc/<pid>/cmdline` contained `[request transferred]` and no instruction
  text. `TestReexecRemovesInstruction` additionally verifies PID preservation.

A direct-exec smoke (`/tmp/w17-live/c`) chose `yield_ms:6000` despite the
request, so it did not exercise clamping. An earlier startup attempt used a
previous binary while the build was still in progress and is excluded from
current-version evidence. A default-profile follow-up (`/tmp/w17-live/b`)
completed with exit 0 after a guarded signal refusal and a Python assertion.
No task answers or benchmark contents were inspected. Nested string repairs
are covered by deterministic pipeline tests, including the recorded
`call_tool.arguments`, `task_db.ops`, and `subagent.card` shapes.

`TestKillallLongAndPathNames` covers Linux process-name truncation and
conservative refusal of executable-path selectors. The normal, PTY and detached
session paths all run the same preflight before launching a process.

Final verification (2026-11-01): `gofmt -l .` produced no output;
`go vet -p 2 ./...`, `staticcheck ./...`, `go test -p 1 ./... -race`,
`GOOS=darwin go build -p 2 ./...`, `GOOS=windows go build -p 2 ./...`,
and `scripts/check-no-bare-go.sh` all passed. Heavy checks ran with
`nice -n 19` and `GOMAXPROCS=2`. Logs are in `/tmp/w17-verification/`.
The initial parallel race run hit the existing five-second deadline in
`TestWiringReviewLoopRuns`; its isolated rerun and the complete serial race
rerun both passed. No daemon code or test deadlines were changed.
## W17: Verifier verdict recovery and behavioural evidence

`EXCALIBUR_VERIFIER_VERDICT` defaults to on; `off`, `false`, or `0` restores
legacy verdict handling and metadata evidence recognition. With the switch
on, refinement, independent verification, and requirement review use an 8192
token output budget and compact structured replies. Truncation or a missing
verdict permits one retry with reduced context. Missing independent verdicts
remain unverified through the existing finish-attempt limit. Refinement and
requirement review keep their conservative fallbacks. Each engine call carries
its site (`engine.step`, `verifier.refine`, `verifier.strong`, or
`verifier.requirement_review`); recovery adds `.retry`.

Existence and size probes alone cannot satisfy behavioural evidence, including
program output, service responses, conversions and numeric thresholds. The
deterministic filter avoids interpreting filenames such as `check.py` as
executed validation. The verifier also receives this rule for semantic review.
The rule does not reject an instruction that only asks for a file to exist.
Mixed commands remain subject to the independent verdict.

Backend coverage: `internal/engine/verifier_verdict_test.go` replays sanitized
zero-token truncation metadata from two verifier records in the read-only
v9-default run dated 2026-11-01 (the older tb21-full run contains only executor
calls). Tests cover successful retry, exhausted retry, absent and partial
verdicts, larger budgets, reduced prompts, call sites, and cancellation.
`internal/tasks/verifier_verdict_test.go` covers metadata-only probes, mixed
behavioural commands, draft-free requirements, contradictory verdicts, and
ablation. Legacy permissive-path tests explicitly disable the switch.
Decision 0068 explains the raw provider telemetry limitation: error-path usage
is retained when available, but discarded usage and raw finish reasons remain
unavailable through the protected connector contracts.

Research consulted before implementation:

- [LangChain harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering): self-verification and trace inspection support explicit stopping checks.
- [Meta-Harness](https://arxiv.org/abs/2603.28052): environment context and harness behaviour affect outcomes, particularly for weaker models; no automatic harness evolution is implemented here.
- [Agentic Harness Engineering](https://arxiv.org/abs/2604.25850): observability supports attributable, auditable changes.
- [Terminus 2 source](https://github.com/harbor-framework/harbor/blob/main/src/harbor/agents/terminus_2/terminus_2.py): explicit completion handling and output-length recovery are separate concerns.
- [mini-swe-agent configuration](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/config/default.yaml): executable reproduction, edge-case checks and explicit submission; truncation receives specific format-recovery guidance.

These sources inform the implementation of the director's specified design;
no benchmark task contents, tests or solutions were consulted or incorporated.

Live checks (2026-11-01): two temporary repositories used `excalibur exec
--profile eval` with operator credentials, `gpt-6-luna`, effort `low`.
Both completed with exit 0. `hello.py` printed the requested literal and
`sum.py` passed exact stdout assertions for empty input, negative values and
multiple integers. Separate post-run executions confirmed all four outputs.
Records remain outside the repository under `/tmp/w17-live/`.

| Task | Model calls | Verifier calls | Observed mechanism |
|---|---:|---:|---|
| Literal stdout | 9 | 4 | `verifier.refine` and `verifier.strong` site tags; no provider errors |
| Integer CLI | 14 | 5 | Site tags, `verifier_verdict: true`, and `max_output_tokens: 8192` on every verifier call; no provider errors |

The first binary preceded the addition of explicit switch/budget audit fields;
both runs exercised the verdict gate. Live calls produced verdicts without
needing recovery; deterministic truncation fixtures cover recovery itself.
The recording-layer test in `internal/cli/verifier_records_test.go` verifies
that error records preserve returned usage, distinguish unavailable usage,
and retain call sites and inferred truncation finish reasons.

Final verification: formatting, `go vet -p 2 ./...`, standard
`staticcheck ./...`, `go test -race -p 1 ./...`, the bare-goroutine check,
and both Darwin and Windows builds passed. A prior parallel race run hit
the unchanged daemon review-loop test's five-second migration deadline under
shared-machine load; the complete serial rerun passed without changes to
that package. Regression coverage also confirms that daemon-owned verifier
verdicts remain owned by the daemon when no embedded model is configured.

### W17 integration

Merged `w17-args-and-selfkill` and `w17-verifier-verdicts`, in that order,
into `agent-quality`. Documentation conflicts retain every prior wave and
both W17 sections. The model-call record conflict retains retry counts and
wait durations alongside the new verifier call-site, budget, switch and usage
fields. Decisions are uniquely numbered 0067 (arguments/self-signals) and
0068 (verdicts); existing decisions 0065 and 0066 remain unchanged. Shared
interfaces and protected connectors are unchanged. The shell parser dependency
is pinned and its BSD-3-Clause license is recorded.

Two original temporary terminal tasks passed against the integrated binary
with `gpt-6-luna`, low effort, and operator credentials. Both exited zero and
independent post-run assertions passed: exact shell-script stdout and integer
CLI sums for empty, positive and negative inputs. Each recorded five verifier
calls with `verifier_verdict: true` and `max_output_tokens: 8192`. The first
recorded refusal of `kill -TERM $PPID`; the second recorded a wrapped exec
`yield_ms` repair from 60000 to 30000. That wrapped call omitted `workdir` and
initially failed to find the script; subsequent execution recovered and the
artifact passed. This is a working-directory follow-up, not evidence that
normalization guarantees successful commands. Records are outside the
repository at `/tmp/integrate-w17/live/{hello,sum}-logs/excalibur/`; aggregate
results are `/tmp/integrate-w17/live-results.jsonl`. No benchmark was launched.

Review limitations remain explicit: self-signal inspection is a snapshot-based
accident guard, not a sandbox; discarded provider usage and exact raw finish
reasons cannot be recovered without changing protected connectors. The
embedded verifier reviews recorded evidence and requests additional executable
checks from the executor; it does not itself reproduce checks as specified
for the Section 8.7 verifier. Daemon ownership is preserved; no broader
verifier redesign was introduced here.

Integrated verification passed: empty `gofmt -l .`, `go vet -p 2 ./...`,
`staticcheck ./...`, complete `go test -race -p 1 ./...`, and Darwin/Windows
`go build -p 2 ./...`. Heavy checks used `nice -n 19` and `GOMAXPROCS=2`.
Logs are `/tmp/integrate-w17/{fmt,vet,static,race,darwin,windows}.log`.
## W14: Task deadline awareness

Switch: `EXCALIBUR_DEADLINE` defaults on; `off`, `0`, `false`, or `no`
restores the prior deadline behavior. The effective switch, duration and source
are recorded in `budgets.json` and `profile.json`, and the budgets event.
Source priority is explicit `--timeout`, `HARBOR_AGENT_TIMEOUT_SEC`,
`EXCALIBUR_TIMEOUT_SEC`, the instruction's “You have N
seconds/minutes/hours”, then the existing configured budget. The full external
timeout includes the closing reserve and starts at entry to `RunExec`, including setup. See decision 0060 for parsing and
interruption details.

At the next model boundary after 50%, 75%, and 90%, one pacing message reports
remaining time and the required action. At 75% the agent must stop exploring
and converge; at 90% it must preserve its best deliverable and call `finish`.
Closing mode refuses new background commands, interactive command input and
explicit waits longer than five seconds (or the remaining duration). Other new
exec commands have an actual process timeout with the same bound. Existing
sessions are not killed merely by entering closing mode. SIGTERM uses the
existing finalization path and records `stopped_by_timeout` rather than leaving
an unfinished record. Ordinary deadline expiry records `paused` with reason
`timeout`, including when expiry occurs during a model call.

Backend coverage: `TestDeadlineSources` checks source precedence, units,
multiple limits, invalid/overflowing values and ablation;
`TestDeadlinePacingAndClosing` checks thresholds, single delivery, remaining
time, refusals and short checks; `TestInstructionDeadlineBudget` checks default
and eval profiles; `TestTerminationFinalRecord` checks the existing final
record path; `TestDeadlineExpiryFinalRecord` checks context expiry and
`TestSIGTERMContext` checks the actual signal handler;
`TestDeadlineStopsSessionAndPreservesShellQuoting` checks actual
process termination and shell quoting.
`TestDeadlineCancellationReleasesInitialWait` verifies that a 30-second initial
yield returns promptly on cancellation, allowing final records to be written. Existing budget-record tests now expect
the full external duration with deadline awareness enabled.

Research reviewed for this bounded change:

- [LangChain, Improving Deep Agents with harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering): explicitly describes time-budget warnings to shift agents toward verification. Its reported 52.8 to 66.5 change combines several interventions and does not establish a deadline-only effect.
- [Meta-Harness](https://arxiv.org/abs/2603.28052): treats stored and presented context as harness responsibilities. The task's deadline is deterministic environment context; no optimization machinery from the paper is added here.
- [Agentic Harness Engineering](https://arxiv.org/abs/2604.25850): motivates observable, attributable changes. The switch and recorded source support ordinary evaluation and auditing.
- [Terminus 2 source](https://github.com/laude-institute/harbor/blob/main/src/harbor/agents/terminus_2/terminus_2.py): reference terminal harness with explicit command durations and trajectory records.
- [mini-swe-agent control loop](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/agents/default.py): explicit wall-time limits and serialized state are useful comparisons for enforcing time outside the model.
- [SWE-agent default prompt](https://github.com/SWE-agent/SWE-agent/blob/main/config/default.yaml): executable reproduction and verification provide the purpose of the closing reserve.
- [OpenHands](https://github.com/OpenHands/OpenHands) and [Aider prompts](https://github.com/Aider-AI/aider/blob/main/aider/prompts.py): reviewed as examples of explicit execution context and command-output feedback; no behavior is imported from them.

- [Codex CLI](https://github.com/openai/codex) and [OpenCode](https://github.com/anomalyco/opencode): reviewed as terminal-agent references; no prompts or stopping rules are imported.

The read-only failure review was used only for timeout and missing-final-record
behavior. No benchmark task contents, test files or solutions were inspected.

Live smoke evidence (2026-11-01): two independent temporary-directory tasks
created a sum file and an alphabetically sorted word list; both deliverables
were checked from the host and were correct. Records at
`/tmp/w14-live-g2y6lui9` show `deadline: true`, `wall_source: instruction`,
120-second budgets, model `gpt-6-luna` and effort `low`. These initial checks
exposed the pre-existing finish loop and expiry-label edge case; they ended
with running/failed labels rather than successful completion. Deadline expiry
normalization was then added and covered by `TestDeadlineExpiryFinalRecord`.
A third real exec check at `/tmp/w14-deadline-final-d2kki5q3` created and verified
a marker file, then received SIGTERM. Its existing final record reports
`stopped_by_timeout`, reason `timeout`, and exit 143, with the instruction-derived
60-second deadline enabled. This validates signal finalization, not task
completion. No Terminal-Bench run was launched.

After adding cancelable initial yields, the signal check was repeated with a
single terminal command that writes a marker and then sleeps for 30 seconds,
requesting a 30-second yield. SIGTERM arrived after the marker appeared. The
process finalized in 0.215 seconds with exit 143 and `stopped_by_timeout`;
the marker remained correct. Records: `/tmp/w14-deadline-final-dlz0_h0b`.

Verification on the final code: formatting, `go vet`, default `staticcheck`,
Darwin and Windows builds, and the no-bare-goroutine check passed. The full
repository race suite passed on the continuation check with
`nice -n 19 env GOMAXPROCS=2 go test -race -p 2 ./...` (exit 0; log:
`/tmp/w14-deadline-iteration2-race.log`). This includes the unchanged daemon
suite, which had exceeded its startup contexts during earlier contended runs.
No daemon tests or timeouts were modified. Refreshed formatting, static-analysis
and cross-build logs are at `/tmp/w14-iteration2-*.log`.
## Scope guard and input protection

`EXCALIBUR_SCOPE_GUARD=on|off` defaults to on. The engine snapshots the
working tree before its first model/tool round and checks protected paths at
both explicit and text finish. File fingerprints include contents and mode;
Git diff baselines and finish hashes supplement the filesystem snapshot.
Existing operator edits form the baseline. A snapshot failure stops the run
before tools execute.

The deterministic grammar recognizes prohibitions (`do not modify/delete/change`,
`never edit`, `leave ... untouched`), named directories, globs and `all files`;
`only modify/edit/change/write/create` limits changed paths. Required locations
follow write/create/save/output/produce/store/place verbs. Explicit `exactly`,
`at least`, `at most`, `more than`, `less than` and `no more than` counts apply
to bytes, lines or JSON-array rows/records/entries when the clause names one
file. Other numeric and universal constraints remain literal in verification
context; arbitrary semantic thresholds are not inferred from prose.

Named database, WAL/journal, archive and binary inputs are copied eagerly into
unique private directories under `/tmp/excalibur-originals/`, retaining companion
file layout. The first model request includes backup locations and copy-first
probe guidance. Copies survive the run. This intentionally precedes even an
apparently read-only database open. Failed checks retain their assertion and
expected-value lines; edits to these lines and changed literals in repeated
inline checks refuse finish with the file or original/replacement line.

Backend fixtures cover protected-file modification/deletion/restoration,
WAL loss with intact copies, failed-check expectation changes, valid source
repairs, inline check changes, allowed edit locations, missing outputs, line
counts, all-file protection and directory additions. These checks supplement
the existing executable verification gate (Sections 7.5, 13.7 and 18.1).

Research consulted: [LangChain](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering)
reports gains from verification, tracing and context engineering;
[Meta-Harness](https://arxiv.org/abs/2603.28052) discusses proactive environment
context and gains for smaller models;
[Agentic Harness Engineering](https://arxiv.org/abs/2604.25850) motivates explicit,
auditable changes and layered evidence. These support external state and clear
feedback, without adding an autonomous harness modification loop.
[Terminus 2](https://github.com/harbor-framework/harbor/tree/main/src/harbor/agents/terminus_2)
provides explicit completion handling;
[mini-swe-agent](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/config/default.yaml)
uses reproduction and verification before submission;
[SWE-agent](https://github.com/SWE-agent/SWE-agent/blob/main/config/default.yaml)
asks agents to restore modified tests at submission;
[Aider](https://github.com/Aider-AI/aider/blob/main/aider/prompts.py)
keeps command output as concrete evidence. No evaluation task content or hidden
tests were used to author these mechanisms or fixtures.

Guard baselines, copies and failed-check signatures are persisted atomically in
private state files under `/tmp/excalibur-originals/`. Reusing a run ID restores
its initial baseline; a changed instruction cannot silently replace it.
`scope_guard.json` and the `scope_guard` event record activation, snapshot path,
protected hashes, copies, constraints, Git diff hashes and the final violation.
The explicit finish protocol keeps refusing violations within the run budget;
the legacy text finish path uses the existing bounded gate and reports pending
criteria instead of accepting completion.

Additional references inspected:
[OpenHands agent loop](https://github.com/OpenHands/software-agent-sdk/blob/main/openhands-sdk/openhands/sdk/agent/agent.py)
keeps completion as an explicit tool action;
[Codex base instructions](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/prompts/base_instructions/default.md)
apply repository instructions by directory scope;
[OpenCode session contract](https://github.com/anomalyco/opencode/blob/dev/specs/v2/session.md)
separates durable admission and execution. These reinforce keeping the initial
constraint state outside model context. The Git check also compares per-path
raw diffs, catching index-only changes that leave working-tree bytes intact.
Deletion-only prohibitions allow content edits, and extensionless filenames
are recognized following explicit scope verbs.

Live validation on 2026-11-01 used three original temporary fixtures and the
operator credentials file with `gpt-6-luna` (default low effort), without any
Terminal-Bench invocation:

| Fixture | Outcome | Evidence |
| --- | --- | --- |
| Sum integers from a binary-named input; preserve a separate file | Correct sum and protected file; run paused at its 16-step cap after existing verification refusals | `/home/cristhian/.local/share/excalibur/run-run_01M6BFAS5BX5DZ2NNS16WAGZXG/` |
| Repair a two-argument addition function; preserve its checks | Correct function and passing unchanged checks; run paused at its 16-step cap | `/home/cristhian/.local/share/excalibur/run-run_01M6BFBF25YSBSC9P32KPNSN53/` |
| Copy preserved binary input into an output and compare | Completed; protected input unchanged; `cmp` passed | `/tmp/w14-live/home3/state/run-run_01M6BFRVEG4TT6KDEKFHMFT8QQ/` |

The third run's `scope_guard.json` records `enabled: true`, one initial file,
its protected fingerprint, a copy under `/tmp/excalibur-originals/`, a durable
snapshot path and an empty final violation. Its recorded cost was $0.073203.
The earlier two checks used the first implementation before the dedicated
scope record was added; their preserved input and tool evidence remain on disk.
These checks establish mechanism activation and behavior, not a benchmark score.

The failed-check detector samples test/check files and directly named scripts;
it does not freeze ordinary implementation variables named `expected` after a
project test failure. External named inputs are compared at their own locations
when enforcing an `only` rule. Backend regressions cover both cases.

Input detection includes extensionless existing filenames. Executable bits,
binary headers and embedded NUL bytes identify binary inputs even without a
recognized extension; special files are excluded from probing and copying.
Empty protected-directory deletion is checked separately from file hashes.

CLI runs without `--project` use their current directory as the snapshot root;
Git diff paths are relative to that root, including runs started in a repository
subdirectory. A simulated CLI test verifies default activation and the report.

Final-build live verification repeated the sum and copy fixture kinds with the
same reference model and default low effort. Both completed with their expected
artifacts and unchanged protected files. The records are
`/tmp/w14-current-live/sum-home/state/run-run_01M6BGQC9A9M7ZQVS59BDQ0MFW/`
and `/tmp/w14-current-live/copy-home/state/run-run_01M6BGS7Z33KZ39GFBAZ3C1ZX1/`.
Each `scope_guard.json` confirms activation, preserved inputs, the initial
snapshot, and no final violation. Recorded costs were $0.134097 and $0.113088.

When the current directory is the filesystem root, there is no project working
tree to enumerate. The guard snapshots explicitly named files and directories
instead of traversing system and virtual filesystems. Tree-wide `only`/`all
files` restrictions at that root require an explicit project directory. Tests
cover this boundary and the existing container certificate handshake test passes.

Explicitly protected directories include their metadata files, even when the
ordinary working-tree snapshot excludes `.git`; a regression test confirms an
untouched metadata directory passes and a later edit is refused.

Final verification passed: `gofmt -l .` (empty), `go vet ./...`,
`staticcheck ./...`, `go test ./... -race`, Darwin and Windows builds, and the
bare-goroutine check. Commands ran at low priority with `GOMAXPROCS=1` and
`GOFLAGS=-p=1` on the shared machine. Logs are under
`/tmp/w14-scopeguard-finalcheck-*.log`. Earlier concurrent attempts exposed an
existing storage busy-counter timing race and daemon migration deadlines; the
complete final serial run passed without changes to those packages.

## W14 integration with the later quality waves

Both W14 workstreams were merged into the existing W17 tree. Deadline-aware
terminal starts retain detached background service support and the self-signal
guard. Scope findings join actionable finish refusal reasons, preserving the
other gate checks and per-item audit records. A missing project flag resolves
the scope snapshot against the current directory; the deadline still starts at
CLI entry rather than after setup.

The later final-record contract remains authoritative: standalone timeout and
signal records use `stopped_by_timeout` and exit 4; delegated tasks retain shared
paused/canceled states. Decision 0060-task-deadline records this integration
resolution. Shared types, API, provider/tool interfaces and connectors are
unchanged. Scope constraints use the bounded grammar and heuristic failed-check
tracking documented in 0060-scope-guard; they do not establish arbitrary semantic
requirements. The existing verifier and executable criteria remain necessary.

Integrated validation passed: empty `gofmt -l .`, `go vet -p 2 ./...`,
`staticcheck ./...`, complete `go test -race -p 2 ./...`, Darwin and Windows
`go build -p 2 ./...`, and the no-bare-goroutine check. Commands used
`nice -n 19` and `GOMAXPROCS=2`; logs are `/tmp/integrate-w14/*.log`.

Two real terminal checks used `gpt-6-luna`, low effort, in temporary Git
repositories: an integer sum CLI and sorting a three-line input file. Host-side
assertions passed and the protected input remained unchanged in both cases.
Both `budgets.json` records show `deadline: true`, `wall_source: instruction`
and 120 seconds; both `scope_guard.json` records show `enabled: true` with no
violation. Records are `/tmp/integrate-w14/live/{sum,sort}-logs/`. Both reached
their deadline and finalized as `stopped_by_timeout`, exit code 4 in records;
they are mechanism checks with correct artifacts, not successful completion
runs. The existing `exec --json` command returns process exit zero after printing
the outcome, even for a nonzero outcome code; this discrepancy is reported for
closure. No benchmark runs were launched.

## W15: call/result pairing invariant (Section 19.2 strict wires)

Owner: call-pairing workstream. Packages: `internal/ctxcompiler`
(`pairing.go`, `compaction.go`), `internal/engine` (`builder.go`,
`pairing.go`, `context_control.go`), `internal/eval` (`trialrec.go`),
`internal/cli` (`exec_engine.go` wiring). Connectors
(`internal/providers/**`, `internal/models`, `internal/gateway`),
contracts, MCP/ACP and Harbor invocation unchanged.

Strict wires reject split pairs: Responses answers HTTP 400 `No
function call found for function call output with call_id ...` when
a result has no call in the same request, and Chat Completions
rejects the mirror (assistant `tool_calls` with no following tool
message). Long runs cross masking and compaction thresholds, and a
budget cut inside a multi-call batch (one assistant event, several
results) or any upstream split fails the run. The invariant holds
at three levels: compaction tails keep pairs atomically, every built
request is validated and repaired to plain-text summaries before the
wire, and a wire rejection repairs the run history the same way
with exactly one retried model call.

### Mechanisms (all default ON)

1. Pair-atomic compaction tails: `SelectTail` extends a cut starting
   at a tool result back to its call so the whole batch (call plus
   every result) rides along; a result whose call is gone entirely
   falls into the discarded span the summary still covers. Masking
   replaces outputs in place and keeps every call identity, so it is
   atomic by construction.
2. Pre-request invariant check in `DefaultBuilder.BuildRequest`:
   `ValidateMessages` over the projected request; orphans become
   bounded plain-text summaries (`RepairMessages`), order and Seq
   preserved. The run history itself is untouched here.
3. Orphan-400 repair and single retry in the engine step loop:
   `IsOrphanPairError` matches both wire directions only; on match
   the history is repaired (`RepairActivity`), the request rebuilt
   and the call retried once. A second rejection fails the run.
   Counters `Run.PairRepairs`/`PairRetries` track both paths; both
   hooks land in the new `pairing.jsonl` trial record via
   `TrialRecorder.RecordPairRepair`.

### Switches (call pairing)

| Switch | Type | Default | Off restores |
|---|---|---|---|
| `EXCALIBUR_CONTEXT_PAIR_ATOMIC` | env | on | legacy single-pair tail re-attachment |
| `EXCALIBUR_CONTEXT_PAIR_CHECK` | env / `DefaultBuilder.PairCheck` | on | no pre-request validation |
| `EXCALIBUR_CONTEXT_PAIR_RETRY` | env / `engine.Deps.PairRetry` | on | fail fast on orphan 400 |

Measure each switch off against on with the same model, effort and
task set (Appendix F); keep a mechanism only if it does not regress
the sample. Research: LangChain "Improving Deep Agents with harness
engineering" (self-verification, tracing, loop detection, context
engineering), Meta-Harness (arXiv 2603.28052: proactive snapshot
removes wasted actions, larger gains on weaker models), Agentic
Harness Engineering (arXiv 2604.25850), Terminus 2, OpenHands /
SWE-agent / mini-swe-agent / Codex CLI / OpenCode / Aider (prompts,
tool design, stopping criteria). No benchmark task contents, tests
or solutions were used.

### Contract and backend tests (call pairing)

- `internal/ctxcompiler/pairing_test.go`: balanced validation,
  orphan-result and orphan-call repair (both wires projected),
  activity repair with Seq order, 400 matcher for both directions
  plus non-matches, default-ON switches, multi-call batch atomic
  tail, and a long synthetic run (100 pairs plus multi-call batches
  every tenth turn) crossing masking and compaction thresholds with
  pair-complete masking, compacted tail and request.
- `internal/engine/pairing_test.go`: builder repairs an orphan
  request and records it without rewriting history; check-off
  switch; step retries an orphan 400 exactly once and completes;
  second rejection fails with no loop; transient errors never retry;
  disabled retry fails fast; nothing-to-repair fails without an
  extra call.
- Existing suites green; see decision
  [0060](../decisions/0060-call-pairing-invariant.md) for the
  architecture gap and the alternatives considered.


### W15 continuation: wire regressions and recovery edge cases

The compaction boundary scans all retained results, including results
separated from their calls by pinned notes. The long synthetic regression
also sends the compiled request through both actual OpenAI adapters to an
`httptest` server that checks the serialized call IDs in both directions.
It makes no external requests.

A pairing HTTP 400 triggers one recovery attempt. When run-history repair
finds no orphan (for example, snapshot-owned activity or an ordering
rejection), the retry converts tool exchanges in the rebuilt request to
bounded text summaries. This fallback preserves the switch and single-retry
limit. Pairing configuration is recorded in `pairing.jsonl` even when no
repair is needed. Unrelated HTTP 400 schema errors and HTTP 500 errors do
not trigger pairing recovery.

Research sources reviewed for context, without changing the director's
design:

- [LangChain harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering):
  trace observability, verification and context control are harness concerns.
- [Meta-Harness](https://arxiv.org/abs/2603.28052):
  context storage and presentation materially affect agent behavior.
- [Agentic Harness Engineering](https://arxiv.org/abs/2604.25850):
  component observability and explicit hypotheses support attributable changes.
- [Terminus 2](https://harborframework.com/docs/agents/terminus-2):
  reference terminal harness and structured agent/environment interaction.
- [SWE-agent history processors](https://github.com/SWE-agent/SWE-agent/blob/main/sweagent/agent/history_processors.py):
  observation elision keeps history entries while replacing their content.
- [mini-swe-agent loop](https://github.com/SWE-agent/mini-swe-agent/blob/main/src/minisweagent/agents/default.py)
  and [Aider history](https://github.com/Aider-AI/aider/blob/main/aider/history.py):
  explicit history management and stopping behavior.
- Repository overviews for [OpenHands](https://github.com/OpenHands/OpenHands),
  [OpenCode](https://github.com/anomalyco/opencode) and
  [Codex CLI](https://github.com/openai/codex) were consulted for harness scope.
  No prompts, examples or benchmark task material were imported.

Live continuation checks used three original terminal tasks in temporary
directories: write integers and assert their sum; implement a stdin line
deduplicator and assert stable output order; write sorted lines and assert
the exact file bytes. All three completed and their artifacts passed
independent checks. The third used the eval profile directly through
`excalibur exec` (no benchmark invocation): records at
`/tmp/w15-live-three/records/excalibur` show `gpt-6-luna`, effort `low`,
state `completed`, and `pairing configuration: atomic=true check=true
retry=true`. No repair was needed on these short histories; masking,
compaction and 400 recovery are exercised by synthetic backend tests.
Interruption during the recovery request preserves full-stop and
tool-only interrupt behavior.

Continuation verification passed: empty `gofmt -l .`, `go vet ./...`,
`staticcheck ./...`, repository-wide `go test -race -p 2 ./...`,
Darwin and Windows `go build ./...`, and the no-bare-go check.
After the final interruption test addition, package-scoped race tests,
vet and staticcheck passed again for the affected packages.

### W15 integration with current run controls

The call-pairing workstream is integrated with the existing deadline, quota,
interrupt and verifier telemetry controls. Both documentation histories are
preserved. `TestDeadlineDuringPairRetry` verifies a budget pause if the deadline
expires during recovery and checks that both calls retain `engine.step` audit
metadata. `TestQuotaDuringPairRetry` verifies capability waiting when recovery
exhausts provider quota. The three pairing switches above remain default ON.
No shared contract, connector, MCP/ACP server or Harbor invocation changed.

Integrated verification passed: empty `gofmt -l .`, `go vet -p 2 ./...`,
`staticcheck ./...`, `go test -race -p 2 ./...`, Darwin and Windows
`go build -p 2 ./...`, and the no-bare-go check. Commands ran at
`nice -n 19` with `GOMAXPROCS=2`; logs are `/tmp/integrate-w15/*-final.log`.

Two original terminal tasks used `excalibur exec`, `gpt-6-luna`, low effort,
and temporary repositories. Independent checks passed for exact numeric-file
bytes and sorted input lines; protected inputs remained unchanged. Both
`pairing.jsonl` records show `atomic=true check=true retry=true`. Neither short
history needed repair; synthetic tests exercise masking, compaction and strict
wire recovery. Records are `/tmp/integrate-w15/live/{sum,sort}-logs/excalibur/`.
Both runs exhausted their 60-second deadlines and recorded
`stopped_by_timeout`, exit code 4, so these are activation and artifact checks,
not successful completion runs. The numeric-file run also recorded an existing
scope-guard rejection when an inline assertion command was reformatted. The
existing `exec --json` process returned zero despite the recorded timeout code.
Those broader closure behaviors remain for their owning workstreams. No
Terminal-Bench run was launched.

## Exec result contract

The exec tool reports the real exit of a finished command, names what a
failed step means, and keeps long output recoverable. Each part has a switch
that defaults on; setting it to `0` or `off` restores the previous behaviour of
that part only. The shared-contract change is recorded in
`docs/decisions/0069-exec-exit-status-in-result.md`.

### Mechanisms

- **Exit status.** A non-zero or unknown exit is a tool error (`is_error`,
  trial `exit_code`). A signal is named in the header, for example
  `exit: 139 (SIGSEGV)`; the code is 128+n, never -1. `tools.Result.ExitCode`
  carries the real code into the trial record.
- **Probe commands.** `test`, `[`, `grep`/`rg` without a match, `command -v`,
  `which`, `type`, `ls` of a missing path, `diff` and `pgrep` answer with a
  non-zero exit. Their exit 1 (exit 2 for `ls`) carries the note
  `note: exit 1 here usually means "not found / no match", not a failure`.
  `IsProbeCommand` classifies a command (every stage a probe or a plain output
  builtin, at least one probe). The finish refusal "your last command errored"
  skips probe failures.
- **Exit 0 with failure text.** A header warning
  `warning: exit 0 but the output contains "<phrase>"` covers `command not
  found`, a shell's `No such file or directory`, a Python traceback, a
  segmentation fault and an unterminated here-document.
- **Failure hints.** Hints for missing tools and missing paths apply only to a
  failed exit. The missing-tool hint names the package managers and the tools
  present, and asks the agent to provision the tool or use a present
  alternative. A missing path lists the nearest existing parent (at most 20
  entries) and asks to check the path.
- **Elided output.** Output above 8,000 bytes keeps the header, the head, and
  the last 2,000 bytes. The middle is stored under the executor's blob store,
  and the marker names the ref and the offset:
  `…[middle N bytes elided; full output: get_tool_output ref=<ref> offset=<n>]…`.
  The stored blob is the output after pip `[notice]` lines are removed.
  `get_tool_output` pages it.
- **Running sessions.** When `exec_io` is part of the surface, a live session
  prints `to poll: exec_io {"op":"read","session":"sess-N"}`. Otherwise it
  prints the elapsed time and points to `status`.
- **Read paths.** `exec_io` read, list and wait_for never provision. A
  provisioner run is an exec side effect and happens only from `exec`.
- **Owner and target.** An unknown `target` is ignored with the note
  `target ignored: commands run locally`. An `exec_io` list with an owner no
  session carries lists every session with a note.
- **Identical re-runs.** The same command in the same workdir, with
  byte-identical output and the same exit, and no file changed since the last
  run, carries `note: identical to call #K (no files changed since)`. The
  check walks the workdir for changes only when a candidate matches, is
  bounded to 5,000 entries, and treats any unreadable entry as changed.
- **Python test-only scripts.** A silent `python X.py` whose file defines
  `test_` functions and has no `__main__` block notes that zero tests ran.

### Switches

| Switch | Default | Governs |
| --- | --- | --- |
| `EXCALIBUR_EXEC_ERROR_STATUS` | on | Exit status and tool error, signal names, probe note, hints on a failed exit only |
| `EXCALIBUR_EXEC_WARNINGS` | on | Exit-0 warnings, identical re-run note, python test note, owner note, running-session wording, pre-install label, missing-path listing |
| `EXCALIBUR_EXEC_ELIDE_REF` | on | Stored elided output, the ref in the marker, pip notice removal |

Read paths never provision and the target note is always shown, so neither has
a switch.

### Contract and backend tests

- `internal/tools/exec_tools_test.go`: exit status and the record field, the
  switch, signal naming (a real signal-killed child and a shell `exit 139`),
  probe classification and note, the zero-exit warnings and the hint gating,
  elision (a stored blob pages through `get_tool_output`, offsets agree, no
  store falls back to the session id, the switch restores the old marker), pip
  notice stripping, UTF-8 safe cuts, identical re-runs, missing-path listing,
  the python test-only note, target and owner handling, running-session lines.
- `internal/tools/autoprovision_w9_test.go`: `exec_io` read, wait_for and list
  return no provision report.
- `internal/cli/exec_exit_record_test.go`: the trial record carries the real
  exit code and `is_error`.
- `internal/engine/exec_probe_finish_test.go`: finish is not refused after a
  probe failure.

### Verification

Package-scoped checks: `gofmt`, `go vet`, `go test -race` for `internal/tools`,
`internal/engine`, `internal/cli` and `internal/wireengine`, and the Darwin and
Windows builds.

## Automatic provisioning follow-up (exec contract)

Owner: harness reliability round, work package B (branch `reliability/package-installs`).
Code: `internal/tools/autoprovision.go`, with the exec and read-path call sites in
`internal/tools/exec_tools.go` and `internal/tools/control.go`.

Scope: when a command reports a missing command, module or library, the exec
call that ran it may install the dependency. Read paths never install.

Contract:

- Exec runs the provisioning step once for its own output. A verified install
  re-runs the original command once, and that result is the command result. The
  pre-install output is reduced to `first run: exit N: <first error line>`. When
  the command is still running at the yield, no re-run happens and the output is
  labelled as predating the install.
- The header reports provisioning time, for example `provisioning: 41s apt python3`.
- The whole provisioning step of one exec call is bounded by its `timeout_s`
  (120 s when none). A cut-off step returns the original output and the line
  `provisioning did not finish in Ns; install it yourself in a background session`.
- `status` and `exec_io` (list, read, wait_for) report `install needed: ...
  (run exec to provision)` and never run a package manager. `status` is read-only
  in every mode.
- Package names come from the curated tables (`commandToApt`, `pythonToPip`,
  `sharedLibToApt`) or from `apt-cache policy` (a real candidate is required).
  Harness tool names, names that are not Debian package names, and prose tokens
  never reach a package manager. A non-apt manager runs only curated names.
- `python3` maps to the full `python3` package. The pip bootstrap installs
  `python3-pip` and `python3-venv`. The minimal interpreter package is never used.
- Before an apt command runs, the provisioner waits, within the same bound, while
  another apt or dpkg process runs (for example an install in one of the run's
  background sessions). `apt-get update` runs at most once per 10 minutes per process.
- pip receives `--break-system-packages` only for an externally managed interpreter
  (PEP 668 marker) and not when `PIP_BREAK_SYSTEM_PACKAGES` is already enabled.
  No virtual environment is created.
- When a dependency is still missing after an install attempt, the report says so
  and asks the agent not to report results that depend on it.

Switches:

| Switch | Default | Effect when off |
|---|---|---|
| `EXCALIBUR_AUTOPROVISION` | auto | unchanged: off disables provisioning, suggest reports without running |
| `EXCALIBUR_PROVISION_RERUN` | on | the first run's output is kept, with the install note, and the command is not re-run |
| `EXCALIBUR_PROVISION_APT_LOCK` | on | apt runs at once, relying on the dpkg lock timeout |

Tests: `autoprovision_followup_test.go` covers the re-run and its switch, the
bounded step, the apt wait and its switch, the update interval, unconfirmed names,
the curated table, the pip bootstrap, the coverage of builtin tool names, and the
read paths. The older provisioning tests (`autoprovision_test.go`,
`autoprovision_w9_test.go`) were updated to the new result shape and to the
read-path contract. Ordinary tests use fake runners and never touch the host's
apt or dpkg state.

## Terminal sessions, environment, signals and process safety

This section covers the terminal session layer behind `exec` and `exec_io`, the
environment given to non-interactive sessions, signal handling for kills
issued by the agent, and the boundary of the task. Every behaviour below is
either on by default or is a correction to an erroneous value, and each
behaviour named in the table has its own switch.

### Exit status of signalled processes

A session whose process ends from a signal reports `128 + signal number`, the
shell convention, instead of `-1`. `Session.ExitSignal` returns the number and
`SignalName` renders the conventional name (for example `SIGSEGV`). The header
rendering of the name belongs to the exec result. This is a correction of an
erroneous value and has no switch.

### Standard input and prompts

Non-tty sessions read standard input from the null device, explicitly. A
program that asks a question reads end of file at once. A session that stays
silent while a process in its tree holds a terminal device open (the shape of
a prompt reading `/dev/tty`) receives a note in its output saying that the
prompt cannot be answered there. Switch `EXCALIBUR_PROMPT_DETECT` (default on).

### Autonomous session environment

Inside an exec scope (`excalibur exec`, which the eval and exec profiles use)
non-tty sessions receive defaults that only unattended runs need:
`PIP_DISABLE_PIP_VERSION_CHECK=1`, `PIP_BREAK_SYSTEM_PACKAGES=1`,
`PIP_ROOT_USER_ACTION=ignore`, and a fixed git author and committer identity
when neither the environment nor the repository configuration provides one.
Outside an exec scope nothing changes. Switch `EXCALIBUR_AUTONOMOUS_ENV`
(default on).

### Signal guard

The guard refuses a signal command whose target is the harness, one of its
process groups, or any ancestor of the harness process. It also refuses a
`pgrep` pattern whose matches reach a signal sender through a pipeline, such
as `pgrep -f pattern | xargs kill`. A `pkill` or `killall` pattern that matches
a live session of the same run is not refused: the command runs and its output
names the session it reaches. The switch `EXCALIBUR_SELF_KILL_GUARD` (default
on) controls all of these.

### Helper sessions and detached services

Sessions started with `background:true` detach into their own session and
survive the agent's exit (`EXCALIBUR_DETACH_BACKGROUND`, default on). Other
sessions are helpers: at run end their process groups receive SIGTERM and,
after the usual margin, SIGKILL, so no helper outlives the run. Switch
`EXCALIBUR_STOP_HELPERS` (default on). The run record lists the surviving
services as before.

The package exposes `ActiveSessions` and `ServiceSessions`, which report the
process id, command, liveness, start time, last output time, detachment and
whether the session's process tree contains a package manager or a build step.
`ProbePort` makes a fresh TCP connection to 127.0.0.1 and ::1. The finish
checks of the completion gate consume these accessors; this package only
provides them.

### Argument vector of the exec request

`excalibur exec` moves the request text out of its argument vector before any
other work. The text is written to a private file (mode 0600), the process
execs itself with `--instruction-file <path>` in place of the request, and the
environment variable `EXCALIBUR_INSTRUCTION_FILE` names the file. The
re-executed process reads the file once, removes it, unsets the variable and
does not hand off again. Process listings therefore never show the request.
Switch `EXCALIBUR_HIDE_INSTRUCTION_ARGV` (default on). The hand-off applies
only to the binary entry point on Linux.

### Task boundary

Inside an exec scope, a command or a file read is refused when it reaches
outside the task: paths under another process's root (`/proc/<pid>/root`,
judged through the harness's own root as well), container runtime storage
(`/run/containerd`, `/var/lib/containerd`, `/var/lib/docker`), the harness's
own log and state directories, `nsenter`, `chroot` into a host root, and bind
mounts of host paths. Symbolic links are resolved for reads. The refusal text
is `refused: outside the task boundary; work with the files the task provides`.
Switch `EXCALIBUR_TASK_BOUNDARY` (default on). The check is lexical on the words
of a command, so a path assembled at run time is not seen.

| Switch | Default | Scope | Effect |
| --- | --- | --- | --- |
| `EXCALIBUR_PROMPT_DETECT` | on | every non-tty session | Notes a silent session that holds a terminal device |
| `EXCALIBUR_AUTONOMOUS_ENV` | on | exec scope | pip and git defaults for non-tty sessions |
| `EXCALIBUR_STOP_HELPERS` | on | every run | Ends non-detached sessions at run end |
| `EXCALIBUR_TASK_BOUNDARY` | on | exec scope | Refuses commands and reads outside the task |
| `EXCALIBUR_HIDE_INSTRUCTION_ARGV` | on | `excalibur exec` on Linux | Moves the request out of argv |
| `EXCALIBUR_SELF_KILL_GUARD` | on | every session | Harness and ancestor protection; warnings for own sessions |
| `EXCALIBUR_DETACH_BACKGROUND` | on | background sessions | Detaches background services |
| `EXCALIBUR_NONINTERACTIVE_ENV` | on | every non-tty session | Pager and prompt defaults (unchanged) |

Backend coverage: the terminal session tests cover signal exit status, the null
standard input, the prompt note with and without a terminal device and with
the switch off, run-end stop of helpers and preservation of services, the
session accessors and process flags, and the probe. The signal guard tests
cover pgrep pipelines, an ancestor outside the platform's group rules, session
warnings and the allowed cases. The boundary tests cover the refused and
allowed command forms, scope and switch behaviour, symbolic links, the read
tool and the exec tool. The environment tests cover the defaults in and out of
scope, precedence of existing identities and of the repository configuration,
and the switch. The hand-off tests cover file mode, the loop guard, a real
re-execution through a helper process, and the entry point.

Package C verification: repository-wide vet and native, Darwin and Windows
builds pass. Race tests pass for the terminal tools, engine, CLI, binary entry
point and platform packages. The instruction hand-off also creates a missing
temporary directory in minimal containers; its regression test checks the
private file permissions and successful consumption.

## File, output and status tools; redaction write guard

This section covers the file tools (`read_file`, `write_file`, `edit_file`, `apply_patch`), `get_tool_output`, `status`, the listed-but-unwired tool stubs, `search_tools` with an empty query, `vision_extract`, and a write-side guard for redaction markers.

### Behaviour

- **Fingerprints.** `expected_fp` is optional. The harness records the fingerprint of every read and write in a run and checks an existing file against that record. A supplied fingerprint is an extra claim: a write proceeds when the file matches either the record or the claim. Empty and placeholder values (`*`, `**`, `UNAVAILABLE`, `none`, `unknown`, `-`) count as absent. A real external change is still refused. The refusal shows the current first lines and the fingerprint to retry with, so one retry succeeds.
- **Paths.** Relative paths in the file tools resolve against the run's project directory when the run declares one. Without a declared directory they resolve against the process working directory, as before.
- **edit_file.** A miss names the three most similar lines with their numbers. An ambiguous match lists every line it occurs on and accepts an optional 1-based `occurrence`. Exact matches are counted first; normalized and trimmed matches apply only when no exact match exists.
- **write_file content.** A JSON object or array given as `content` is written as indented JSON, keeping the key order the model wrote. A single-line `.json`, `.toml`, `.yaml`, `.yml`, `.ini`, `.cfg`, `.py` or `.sh` text that fails to parse, but parses once its literal `\n` sequences become newlines, is refused with `content has literal \n sequences; send real newlines`. A written `.json`, `.toml`, `.yaml` or `.yml` file that does not parse adds a warning line to the result. `.py` and `.sh` checks use the host `python3` and `sh` in syntax-only mode; they are skipped when the interpreter is absent.
- **get_tool_output.** Accepts a stored blob ref, a call id `call-N` printed by `list`, a terminal session id `sess-N` (its full log, paged with `offset` and `limit`), or `{"list": true}`, which shows the newest stored outputs with their size and producing call, then the terminal sessions. Unknown refs name every valid form.
- **status.** Reads only. It shows each session's last three lines and never runs or suggests the provisioner.
- **Listed but unwired tools.** Answer with a non-error statement that says nothing was done and what to use instead (for example, `ack_directive` reports that no directives are pending). They no longer name another subsystem.
- **search_tools with an empty query.** Lists every deferred tool with a one-line purpose. Schemas come from a named query.
- **vision_extract.** With no vision model configured, returns an error. It does not return placeholder text.
- **Redaction write guard.** `write_file`, `edit_file`, `apply_patch` and `exec` calls whose arguments contain a redaction marker (`‹secret:`) are refused before they run, including nested calls through `call_tool`. Output redaction is unchanged; the guard stops a marker from being written back into a file or a command.

### Switches

| Switch | Default | Effect when off |
| --- | --- | --- |
| `EXCALIBUR_FP_AUTO` | on | Explicit `expected_fp` is compared as given; `*` skips the check with a note. |
| `EXCALIBUR_TOOL_OUTPUT_REFS` | on | Only blob refs resolve (session ids still resolve under `EXCALIBUR_TOOL_FRICTION_REPAIR`); no list mode; earlier error wording. |
| `EXCALIBUR_REDACTION_WRITE_GUARD` | on | Marker-containing writes and commands run as before. |
| `EXCALIBUR_VISION_HONEST` | on | `vision_extract` uses the placeholder backend. |
| `EXCALIBUR_STRUCTURED_WRITE_CHECK` | on | No literal-escape refusal and no parse warnings. |
| `EXCALIBUR_RUN_WORKDIR` | on | Relative paths resolve against the process working directory. |
| `EXCALIBUR_EDIT_HINTS` | on | Earlier edit_file wording; `occurrence` ignored. |
| `EXCALIBUR_STUB_REPLIES` | on | Unwired tools return an error without the owner wording; `search_tools` empty query returns the earlier popular list. |

Object content in `write_file` is normalized under the existing `EXCALIBUR_TOOL_TOLERANT_ARGS` switch.

### Backend coverage

`internal/tools/filetools_test.go`: recorded-state writes, explicit and placeholder fingerprints, retry fingerprint, external-change refusals, legacy fingerprint switch, nearest-line misses, ambiguous matches and occurrence, relative paths against the run directory, object content, literal-escape refusal and parse warnings, shell and INI checks, vision without a model.

`internal/tools/outputrefs_test.go`: call ids, list mode with producers and sessions, session paging, error text naming valid forms, the output-refs switch, passive status and its switch, stub replies, the search catalogue, `call_tool` running a direct tool, and the redaction write guard for `write_file`, `edit_file`, `apply_patch`, `exec` and nested calls, with its switch.

`internal/vault/redact_test.go`: `ContainsRedactionMarker`.

Earlier tests updated because they encoded the replaced behaviour: `TestWildcardFingerprint` (the `*` placeholder is now checked, not skipped), `TestEditThreeLevels` (ambiguous-match wording with line numbers), `TestSessionOutputCompatibility` (session refs need both switches off for the legacy refusal), `TestStatusRendersSnapshotAndSessions` (empty status wording), `TestProvisionStatusSessionTail` (status never provisions), and `TestEmbeddedSurfaceDisabled` (unwired stubs reply without error).

### Limits

- The vision backend stays unconfigured in this checkout; `vision_extract` reports that no model is configured. Real image extraction needs the provider contract to accept image parts; the text-only contract in this checkout does not.
- Relative paths in `grep`, `glob` and `exec` still resolve against the process working directory unless the caller passes a workdir.

## Run loop, time and spend (loop guard v2, time footer, spend caps)

This section documents the run-loop and budget behaviour of non-interactive
runs (`exec`, `delegate`, `eval`). Interactive master turns do not use these
budgets.

### Wall budget

The wall budget of a non-interactive run is the real limit it is held to, and
it resolves in this order:

1. the explicit `--timeout` given to the process;
2. `EXCALIBUR_EVAL_WALL_SECS`, when positive;
3. 900 seconds.

The Harbor operator variables and an instruction-stated duration no longer set
the budget. The Harbor adapter passes its task timeout as `--timeout`. No
closing factor is applied to the budget: pacing and closing are read from the
budget itself (see the footer below). `budgets.json` records `wall_secs` and
`wall_source` (`explicit`, `env` or `default`).

### Time footer

When the run is time-only and has a wall budget, every tool result ends with
one compact line:

```
[time: 4m10s used, about 10m50s left]
```

At 50, 75 and 90 percent of the budget the line also carries one pacing note,
once per threshold: "check that the deliverables exist" (50%), "stop exploring;
verify and finish" (75%) and "finish now with what works; do not start new
work" (90%). Elapsed and remaining times round down. The footer does not reach
the trial records; it only shapes what the model reads. Switch:
`EXCALIBUR_TIME_FOOTER` (default on). With the footer off, the pacing advice
returns as volatile notes.

### Exact-call guard (loop guard v2)

Switch: `EXCALIBUR_LOOP_GUARD_V2` (default on). The guard counts one exact call
by its tool, its normalized arguments and a fingerprint of its result. The
fingerprint blanks volatile tokens (exec duration, session id, process id and
clock times) so one observation compares equal across runs.

- Pure `echo`, `printf`, `true` and `sleep` commands compare by result alone.
- Any workspace change resets the counts: a successful write or edit, a shell
  command that redirects to a file or calls a file-writing utility, a
  provisioner install, or input sent to a live session. A change does not lift
  an active refusal window.
- The third identical occurrence adds the note "you ran this exact call 3 times
  with the same result; change approach".
- The sixth identical occurrence adds a note and refuses that exact call for the
  next five turns. A refused call does not run; its result is the refusal text.
- A session that is still running (`alive: true`, `exited: false`) is progress,
  not a repeat, and is never counted.
- Repeated `finish` refusals with an unchanged reason count toward the same
  key, including normalized finish arguments, and open the same bounded refusal
  window after the sixth occurrence.

The guard never ends a run. It replaces the single-call repeat blocker and the
identical-repeat rule of the loop detector while on. With the switch off, both
behave as before, and the loop detector's staged response is no longer
terminal: the third and later stages repeat the strategy note.

### Spend caps

Switch: `EXCALIBUR_SPEND_CAP` (default on). In time-only runs two caps apply:

- the USD cost cap, `EXCALIBUR_COST_CAP_USD` (default 20);
- the input-token cap per run, `EXCALIBUR_INPUT_TOKEN_CAP` (default 30,000,000; fallback when cost is unknown or its cap is unavailable).

At 100 percent of a cap the agent receives one closing note: finish within the
next three turns. At 125 percent the run stops as paused with the budget
motive, and the report names the cap ("cost" or "input_tokens"). The step
safety cap no longer counts the USD cost cap. `budgets.json` records
`input_token_cap` and `spend_caps`.

### Default and delegate exec runs

Default and delegate `exec` runs use the same wall budget and spend caps as
eval runs (decision 0069). Set `EXCALIBUR_TIME_BUDGET_ONLY=0` to restore the
legacy step and cost limits.

### Tests

Backend tests: `internal/tasks/loops_test.go` (guard keys, counts, refusal
window, state change, pure and writing commands, live sessions, finish tally,
near-repeat escalation); `internal/engine/loopguard_test.go` (refusal window
end to end, live sessions, state change, pure commands, footer and pacing on a
fake clock, cost and input-token caps, switches off);
`internal/engine/deadline_test.go` and `timebudgetonly_test.go` (budget order,
footer format, thresholds); `internal/cli/budgetsonly_test.go` (budgets record).

The clock tool stays hidden in eval and delegate prefixes while time footers
carry the elapsed and remaining time. The full assistant prefix retains it.
Active session output also bypasses the older near-repeat and cycle rules,
so ongoing builds and installs do not trigger strategy-change notes.

### Finish verdicts and deliverable review

For explicit-finish runs, `EXCALIBUR_FINISH_VERDICT_RESULT` (default on)
puts the refusal, confirmation checklist, or terminal acceptance in the
finish tool result. A refused finish with no intervening tool work repeats
the reasons without another verifier call. Correcting invalid finish
arguments permits validation without a file edit.

`EXCALIBUR_DELIVERABLE_CHECKS` (default on) checks named outputs, the last
verification, program execution after edits, running work, proof placeholders,
and declared local services. `EXCALIBUR_INPUT_SNAPSHOT` (default on) captures
provided inputs up to 50 MB and reports unexpected changes as warnings.
`EXCALIBUR_FINISH_CHECKLIST` (default on) lists concrete instruction
requirements and inspection warnings in the confirmation round.
`EXCALIBUR_SOFT_VERIFIER` (default on) limits verifier refusals to one;
later or incomplete verdicts are advisory. These checks apply to explicit
finish profiles; interactive assistant profiles retain their existing flow.

A mandatory safety bound ends explicit-finish runs with pending items after
25 consecutive refused finishes without other tool work. Runs with no
configured budget stop after 200 turns. The report records the safety
reason. These bounds prevent unbounded history growth from a repeating
provider and remain active independently of optional review switches.

## Provider resilience, run records and evaluation reporting

This section covers the provider retry policy, the credential quarantine, the
run and trial records, and how evaluation reports classify infrastructure
failures. Model calls use the gateway policy in `internal/models`; the records
are written by `internal/eval` and `internal/cli`.

## Attempts isolation and promotion (best-of-N)

Best-of-N (Section 18.2) runs every attempt in its own full copy of the real
working directory. The real directory is written only by the promotion of the
selected attempt, so the attempts cannot overwrite one another or the deliverable
paths of the request.

### Mechanisms

- **Full copies.** Each attempt copies the real directory in full, including
  ignored and untracked build products and `.git`, under an attempt root that
  keeps the real path layout (`<root>/app` for `/app`). The attempt-local
  `.excalibur` state is never copied. Git worktrees are no longer used: they
  carry only the committed tree and drop the untracked inputs an attempt needs.
- **Workdir mapping.** Absolute paths under the real directory, relative path
  arguments and shell commands in an attempt's tool calls resolve onto that
  attempt's copy before the tool runs. A command with no `workdir` runs in the
  copy. The instruction text the model reads is never rewritten. Records keep
  the arguments the model sent.
- **Selection.** Attempts are ranked, in order, by: the named outputs of the
  request exist in the copy and the attempt did not fail; the attempt changed
  files (an attempt that changed nothing never wins over one with passing
  changes); more verified checks; fewer changed files; completion state; fewer
  tool errors; launch order. The named outputs come from the same extraction the
  completion gate uses (`tasks.ExtractArtifactRequirements`) and are checked with
  its checks (`tasks.CheckArtifactFiles`).
- **Promotion.** Only the winner's changes move into the real directory:
  changed, added and deleted paths, measured against the baseline taken before
  the copies. Each replaced entry is backed up first. The plan is refused before
  any write when it would replace a directory that still holds entries the winner
  did not change. After the copy the real directory is re-hashed and every applied
  path is compared with the winner. Any failure restores the previous state.
- **Real-directory audit.** Changes to the real directory made during the fan-out
  (a command that escaped the mapping) are reported in an
  `attempts_real_dir_touched` event. They are not promoted and not reverted.
- **Fallback.** When the promotion fails, or the apply moves zero files for a
  winner that changed files, the run records the reason and runs one normal
  attempt in the real directory with the remaining time. The fallback's records
  are merged with the attempt index one past the last attempt and
  `fallback: true`. A named output still missing after the promotion (re-checked
  on the real directory) ends the run as `completed_with_pending`.
- **Records.** `attempts.json` adds `isolated`, `applied_files`,
  `applied_file_count`, `fallback`, and per attempt `changed_files`,
  `deliverable_missing` and `passes`. The outer `tool_calls.jsonl`,
  `model_calls.jsonl` and `gate_attempts.jsonl` carry every attempt's rows with an
  `attempt` tag.

### Switches

| Switch | Default | Effect |
| --- | --- | --- |
| `EXCALIBUR_PROVIDER_RETRY_V2` | on (`0` disables) | Exponential retry with jitter, Retry-After honored, network failures retried, one continuation for an output-limit cut. |
| `EXCALIBUR_MODEL_REQUEST_TIMEOUT` | `300` seconds | Deadline for one provider request; `0` disables it. A timed-out request is retried. |
| `EXCALIBUR_QUOTA_PROBE` | on (`0` disables) | A quota or billing response quarantines its credential only after a minimal probe on the same credential repeats the evidence. |
| `EXCALIBUR_RUN_RECORDS_V2` | on (`0` disables) | Real exec outcomes, output tails and elision metadata, final finish details, and protection of completed trial finals. |
| `EXCALIBUR_EVAL_INFRA_CLASSIFICATION` | on (`0` disables) | Report and triage identify infrastructure trials independently of reward, including trials without a reward. |

The existing `EXCALIBUR_EXHAUSTED_KEYS` and `EXCALIBUR_BUDGET_RETRIES`
switches keep their meaning. The per-request deadline remains active when
budget retries are disabled.

### Retry policy

- Rate limits (429) and overloads (5xx) are retried with exponential backoff:
  1 s, 2 s, 4 s and so on, capped at 60 s. Each step is jittered down by up to
  20 %. A server `Retry-After` or "try again in" hint replaces the computed step.
- Connection resets, request timeouts and decode failures are retried up to six
  tries per endpoint. Rate limits and overloads keep retrying until the caller's
  wall budget ends; the sleep is cut at the deadline, so the total wait never
  exceeds the remaining budget.
- Authentication, billing and input errors are not retried.
- A response cut at the output limit gets one follow-up call before the gateway's
  existing token doubling. A cut tool call is sent again unchanged; cut text is
  continued from the partial answer with an instruction to finish concisely.
  Usage from both calls is summed.

### Credential quarantine

- A quota or billing response (HTTP 402, or OpenAI-style 429 with a recognized
  billing identifier) is not enough on its own. With the probe on, a minimal
  request on the same credential must repeat the evidence before the credential
  is quarantined for the process.
- Rate limits never quarantine and are never probed.
- After a confirmed exhaustion the fallback credential serves the call. The
  switch is counted in `CallMeta.CredentialSwitches` and recorded on the model
  call as `credential_switches`.

### Run records

- `tool_calls.jsonl`: `exit_code` and `is_error` for the exec tool come from the
  exit status in the output header. `header_exit` records that status (absent
  for a running session). `model_elided` is true only when the model received an
  elision marker. `stdout_inline` is a 4 KiB head preview; longer outputs also
  keep `stdout_tail` (the last 2 KiB). The blob holds the complete model-facing
  text.
- `model_calls.jsonl`: `provider_error_reason` names the failure with an
  allow-listed token (`billing`, `rate_limited`, `http_5xx`, `timeout`, `tls`,
  `auth`, `connection`, ...). Messages are never recorded.
- `final.json` (trial record): `final_exit` is the exit status of the last exec
  command; `summary`, `final_text` and `finish_evidence` carry the accepted
  finish.
- `events.jsonl`: `run_finished` carries `final_text` and `evidence` when the
  run completed through finish. A run that is stopped by its deadline or a signal
  always ends with one `run_finished` row whose state is `timeout`.
- The run summary is the finish summary or a generic state name (`completed`,
  `failed`, `blocked`, `paused: budget exhausted`). It never repeats the task
  request.
- Once a finish is accepted, a later provider or engine error is recorded as a
  `post_finish_error` event with its class only; the run state stays completed.

### Infrastructure classification

`bench report` and `bench triage` mark a trial as infrastructure when:

- the agent install failed (sha256 mismatch on the installed binary);
- the environment did not start (container compose failure);
- the trial made no successful model call and recorded a provider error;
  billing and quota responses are reported as `provider_billing`;
- the verifier produced no test results (no pytest ctrf report, zero tests,
  a verifier timeout or no reward file);
- the trial was cancelled before the harness recorded a terminal state.

Infrastructure trials are excluded from the headline pass rate, pass@k, pass^k
and the per-task rates. The report also gives the rate with infrastructure
trials included at their recorded reward, and lists the infrastructure reasons.

Verifier output is inspected only for CTRF test counts or a completed pytest
summary with a positive outcome count. Test names and verifier text are never
sent to the triage model.

## Harness reliability H: self-checks, error cards, eval tool surface and working method

This section records four changes, decided in
`docs/decisions/0072-checks-cards-method-tool-surface.md`.

### Error-triggered skill cards (EXCALIBUR_CARDS_STRICT, default on)

A card is appended to a failed tool result only when all of the following hold.
The exec result has a non-zero numeric exit in its first header, and the output matches a
concrete error pattern for the card. The command is not a probe: `test`, `[`,
`which`, `command -v`, `grep`, `diff` and similar checks, where a non-zero exit
is a normal answer. The same output does not show a successful install or a
verified working command for the binary the error names. The card name has not
fired earlier in the same run. Each card fires at most once per run. The
switch off restores the earlier rule.

### Verifier verdict retry (EXCALIBUR_VERDICT_RETRY, default on)

An empty or truncated verifier reply is retried once. The retry doubles the
output budget, capped at 32768 tokens, and keeps the smaller prompt. A verdict
still missing after the retry is journaled as `verifier_no_verdict` with its
shape: call site, budget, output tokens, and whether the reply was empty or
truncated. The reply text is not recorded. The call returns
`tasks.ErrVerifierNoVerdict`. This distinguishes missing evidence from an
explicit verdict. Advisory handling in the finish gate belongs to the finish
work package; this change alone does not implement that handling. The switch off keeps the original budget
on the retry.

### Exercise predicate (EXCALIBUR_EXERCISE_STRICT, default on)

`tasks.ExercisesDeliverable` decides whether a command ran, queried or parsed
something on real input. The following do not count: help and version output
(`--help`, `-h`, `--version`, for interpreters and local programs), syntax-only
checks and compilation alone (`gcc`, `javac`, `rustc`, `go build`,
`cargo build`, `bash -n`, `gcc -fsyntax-only`, `py_compile`, `node --check`, `ruby -c`,
`perl -c`, `php -l`), `python -c` code that only imports modules, and display or
observation commands (`cat`, `ls`, `stat`, `test`, `wc`, `echo`). A command
counts when at least one of its simple commands is an exercise. The predicate is
exported for the finish gate. The switch off restores the marker rule.

### Working method evidence rules (EXCALIBUR_METHOD_V2, default on)

The working method keeps its original eight steps and adds nine short rules
(steps 9 to 17): copy provided data before changing it, test the success path,
check beyond the examples with an independent reference, measure the way the
instruction measures, pass values through files or pipes, run scripts on real
input after their last edit, keep required services running, try other routes to
a source, and use only the task's own files and tools. The block stays under
about 2,600 characters because it is the shared L0 prefix. The switch off
reproduces the original block byte for byte, which a golden test pins.

### Eval tool surface

The eval core table drops `ack_directive`, `leave_continuation_note`,
`subagent` and `task_db`. The reasons are recorded in
`docs/decisions/0072-checks-cards-method-tool-surface.md`. The assistant and
delegate profiles keep the earlier core through `cli.EffectiveTools`.

| `EXCALIBUR_ATTEMPTS` | `1` (off) | Number of attempts; `1` keeps the single-run path. |
| `EXCALIBUR_ATTEMPTS_DELIVERABLE` | on | Named outputs decide selection and the post-promotion re-check. `0` selects on the change and verification signals only. |
| `EXCALIBUR_ATTEMPTS_FALLBACK` | on | A failed promotion falls back to one normal attempt in the real directory. `0` ends the run as failed. |
| `EXCALIBUR_ATTEMPTS_PATH_MAP` | on | Maps each attempt's tool paths onto its copy. `0` is an ablation only: absolute paths reach the real directory again. |
| `EXCALIBUR_ATTEMPTS_FULL_BUDGET` | on | Each attempt keeps the full step and USD budget. `0` restores the legacy split. |
| `EXCALIBUR_ATTEMPTS_MERGE_RECORDS` | on | Merge the attempts' records into the outer trial records. |

The switches are read on every run. They apply only to fan-out runs, so the
assistant profiles keep the single-run path unless `EXCALIBUR_ATTEMPTS` is
greater than one.

### Known limits

- Path mapping covers path-valued arguments and shell command text that names
  the real directory literally. A path computed at run time (for example from an
  environment variable, or inside a script the attempt runs) is not mapped and
  can reach the real directory. The audit event reports such writes.
- The copies are full, so the disk cost is one copy of the directory per attempt.
- Ignored files are promoted when the winner changed them: the promotion does not
  filter by `.gitignore`, so build products are included.
- Commits and other changes inside `.git` are not promoted; the attempt's
  repository state is not carried back to the real directory.

### Tests

`internal/attempts` covers the copy layout and isolation, build-product
promotion, mode and link preservation, deletions, rollback after an injected
failure, the refused replacement of a populated directory, escaped writes, the
workdir mapping rules, the deliverable checks and the switches.
`internal/cli/attempts_isolation_test.go` covers losing attempts that never reach
the real directory, named-output selection, the apply failure fallback with and
without its switch, the tool-path runner and the artifact checks through the map.

## Loop reliability: completed background commands (F1)

`EXCALIBUR_BACKGROUND_YIELD` defaults on. Background exec calls wait through
the ordinary yield window. Commands that finish return the ordinary exit
header and their complete output; only live sessions report detached service
metadata. Completion waits for the log drain, including output larger than a
single read. Set the switch to off to restore the short background yield.

## Loop reliability: finish error evidence (F2)

`EXCALIBUR_FINISH_ERROR_ENTRY` defaults on. A last-command refusal names
its own command, exit status and output tail. Its suggested check is joined
by call ID to that same failing history entry, including when later probes
have completed. Successful later calls keep their normal success semantics.

Error refusals take precedence over criterion keyword matching: a filename in
an error cannot replace its retry command with an unrelated draft check. Error
excerpts omit executor path suggestions and engine clock footers so the actual
process error remains visible.

## Loop reliability: unchanged verification reminders (F3)

`EXCALIBUR_RECHECK_GUARD` defaults on. With an open finish refusal, eight
consecutive executed calls whose output repeats earlier content and whose
workdir stays unchanged produce a reminder naming the first open refusal
item. Further reminders are spaced eight calls apart. This guard never stops
or refuses work. Novel output, filesystem changes and live sessions reset the
streak. Parallel batches count only when the whole batch leaves the workdir
unchanged. Snapshots include content, metadata and symlinks; incomplete scans
(over 20,000 entries, 50 MiB, unreadable or special files) suppress reminders.

## Loop reliability: autonomous credential redaction (F4)

`EXCALIBUR_REDACT_PATTERNS_AUTONOMOUS` defaults off. Non-interactive exec,
delegate and eval entry points redact known literal credentials and their
base64/URL encodings: current vault values, resolved primary/fallback provider
keys (including config and operator-file values), and credentials explicitly
injected through the embedded catalog. Candidate patterns read from workspace
files remain visible so credential auditing and replacement can operate on
real data. Set the switch on to also redact candidate patterns. Assistant and
daemon catalogs retain pattern redaction. Known short literals stay protected
in autonomous runs, and vault changes take effect without rebuilding tools.


## Loop reliability: cost-first spend protection (F5)

The default input-token cap is 30,000,000. `EXCALIBUR_COST_PRIMARY_SPEND`
defaults on: when usage has a known positive price and a finite positive cost
cap, cost governs spend without an independent token stop. Any unpriced usage
(including verifier calls) or unavailable cost cap keeps the token fallback.
A later priced call cannot erase earlier unknown cost. Set the switch off to
apply both caps independently. `EXCALIBUR_INPUT_TOKEN_CAP` still overrides the
fallback token allowance; closing and stop shares remain unchanged.

## Loop reliability: inspect data formats before computing (F6)

`EXCALIBUR_METHOD_DATA_FORMAT` defaults on with method V2. The L0 method asks
for a format check (separators, decimal commas, units and header), plus a few
parsed values next to raw text before computing. Other evidence rules use
shorter wording to keep the full block within 2,600 characters. Turning the
switch off omits this line; method V2 off retains the original golden block.

## Provided check provenance (F7)

`EXCALIBUR_SCOPE_PROVIDED_CHECKS` defaults on. Scope guard expectation
tracking applies to files present in the initial snapshot and files under
explicitly named test directories. Agent-authored inline checks and temporary
scripts do not become task requirements after failing. For file checks, Python
assertion messages, comments and insignificant whitespace are ignored; changes
to conditions, expected literals and tolerances remain finish violations.
Setting the switch to `0` restores legacy inline and verbatim tracking.
This preserves the verification responsibilities in Sections 7.3 and 8.

## Elided exec output digest (F8)

`EXCALIBUR_EXEC_OUTPUT_DIGEST` defaults on. Elided terminal output includes
its full line count and a compact list of search-result paths with hit counts,
including hits outside the visible head and tail. The list shows up to 60 paths
and reports how many remain. Long path displays are abbreviated. The digest
shares the existing inline byte budget; full output remains available through
`get_tool_output`. Set the switch to `0` to omit the digest.

## Git history rewrite advisory (F9)

`EXCALIBUR_GIT_HISTORY_WARNING` defaults on. Exec results warn when a command
rewrites or discards Git history or refs and the literal instruction does not
mention history, commits or rewriting. This covers filter-branch, filter-repo,
interactive or root rebase, hard reset to another commit, forced push, amend,
ref deletion, reflog expiration and immediate garbage collection pruning.
The same advisory appears in the finish confirmation checklist after such a
command. It never refuses execution or completion and does not alter exit status.
The engine supplies the literal instruction through an additive tools context
helper. Set the switch to `0` to omit both warnings.

### Exec output limits (F10)

`EXCALIBUR_EXEC_HEAD_LIMIT` defaults on. Completed exec output ending in a
pipe to `head -n N`, `head -N`, or `head` (10 lines), or a final `grep -m N`
stage, receives an advisory note when the captured output reaches that limit.
The note recommends counting results or listing matching files before relying
on the list. It uses output before harness elision. Only a single pipeline is
inspected: intermediate stages and multi-command output cannot be counted
reliably from the final terminal output. Set the switch to `off` to disable it.

### Broad final verification (F11)

`EXCALIBUR_BROAD_REVERIFICATION` defaults on. The L0 working method asks for
an all-files broad pattern search after removing or replacing every occurrence.
When the literal instruction combines `all`, `every`, or `any` with a removal,
replacement, deletion, sanitization, or redaction verb, finish confirmation adds
an advisory item asking for that search and its result. This item does not
refuse completion. The guidance applies to interactive and autonomous work;
`EXCALIBUR_METHOD_V2=off` still restores the legacy L0 method, and
`EXCALIBUR_FINISH_CHECKLIST=off` still disables finish confirmation items.
Set `EXCALIBUR_BROAD_REVERIFICATION=off` to disable both additions.

### Engine command positions (F12)

`EXCALIBUR_COMMAND_POSITION_CHECK` defaults on. Finish checks parse shell
executable positions, excluding comments and argument text, and prefer live
session process flags when available. Exited sessions never block completion.

### Engine confirmation progress (F13)

`EXCALIBUR_FINISH_CONFIRM_PROGRESS` defaults on for explicit finish rounds.
Pending confirmations appear in tool result footers. Twelve consecutive calls
without file changes add a stronger reminder. At 95% of the wall budget, an
unchanged, fully snapshotted workspace completes the previously accepted finish
with the recorded reason `confirmation pending at deadline`. File changes or
incomplete snapshots prevent this automatic completion. The same acceptance
check runs when an in-flight call reaches the context deadline, using a bounded
persistence context.

### Engine display and probe re-checks (F14)

`EXCALIBUR_RECHECK_PROBES` defaults on. The advisory re-check guard observes
pending confirmations as well as refusals. Calls composed entirely of display
or probe commands count as re-checks when their workspace snapshot is unchanged,
even when output text varies. Every eight consecutive re-checks adds advice;
this guard never ends the run. File changes and live session output reset it.

### Engine confirmation checklist ranking (F15)

`EXCALIBUR_CHECKLIST_LIMIT_RANKING` defaults on. Confirmation checklists split
sentences and list entries, normalize duplicates, discard entries shorter than
four words, and rank numeric limits ahead of descriptive obligations or named
outputs. Scientific notation, comparators, percentages and limit words are
recognized. Up to eight entries appear, preserving source order within a score.
Disabling the switch restores the previous extraction.

### Exec history confirmation

`EXCALIBUR_GIT_HISTORY_CONFIRM` defaults on. An exec command that rewrites
Git history or refs is refused before execution when the instruction does
not request history changes. Repeating the exact command in the same run
and workdir acknowledges the warning and permits execution. The existing
`EXCALIBUR_GIT_HISTORY_WARNING` controls the result and finish checklist
advisory independently. Setting the confirmation switch to `0` restores
immediate execution. This protection applies to all exec callers.

### Instruction deliverables

`EXCALIBUR_INSTRUCTION_DELIVERABLES` defaults on. Finish checks take their
deliverable set exclusively from strict extraction of the instruction's
named output paths. Files written by the agent still inform edit freshness,
but scratch scripts and other unnamed files do not become deliverables or
require execution. Setting the switch to `0` restores write-history
inference. This applies to all profiles.

### Check refusal hints

`EXCALIBUR_PLAIN_CHECK_HINTS` defaults on. Verification refusals that suggest
repeating an exec check decode its command from the tool arguments and show
plain shell text. Hints show only the first line, capped at 160 characters,
with `…` when text is omitted. Setting the switch to `0` restores the
previous JSON argument excerpts. This applies to all profiles.

## Finish evidence after loop refusals

`EXCALIBUR_FINISH_EXECUTED_ERRORS` defaults on. Explicit finish checks use the
last executed tool result when a repeat guard rejects a later call. Guard
refusals remain visible as error results in the conversation and audit history;
they do not become evidence that a process failed. An actual failed command
before the refusal still prevents completion. Setting the switch to `0` restores
the previous last-result behavior. This applies to the explicit finish protocol;
assistant completion policy is unchanged.

`TestFinishAfterClockRepeatRefusal` uses a fixed engine clock and both constant
and advancing clock-tool results. `TestRepeatRefusalPreservesExecutedFailure`
covers both repeat guards, the original failing exit, and switch-off behavior.
The CLI trajectory past 25 steps has an explicit 60-second budget and a bounded
15-second test context; completion does not depend on crossing a clock second.

The same executed-history view supplies deterministic artifact checks, verifier
requests, blocked-claim evidence and actionable refusal hints. This prevents a
rejected repeat of a successful verification command from failing the next
finish. `TestFinishVerificationExcludesUnexecutedRepeat` checks this path and
confirms the original refusal remains in the model/audit history.

## Skills-off enforcement (Sections 13.2–13.4)

The existing `EXCALIBUR_SKILLS=off` switch removes `search_skills` and
`load_skill` from the core and deferred discovery surfaces and refuses
direct calls and calls through `call_tool`. Unset retains the existing
assistant defaults. Tests cover all CLI profiles, discovery, direct execution,
and wrapped execution.

## Instruction restrictions in the working method (Section 7.3)

`EXCALIBUR_METHOD_NO_OUTSIDE_CODE` (default ON) adds a concise rule to the
v2 method: when the instruction forbids outside code or dependencies,
do not download or copy them; build from available tools. This is guidance.
The method still fits the 2,600-character budget; the v1 ablation golden
is unchanged. The existing broad-search rule is shortened without changing
its meaning.

## Recoverable provided inputs (Sections 7.5–7.6)

`EXCALIBUR_INPUT_COPIES` (default ON) extends the existing run-start input
snapshot with original bytes under the run's private `input-originals`
directory (mode 0700). Copying requires `EXCALIBUR_INPUT_SNAPSHOT` and a
run-state destination. Exec supplies that destination; embedded engine callers
can supply it with `SetInputCopyDir`.

Eligible trees totaling at most 64 MB are copied. Larger trees and walks
reaching the file-count bound retain metadata only. Build/cache directories,
symlinks, and the copy destination are excluded. Reads are confined to the
workdir. Copies are retained with run state. Changed or deleted inputs that
the instruction did not ask to modify get an advisory original-copy path and
a shell-quoted `cp -a` restore command in the finish checklist.

## Literal instruction examples (Sections 7.5–7.6)

With `EXCALIBUR_CHECKLIST_EXAMPLES` (default ON), the confirmation checklist
includes up to five deduplicated literal input/output pairs from function
equalities, prose examples, arrows, or labelled fenced input/output blocks.
Each asks for reproduction and at least one input outside the examples.
These are advisory reminders: they add no refusal condition and the normal
second finish can complete without machine-readable example evidence.

## Explicit installed deliverables (Sections 7.5–7.6)

`EXCALIBUR_INSTALLED_DELIVERABLES` (default ON) extends strict artifact
extraction to named paths under /usr/local/bin, /usr/local/lib,
/usr/local/share, and /opt. The instruction must name the path and ask to
create, install, place, or build it. Mere mentions, examples and negated
installation instructions do not cause checks. All such paths must exist;
paths containing /bin/ must be executable files. Libraries and shared data
need no execute bit. Basenames inside installed paths do not create duplicate
workdir-relative requirements. Existing artifact-gate/profile controls remain
in force; disabling the switch restores the previous root set.

### Exec instruction arguments

`EXCALIBUR_EXEC_LEADING_DASH` (default on; `0` disables) lets `exec`
accept dash-prefixed positional instructions, including bullet lists. Known
options retain their normal parsing rules, `--` ends option parsing, and the
last positional argument is the instruction. The private instruction-file
handoff places its option before `--` so re-execution preserves these rules.

### Bounded autonomous completion checks

For explicit exec/eval finishes, `EXCALIBUR_FINISH_REFUSAL_CAP` (default on)
lets each refusal item block at most twice per run. Later occurrences appear
as advisory items in the confirmation checklist. The freshness warning shares
one run-wide allowance even when its suggested check changes. Unrelated new
problems retain their own allowance. Assistant verification is unchanged.

`EXCALIBUR_VERIFICATION_FRESHNESS` (default on) examines shell command syntax
when identifying mutations. Words inside verification script arguments no
longer invalidate edits that already have a later passing check. Checks may
write their own logs or follow a mutation in the same command; a later edit
still invalidates that evidence. Polling an exercised live service keeps its
verification fresh.

`EXCALIBUR_SERVICE_EXERCISE` (default on) counts a live program session plus
a later successful localhost client command, or a client check against its
reported port, as exercising a service. A failed client or a stopped session
does not satisfy this check. A service does not have to exit to pass.

### Artifact gate precision

`EXCALIBUR_STRICT_ARTIFACT_GATE` (default on) limits hard filesystem checks
to absolute paths paired with an explicit creation, installation or writing
instruction. Relative paths and prose file names remain advisory. Existing
absolute-path root and example exclusions still apply.

`EXCALIBUR_TYPED_ARTIFACTS` (default on) checks artifacts according to their
filesystem type. Output directories pass when they contain entries. Sockets
and FIFOs pass by existence, without content reads or execute-bit checks.
File-based program and content heuristics skip these special objects.

### Delivery time reserve

`EXCALIBUR_FINISH_TIME_RESERVE` (default on) reserves the end of an autonomous
exec/eval wall budget for delivery. From 70%, finish gate refusals appear as
advisory confirmation items, and the run receives one message: "time reserve:
finish now with what works; refusals are advisory from here". From 85%, a
valid finish call or an already pending confirmation completes immediately,
without another verification pass. This uses elapsed wall time and works with
time-only budgets. Interactive assistant verification retains its policy.

### Open terminal sessions and services

`EXCALIBUR_SURVIVE_SESSIONS` (default on) preserves open sessions at autonomous
run end. `EXCALIBUR_STOP_HELPERS` still stops identified package-manager and
build process trees; a listening TCP socket anywhere in the tree protects the
session from cleanup. Turning survival off restores the earlier cleanup policy.

In exec scope, ordinary non-PTY commands use independent Linux process sessions
and write stdout/stderr directly to persistent log files. Polling tails those
files during the run, so services keep producing output after the harness
exits. PTY sessions retain a real terminal and interactive input; an independent
output pump holds their terminal open and records output after harness exit.
Surviving foreground sessions are included in the service audit record.
Interactive assistant terminal behavior retains its existing contract.

### Round 2: stable request prefixes (engine, C1)

`EXCALIBUR_STABLE_PREFIX` defaults on. A run retains its initial system,
identity, scope, task instruction and tool definitions. Changed scope/task
facts travel as versioned activity deltas with explicit retirement of old
facts. Compaction refreshes L3 at the Section 11.4 boundary. The volatile
control tail remains outside the stable prefix; observation masking retains
its existing batched policy. Setting the switch to `0` restores re-rendering.

The 20-turn offline fake-endpoint regression retains 100% of each previous
stable prefix. In its scripted fixture, 95.70% of complete serialized request
bytes match the preceding request prefix (96.69% excluding volatile state).
These are byte measurements, not estimates of billed cache tokens.

### Round 2: bounded control guidance (engine, C2)

`EXCALIBUR_CONTROL_TAIL` defaults on. Control guidance replaces the prior
note with the same issue category, evicts oldest categories as needed, and
caps the complete volatile layer to `EXCALIBUR_CONTROL_TAIL_BYTES` (default
2048 bytes). Full notes, including retired versions, remain in `control_note`
audit journal entries. `0` restores cumulative guidance.

Literal user directives are promoted to append-only activity before applying
the control cap. Their complete instruction text is retained even when longer
than the control allowance, and unchanged snapshot directives are not repeated.

### Round 2: versioned verifier verdicts (engine, C3)

`EXCALIBUR_VERDICT_REUSE` defaults on. Validation, confirmation and completion
reuse an explicit independent verdict for unchanged instruction, criteria,
verification settings, executable evidence and inspected file contents/modes.
Identical repeated checks share an evidence version. Delivery prose does not
force another review. Missing replies and provider errors are never cached.
Setting `0` requests a new review on each attempt.
New user directives participate in the instruction version and reach the
reviewer even when verdict reuse is disabled. Their complete literal text
is also pinned in structured compaction state.

### Round 2: dependency freshness (engine/tasks, C4)

`EXCALIBUR_EVIDENCE_DEPENDENCIES` defaults on. Successful checks retain
content/mode receipts for deliverables and literal files read or executed,
including local script imports and literal data references. Gate evaluation
compares these versions with the filesystem, so unrelated diagnostic writes
do not expire a passing check. Older histories without receipts use command
mutation targets and dependency path overlap conservatively. Unknown mutation
targets expire old evidence. `0` restores the global mutation frontier.

Dependency receipts also cover implicit project source/config inputs and source
manifests for standard language runners. Adding source files expires the old
check; creating unrelated diagnostic files does not. Literal local data and
import dependencies are followed without executing discovery code. Discovery
is bounded; incomplete or unreadable receipts use conservative history checks.
Receipt capture runs when each tool finishes, before waiting for batch peers.

On Linux, call-scoped workspace access monitoring adds actual read paths,
including dynamically constructed data filenames, to the dependency receipt.
It stops before hashing the receipt. Concurrent checks can conservatively share
read dependencies; files outside the workspace still use literal references
and deliverable tracking. Monitor unavailability retains static discovery.

Explicit source operands and inline scripts retain narrow read/import
dependencies; project-wide source manifests apply to implicit project runners.

Artifact edit tracking uses write destinations, excluding names mentioned only
inside diagnostic contents. Content-version receipts take precedence over
ambiguous historical filename matches; service-client evidence follows the
same dependency freshness rule as finite program checks.

### Round 2: explicit failure markers (engine, C5)

`EXCALIBUR_PRECISE_FAILURE_MARKERS` defaults on. Nonzero exits and explicit
assertion, traceback, FAIL and nonzero failed-count markers indicate failed
checks. Line-leading `error:` diagnostics require a known compiler/build/test
tool origin and a nonnumeric diagnostic. Numeric error measurements on exit
zero remain valid observations. `0` restores the older textual classifier.

### Round 2: authoritative terminal state (engine/tasks/tools, C6)

`EXCALIBUR_SESSION_AUTHORITY` defaults on. All finish checks receive current
session-manager facts. Exited, closed and evicted sessions retain compact
terminal tombstones, including exit status when known. Historical output can
no longer keep a completed job alive or apply one listing entry's state to
another session. Histories replayed without a session manager retain their
recorded-state fallback. `0` restores historical liveness interpretation.

### Round 2: complete payload cache telemetry (CLI, C7)

`EXCALIBUR_CACHE_PAYLOAD_TELEMETRY` defaults on. Message accounting hashes
and sizes the complete serialized message, with separate tool-call and
tool-result entries and a full-request fingerprint. This exposes argument,
result and masking changes without storing another payload copy. Cache-read
and cache-write token counters remain separate. `0` keeps content-only message
accounting. No provider or gateway protocol changes are involved.

Payload sizes are overlapping diagnostic views (request, messages and tool
components). Token and cost totals continue to use provider usage counters.

### Round 3: explicit time reserves (CLI and engine, T2)

`EXCALIBUR_TIME_RESERVE_EXPLICIT_ONLY` defaults on for non-interactive runs.
A positive `--timeout`, `EXCALIBUR_EVAL_WALL_SECS`, or an existing configured
legacy wall limit has source `explicit`; the built-in 900-second guidance has
source `default`. `budgets.json`, budget events and `budget.jsonl` record
`wall_source`. `budgets.json` and budget events also record `hard_wall_secs`.

Explicit limits keep the 70% advisory reserve and 85% finish acceptance.
With the fallback, the one-time 70% notice describes an estimate, refusals
retain their two-strike cap, and neither the 85% reserve nor the older 95%
pending-confirmation rule accepts a finish automatically. Time guidance also
labels the unknown limit. Engine and CLI hard stops use four times the fallback
estimate (3600 seconds); an earlier parent deadline or termination still wins.
This safety bound applies even while commands are active. Independent spend
and step policies remain in effect.

A running build or test from this run prevents finish acceptance until it
exits, including after repeated finish calls. Only an explicit limit past 85%
permits the reserve override. Session history and live process facts cover
foreground and background commands; service completion keeps its existing
policy. Interactive assistant runs keep their configured limits. Set the
switch to `0` to restore the original reserve and fallback hard-stop behavior.

### Original input restoration

`EXCALIBUR_INPUT_RESTORE` defaults on. Non-interactive runs expose
`restore_input` for preserved manifest files only, optionally to a disposable
path inside the task tree. Restoration verifies SHA-256 before writing and
confines destination symlinks. Private logs remain inaccessible. Disabling
this switch removes the tool; snapshot and copy switches remain independent.

### Default command preflight

`EXCALIBUR_DEFAULT_PREFLIGHT` defaults on. The working method asks for a small
input check of the requested default command through final file creation before
expensive work, reusing fresh preflight evidence. Overrides, accepted API calls
and progress messages do not establish the default path or final serialization.
This guidance introduces no mandatory extra execution or completion refusal.

### Literal interface review

`EXCALIBUR_LITERAL_CONTRACT_REVIEW` defaults on. The independent reviewer compares
the request's literal interface and objectives against both implementation and
executor tests, flagging correlated mistakes in clients or tests. This prompt
addition preserves existing refusal caps and does not add review rounds.

### Path templates

`EXCALIBUR_PATH_TEMPLATES` defaults on. Artifact extraction excludes entire
path tokens containing unresolved angle/braced placeholders, variables, wildcards
or ellipses before matching paths or basenames. Direct artifact checks also skip
templates; removing placeholders cannot introduce a leftover file obligation.

### Live job observations

`EXCALIBUR_LIVE_JOB_PROGRESS` defaults on: yielded exec calls and session reads
or waits report STILL RUNNING, session identity, elapsed time, last-output age
and bytes produced. `EXCALIBUR_LIVE_JOB_DUPLICATES` defaults on: each new launch
warns once when a live session has the same command in the same directory or
a shared literal output destination (redirections and output flags). Warnings
never block services or intentional parallel work; dynamic paths are unknown.

### Compiled program evidence

`EXCALIBUR_BUILD_EVIDENCE` defaults on and extends dependency freshness receipts.
Successful compiler calls capture the source manifest and executable versions.
Loaded ELF libraries inherit build provenance too. New local compiled outputs
in source projects without a successful build receipt remain unverified.
Later local ELF executions inherit the matching build's original versions;
changed sources or a failed subsequent compiler exit cannot certify a retained
executable. Shell lists that can mask compiler failure do not certify builds.
Yielded compilers retain their launch source receipt and require a successful
terminal session exit before later executions can inherit it. Existing supplied
executables without a recorded build keep ordinary receipts.
The parent `EXCALIBUR_EVIDENCE_DEPENDENCIES` switch still governs receipt capture.

### Repetitive output in active context

`EXCALIBUR_COLLAPSE_PROGRESS` defaults on. Model observation projection collapses
runs of eight or more lines differing only in counters or progress bars, retaining
two head and tail lines, every error/warning line and exact omitted counts.
Scanning is bounded to 2 MiB and 20,000 lines; excess output remains unchanged.
Durable history, tool logs and blobs retain full bytes. No tool call is removed.

### Durable finish verdict audit

`EXCALIBUR_FINISH_AUDIT` defaults on. Every finish verdict writes synchronously
through the per-run sink into `gate_attempts.jsonl`, including refusals,
confirmation requests, acceptance, wall reserve acceptance and safety caps.
Rows include sequence, originating attempt identity, mechanism, outcome and note.
Automatic decisions receive distinct identities. Finalization stops manufacturing
passing rows for engine runs; the deterministic stub retains its summary row.
This stream is observability only and does not change gates or refusal limits.

The finish audit stream also covers non-eval exec runs in their run directory;
evaluation runs retain their existing `excalibur/` record directory. This does
not enable the other evaluation exports for assistant or delegate profiles.

### Context observation identity (E1)

`EXCALIBUR_MASK_IDEMPOTENT` defaults on. Observation masking records the original
call identity, digest, line count and rendered marker. An omitted observation
never re-enters the literal token pool or hashes its own marker. Recent protected
observations retain their exact bytes. Full outputs remain retrievable through
`get_tool_output`; masking changes only the context excerpt. Older journal
markers are recognized by their complete call-specific envelope. Setting the
switch to `0` restores the earlier masking algorithm.

Masking also preserves original activity text for auditing and binds actual
observation call IDs to immutable output blobs, including initially inline
results. Only the marker is projected into model messages. Rebinding an omitted
observation cannot replace its original blob. This uses
`EXCALIBUR_MASK_IDEMPOTENT`; existing blob and session references remain available.

### Serialized prefix audit (E2)

The offline executor audit covers 65 turns with paired tool calls/results in
both append-only and batched-masking modes, with compaction disabled. It compares
complete serialized messages, including nested tool payloads. Every earlier
message is byte-identical except at a full-to-masked observation transition;
each such result may change once. The final volatile message is excluded.
The audit also drives five actual executor steps through a fake model, including
startup hooks. Startup guidance stays in append-only activity and precedes the
final volatile message, so subsequent requests retain its exact bytes. The
behavior uses the existing default-on `EXCALIBUR_STABLE_PREFIX` switch; masking
identity uses `EXCALIBUR_MASK_IDEMPOTENT`.

### Process-owned session evidence (E3)

`EXCALIBUR_TYPED_SESSION_EVIDENCE` defaults on. Execution and session-control
results carry typed process receipts into durable activity and verification
history. Running and successful evidence uses those receipts, refreshed from
current session authority and tombstones. A narrative mentioning a session does
not own that process. Compatibility decoding for older records is restricted to
execution headers and session-control arguments/list entries. Live finite builds
and tests still block autonomous finish; exercised healthy services retain their
liveness policy. Set the switch to `0` for the earlier text inference.

### Model delivery reserve (E4)

`EXCALIBUR_MODEL_DELIVERY_RESERVE` defaults on for autonomous exec/eval profiles.
Optional verification calls share a child deadline of half the remaining wall
horizon, capped at 180 seconds. Confirmation/finalization model calls use the
remaining horizon, capped at 90 seconds. An earlier parent deadline also applies.
Assumed budgets use the extended safety horizon from the explicit-time policy;
passing an assumed estimate does not authorize automatic completion. Calls
without a usable wall estimate or parent deadline keep their existing behavior.
Computation-tool deadlines are unchanged.

A review that reaches its child deadline remains unverified and pending, with no
further optional review retries. One bounded finalization opportunity remains;
subsequent completion preserves the pending outcome. Current live build/test
checks still apply. `model_delivery_reserve` journals the timeout outcome. The
caller recorder retains per-call `latency_secs`, `retry_wait_count` and
`retry_wait_seconds`, including unsuccessful calls; retry waits are only those
reported by the existing gateway metadata. Set the switch to `0` for the earlier
unreserved model waits. Interactive assistant profiles retain their deadlines.

### Review reuse audit (E5)

`EXCALIBUR_REVIEW_REUSE_AUDIT` defaults on. Every independent verdict-cache
lookup journals a `review_reuse` event with its key and effective hit/miss,
literal-request revision digest, context revision, dependency identities and
current versions, dependency-receipt count, literal-discovery fallback flag,
version validity and session-authority version digest. This is observational:
it does not change cache keys, verdicts or model calls. Records contain no
request text, tool output, file contents or credentials. Setting the switch to
`0` disables these audit events while preserving review reuse.

### Control issue priority (E6)

`EXCALIBUR_CONTROL_ISSUE_PRIORITY` defaults on. Command-lifecycle guidance shares
one issue identity across commands. Current finish, gate, refusal and budget
issues take priority over older tips in the existing bounded control layer.
Critical issues share reserved space so one long refusal cannot erase another
current state. User directives continue through the complete append-only
instruction path.

`control_layer_loss` journals the issue IDs clipped or evicted at both note
selection and final volatile rendering. These loss records contain identities
and actions only. The original `control_note` records remain available for
producer guidance. The byte cap is unchanged, including custom
`EXCALIBUR_CONTROL_TAIL_BYTES` values. Set the priority switch to `0` to restore
insertion-based selection and prefix clipping.

### Effective resource limits (envsnap and terminal, F1–F3)

`EXCALIBUR_EFFECTIVE_LIMITS` defaults to on. Set it to `0`, `off`, or
`false` to restore host-only resource reporting and disable the associated
parallelism guidance and OOM diagnostics. It applies to all profiles because
resource availability is an environment fact (Sections 5.2 and 13.5).

The environment snapshot reports effective memory and CPUs from Linux cgroup
v2 `memory.max`, `memory.high`, `cpu.max`, and cpusets, with v1 controller
fallbacks. It considers visible ancestors, subtree mounts and cgroup namespaces;
missing, malformed and unlimited controls preserve the host fallback. CPU quotas
remain fractional in the rendered snapshot; integer CPU consumers receive at
least one CPU and otherwise the rounded-down effective count. Host memory and
online CPU capacity appear as secondary facts only when they differ.

Validation uses fake proc/sys roots in `internal/envsnap/resources_test.go` and
requires no changes to the host's resource controls.

For a constrained memory budget, the snapshot adds one sentence immediately
following the resource facts: build/test with at most the rounded-down minimum
of effective CPUs and effective memory divided by 1.5 GiB per job, with a minimum
of one job, and lower parallelism after an OOM kill. The two-job sentence costs
76 bytes including its newline, within an 80-byte test budget; it shares the
snapshot's existing render cap. Unconstrained snapshots omit the sentence.

Terminal sessions sample the nearest readable memory-controller counters before
launch and freeze the after-sample at completion. An increased v2
`memory.events` `oom_kill` (or v1 `memory.oom_control` `oom_kill`) adds
"killed by the out-of-memory killer (memory limit X); reduce parallelism or
memory use" to the result header. The retry recommendation halves the computed
job budget, with a minimum of one; a serial retry must reduce memory use.
Descendant kills are visible even when the shell/build driver returns a
different status. The original process exit code is preserved. Foreground,
PTY, detached and background sessions use the same completion path; later
reads, waits and listings retain the diagnosis, independently of build evidence.

An exit 137 with unavailable kill counters says "SIGKILL; possibly out of
memory". Unchanged/reset counters never establish a kill. On v1 kernels without
an `oom_kill` field, an increased `memory.failcnt` only establishes allocation
pressure and retains the uncertain wording: allocation failure alone does not
prove that the kernel killed a process. Counters describe the command's shared
cgroup, so concurrent commands in that same cgroup can share an event; a readable
local counter takes precedence over unrelated ancestor events. Missing resource
files degrade gracefully. No diagnostic changes process scheduling or limits.

Additional tests in `internal/envsnap/oom_test.go` and
`internal/tools/terminal_resources_linux_test.go` use fake cgroup files and
controlled shell exits/signals. They cover counter changes, counter resets,
v1 fallback, uncertain SIGKILL, killed workers, frozen background results,
PTY paths and the disabled switch without allocating excessive memory.

A zero-valued memory control is a real budget, including `memory.high=0`;
it remains visible as zero and recommends a serial job. It is distinct from
missing/unlimited memory controls and from an unknown limit in OOM diagnostics.

## Local image inspection (Sections 7.5, 13.2–13.4)

`EXCALIBUR_INSPECT_IMAGE` defaults on. It adds the read-only
`inspect_image` tool to exec/eval and assistant worker surfaces. Setting
it to `0`, `false`, `off` or `no` removes the tool and disables its guidance.
No image input or model provider change is involved.

`path` resolves within the run work directory (current directory when
unspecified); both it and optional `compare_to` must remain inside that
resolved tree, including symbolic links and the existing task boundary.
PNG, JPEG, GIF (first frame), BMP, PPM (P3/P6) and PGM (P2/P5, including
16-bit samples) decode in Go. Headers over 64 million pixels are rejected
before raster allocation; files must be regular and at most 512 MiB.

The report gives exact RGB color shares, dominant background, fraction of
pixels different from that background, uniform/blank detection and color,
mean brightness on a 0–255 scale, and an area-averaged grayscale ASCII
thumbnail. Transparency composites over white. Defaults are 64 columns
and at most 24 rows; `width` accepts 8–96 and `height` accepts 4–32. The
thumbnail fits those bounds with terminal cell aspect correction.
`compare_to` requires matching dimensions and reports changed pixels and
percentage, a zero-based bounding rectangle, and a difference mask. A
pixel changes when any RGB channel differs by more than 3. Uniformity is
exact; no claim about semantic correctness follows from a nonblank image.
Reports remain under 12 KB even at maximum thumbnail settings.

Generated-image tests cover blank and colored screens, a rectangular
window, all supported formats, portable-map binary separators, changes
and unchanged comparisons, tolerance, geometry, deterministic output,
path containment, malformed inputs, header/file limits and cancellation.
Profile tests cover enabled/disabled availability for every worker profile.

`ocr: true` optionally runs an already installed `tesseract` against the
validated image path, with a three-second timeout and a 2,048-byte text
cap. Missing installations, errors, empty output and timeouts produce a
single-line hint alongside the pixel report. Nothing is installed and no
OCR runs without this opt-in. Fake-runner tests verify invocation, opt-in,
availability, deadlines, failure handling and bounded output.

The working method adds two sentences asking workers to capture visual
or interactive results and inspect them, correct blank/unchanged screens
instead of relying on process checks, and use before/after `compare_to`
for input-driven changes. The independent verifier requires this evidence
for visual claims. Successful direct inspection calls and their recorded
results are included in its evidence packet, with a 4,000-character result
limit to retain default thumbnails. Comparison metrics precede thumbnails
so they remain visible in shorter excerpts. This adds no deterministic
classifier or new completion policy.

With other default switches unchanged, the method grows from 2,998 to
3,294 bytes (+296), and the verifier instruction grows by 240 bytes. The
method's size tests now allow 3,300 bytes to retain all previous guidance.
Switch tests check both prompt deltas, visual and interactive evidence
requirements, direct-call evidence inclusion and default-thumbnail retention.
As with the other recent method additions, the method text follows
`EXCALIBUR_METHOD_V2`; disabling that block preserves its legacy text.

## Reading text-mode screens (Sections 13.2–13.4)

The existing default-on `EXCALIBUR_INSPECT_IMAGE` switch also gates built-in
screen reading. `inspect_image` automatically emits `screen_text` for detected
character grids; `text: true` requests an explicit unavailable hint when no
supported grid is found. Reading uses embedded UniVGA 8×16 glyphs under its
permissive X11 license and public-domain console-setup VGA 8×14 glyphs. Missing
8×14 mixed borders and vertical half blocks use UniVGA with its edge rows removed.
The source comment retains the license, source URLs and source checksums.
No external program or model is needed.

The reader supports 8- or 9-pixel cells, 14- or 16-pixel heights, and integer
1×/2× scaling, including 720×400, 640×400, 640×480, 640×350, 1280×800 and
1440×800 screens. Other whole-image grids within 20–160 columns and 5–60 rows
use the same detector. Cells must have few colors and match printable ASCII
or CP437 border/shading/block glyphs. The ninth column repeats the eighth
for CP437 codes 0xC0–0xDF and is blank otherwise. Both foreground polarities
are tried. Hamming confidence is normalized by foreground size; cells below
0.80 become `?`, and detection requires at least three distinctive glyphs,
80% recognized nonuniform cells, 98% low-color cells and mean confidence 0.85.
The reported mean covers nonuniform cells so blank margins cannot inflate it.
Uniform cells have no glyph evidence; a solid non-background cell is rendered
as a full block. Other fonts and graphical text may require optional OCR.

Text preserves leading/interior spacing and empty interior rows, trims trailing
spaces and empty edge rows, and is capped at 4,096 UTF-8 bytes including its
confidence label and truncation marker. Text precedes thumbnails so short
evidence excerpts retain it; the complete report remains capped at 12 KB.
Generated-image tests cover the geometries, color polarities, printable ASCII,
borders and blocks, noise, unknown cells, graphical rejection, cancellation,
automatic/explicit requests, the switch and UTF-8 output limits.
