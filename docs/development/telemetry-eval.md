# Telemetry and evaluation (H4, measurement half)

Contract and backend tests of Sections 22 and 23 as built on this
branch: per-task metrics from v1, OpenTelemetry traces, incremental
ATIF export, plain-language status, system health, eval statistics,
report aggregation, replay mode, eval smoke scaffolding, the Section
23.5 suite registry, and the Terminal-Bench benchmark package.

## Telemetry contract (`internal/telemetry`)

- **Events (22.1)**: `Bus.Publish` emits one typed `types.Event` per
  action (lifecycle, model calls with usage/cost, tool calls, questions,
  plan approvals, directives, memory writes, compactions, loop
  detections, errors, file edits with diff). `Replay(cursor)` resumes
  from the last delivered sequence with no gaps or duplicates.
  `Sink` appends every event and metrics snapshot to JSONL under the
  trial log dir with fsync per write, so cut trials keep partial data.
- **Metrics (22.2)**: `TaskMetrics` accumulates RAM (daemon delta vs
  total kept separate from child peaks), tokens (input incl. cached,
  cache read/write, output incl. reasoning), cost via the versioned
  `PriceTable`, calls by role and by tool, time split (model/tool/
  waiting/wall), quality (final state, criteria, interventions,
  corrections) and efficiency (hit rate, compactions, masks, loops,
  retries). `SetModel` records provider/model for the single-model
  check. `Sampler` polls a `platform.ResourceSampler` every second in
  a `safego.Go` loop and feeds daemon RSS/CPU only, so harness RAM
  and container RAM never mix; samples are retained for the Appendix F
  minimum records. `Validate` rejects cached-exceeds-input and negative
  counters. `Snapshot` exports to `metrics.json` via `WriteMetricsFile`
  and on to Harbor `AgentContext`.
- **Traces (22.1)**: spans `invoke_agent`, `chat` and `execute_tool`
  with the pinned GenAI conventions version
  (`GenAIConventionsVersion`); `ValidateSpan` checks names, ids and
  required `gen_ai.*` attributes. The OTel SDK stays optional so unit
  tests never need a collector.
- **ATIF (23.2)**: `Writer` emits `trajectory.json` in selectable
  `v1.7`/`v1.8`; every `Append` rewrites the file atomically
  (temp + fsync + rename) so partial trials stay valid.
  `Validate`/`ValidateFile` check version, metadata (v1.8 additionally
  requires provider and profile hash), ordered steps and well-formed
  tool calls.
- **Status (22.4)**: `RenderStatus` turns an `api.RunStatusResult`
  snapshot into sentences; `RenderMetricsLine` answers `metrics.query`
  in one line. Deterministic: no model call.
- **Health (22.5)**: pure threshold checks `CheckStuck/WAL/RAM/Disk/
  Provider/CacheDrift` with `DefaultThresholds` from Appendix B;
  alerts go to the master, which decides whether to inform the user.
- **Provenance (22.3)**: `Provenance.Chain` walks
  artifact -> tool call -> run step -> request -> instruction revision
  -> evidence, answering "why did you do X?".

## Eval contract (`internal/eval`)

- **Statistics (23.7)**: `BootstrapCI` (seeded, deterministic), paired
  bootstrap over tasks `PairedDiffCI`, exact `PassAtK` (unbiased
  `1-C(n-c,k)/C(n,k)`) and `PassPowK` (`(c/n)^k`), `PowerTrials` normal
  approximation with the `MinTrials = 5` floor, and the
  `AcceptImprovement` rule (interval excludes 0 and gain clears the
  practical bar, 3 points on Terminal-Bench).
- **Report**: `TrialOutcome` (Appendix F minimum per trial; Harbor
  rule `reward > 0`, errored trials count 0; failure categories
  verifier/timeout/harness/crash/oom/budget/blocked),
  `TrialFromSnapshot` (canonical snapshot-to-trial mapping, peaks kept
  separate), `LoadTrials` over trial directories, `Aggregate` (success
  CI95, pass@k, pass^k, tokens/cost per success, failure histogram,
  per-task rates), `WriteReport`, `HashTrials`.
