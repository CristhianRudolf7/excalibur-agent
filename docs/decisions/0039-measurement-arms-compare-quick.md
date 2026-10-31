# 0039 Measurement for agent quality: arms, compare, quick suite, resource peaks

Date: 2026-10-31. Workstream: w1-measurement. Scope: `internal/eval/**`
(except the Harbor invocation core), the `bench` command's report side
(`report`, new `compare`), `eval quick`, `tests/agentquality/`.

## Context

The agent-quality round strengthens the harness for small, cheap
models with the model fixed. Measurement owns the ablation
machinery: named arms, paired comparison with the §23.7 statistics, a
cheap local regression suite, and honest resource peaks. Mechanism
switches themselves are owned by the sibling workstreams; measurement
declares, forwards, records and hashes them.

## Decisions

### 1. Switches travel verbatim as container environment

`bench run --set key=value` appends `--ae key=value` to the Harbor
command (additive only: with no `--set` the command is byte-identical
to before). Alternatives considered: namespacing/rewriting keys, or
baking switches into the binary per arm. Verbatim forwarding keeps
one binary for all arms (Appendix F: same binary, declared config),
needs no key registry shared across branches, and stays auditable in
dry-run output. Convention (documented, not enforced): mechanism
switches use `EXCALIBUR_` names. Credential-looking keys
(`KEY/SECRET/TOKEN/PASSWORD`) print redacted.

### 2. Frozen profile plus arm hash, no container-side profile change

The frozen eval profile is NOT parameterized per arm: the container
records the same `profile.json`/hash in every arm, and the run
manifest binds it to the arm via `arm_profile_hash =
sha256(profile_hash + arm + sorted switches)`. The default arm
hashes exactly to the frozen hash, so pre-arm runs stay comparable
without migration. The alternative (container-side profile variants)
would have required `internal/cli` profile changes owned elsewhere
and would weaken the frozen-profile invariant.

### 3. `compare` pairs strictly and reports attribution first

Only tasks present in both runs enter the paired statistics;
one-sided tasks are listed, never silently averaged in. Cost/tokens
per success are computed over the paired tasks only. The switch diff
(arm names plus every changed key, `<absent>` for one-sided keys)
leads the report: an empty diff with a nonzero delta is labeled
noise. The accept rule reuses `eval.AcceptImprovement` with the
§23.7 3-point bar, overridable via `--bar`.

### 4. Quick suite runs the real binary as a subprocess

Each quick task runs `excalibur exec --profile eval` with the task
work directory as cwd, then a deterministic in-process Go verifier
checks the result. No Docker-in-Docker, no Harbor, no network. This
keeps the runner in the measurement workstream (no engine imports,
no import cycle with `internal/cli`) and makes quick trials faithful
rehearsals of container trials (same profile, same records layout:
`trial.json`, `trials.jsonl`, `manifest.json`, `report.json`).

### 5. Resource-peak fix split across eval and minimal cli touches

The zero peaks had three causes: the engine sampler never fed
metrics, the finalizer appended a zero row after the real final row,
and cumulative CPU counters were summed. The index now folds all
rows to peaks (max semantics; trailing zeros cannot hide a peak),
and two minimal `internal/cli` touches (a new max-semantics metrics
method plus sampler/finalizer feeds, ~20 lines) make short, stub and
failed runs record real peaks. `internal/cli` was touched because
the task explicitly assigns the sampler fix here and the hard
do-not-modify list does not include it; the change is additive and
covered by new tests in place. Container peaks: in the installed
route the harness process is the container workload, so its peak is
reported in both peak fields (kept separate for the external
route). Recommendation left for the owning workstream: populate
`ChildPeakRSS` from the cgroup v2 `memory.peak` counter; the index
already consumes it.

## Consequences

- `bench run` gains `--arm/--set`; `bench compare` and `eval quick`
  are new. All covered by `internal/eval` unit tests plus the
  public-API gate in `tests/agentquality/`.
- No new dependencies (stdlib only); `docs/decisions/dependencies.md`
  unchanged.
- No benchmark task contents, tests or solutions were consulted; the
  quick suite is written from scratch from general engineering
  knowledge.