- **Replay (23.6)**: `Recorder` appends `{request_hash, request,
  response}` JSONL; `Player.Replay` serves recorded responses while the
  compiled request hash matches and returns `*Divergence` at the first
  mismatch. Post-divergence live calls need an approved budget.
- **Smoke**: `SmokeRunner` runs repository-owned local tasks (no
  Harbor/Docker), writing incremental ATIF plus `metrics.json` per
  task and validating both before any paid trial.
- **Suites (23.5)**: `Registry` lists all 17 suites with fixtures;
  `ByID` resolves; `FixtureHash` pins deterministic setups.
- **Profile (23.2)**: frozen `EvalProfileConfig` (`eval@1`) with the
  Section 23.2 capability table, `Hash`, `CheckDataDir` (non-empty dir
  aborts without `--profile-override`), `BlockedBenchmarkSource`
  (benchmark repos/Hub/tbench.ai blocked and logged),
  `WebToolsVisible` (hidden under restricted network),
  `CheckSingleModel`, and `WriteProfile` for the minimum records.
- **Harbor usage**: `FromMetricsFile` builds `AgentContext` from
  `metrics.json` (loud failure when absent); `MetricsPath` is the
  canonical `/logs/agent/excalibur/metrics.json` location.

## Benchmark package (23.2)

| Piece | Location | Notes |
|---|---|---|
| Static build | `scripts/bench/build.sh` | cgo + musl static amd64/arm64, sha256 published |
| Installed adapter | `integrations/harbor/excalibur_agent.py` | install/configure/run; `AgentContext` from metrics; tests in `excalibur_agent_test.py` (stdlib unittest) |
| Launchers | `scripts/bench/run.sh <tb21\|tb40> <provider/model> [k]` | Pinned datasets, `-k 5` default, no timeout/resource changes, no upload by default (Appendix F) |
| ACP route | `integrations/harbor/ACP_NOTES.md` | `excalibur acp --profile eval` via Harbor's generic acp agent; less rich than native ATIF |

## Backend tests

- `internal/telemetry`: cursor resume without gaps (incl. diff
  payload), token/cost/call accumulation with exact cost math,
  1-second daemon sampler via the platform fake (child peak untouched),
  model/provider recording with `metrics.json` roundtrip,
  ATIF incremental validity after every step plus version/metadata
  rejection, span validation, provenance chain, status rendering,
  all six watchdogs, sink JSONL presence.
- `internal/eval`: exact pass@k/pass^k values, bootstrap
  determinism, paired win/tie decisions, power floor, replay
  zero-provider-call rerun and first-divergence stop, 17-suite
  registry with fixture hashes, profile tool table vs Section 23.2,
  data-dir abort, network gating, blocked sources, single-model
  check, report aggregation math, `TrialFromSnapshot` peak mapping,
  smoke ATIF+metrics validity with smoke-metrics feeding
  `AgentContext`, AgentContext mapping incl. reasoning in output.
- `cmd/excalibur`: `eval smoke` writes local trials and exits 0 only
  when all pass; `eval report` aggregates a trials dir and writes
  `report.json`.
- `integrations/harbor`: 7 unittest cases (install+sha256, configure
  env, run with fake binary filling AgentContext, missing-metrics
  loudness, paths).
- No test calls a paid API; no live benchmark is run (`EXCALIBUR_LIVE`
  unset). Engine-dependent rows of Section 23.8 (full episode runs,
  timeout/OOM-cut trials, rerun isolation with the real engine) are
  deferred to the engine owner.

## Deferred

- SQLite-backed `telemetry.db` (uses JSONL sinks behind the same
  call sites until the H2 store lands; the porter's group-commit and
  WAL rules are untouched).
- Real Harbor `run.sh` trial execution and any paid benchmark
  (needs user approval and budget per Section 23.1).
- `excalibur eval smoke|report`: wired in `cmd/excalibur/eval.go`
  (smoke exits non-zero when any local task fails; report writes
  `report.json` and prints success CI95, pass@k, pass^k, cost/success).
  `excalibur exec --profile eval` enforcement stays with the
  non-interactive-half agent; this branch provides the validating
  primitives.
