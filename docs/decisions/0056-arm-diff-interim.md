# 0056 Arm diff and interim estimates (agent quality)

## Context

Section 23.7 fixes the paired comparison (bootstrap over tasks, 3-point
practical bar) but the arm loop needs two report-side answers the existing
`bench compare` does not give: which tasks flipped and why, and whether a
still-running arm is already out of reach. This workstream adds
`bench diff` and `bench interim` in `internal/eval` plus the `bench`
command's report side. Connectors (providers, gateway wire protocol, API,
contracts, integrations, MCP/ACP, Harbor invocation) are untouched.

## Decisions

1. Flips use the all-or-nothing convention already in `CompareReport`
   (`FlippedToKO`: A passes at least once and B never passes;
   `FlippedToOK`: the reverse), so `compare` and `diff` never disagree
   on what "flipped" means. Partial flips (both sides pass at least
   once) stay in the per-task rate table only.
2. Lazy flip triage is deterministic-only (`TriageRun` with
   `NoModel: true`, 5-minute budget): `bench diff` stays offline and
   credential-free. A checked-in `triage.jsonl` wins when present.
   Rationale: triage with a judge model would make a pure reporting
   command depend on credentials and non-determinism; the deterministic
   pass already classifies the dominant harness-friction and
   timeout/environment causes.
3. The STOP rule is `Diff.High < -bar` on the paired diff CI
   (current minus reference per completed task). "Below the reference
   by more than the practical bar" is read on the paired differences,
   not on overlapping intervals, so the verdict reuses the Section 23.7
   paired bootstrap exactly.
4. Partial runs load what is completed so far: `trials.jsonl` when
   present, merged with Harbor `result.json` trial directories finished
   since, falling back to `trial.json` files. Trial directories without
   a readable reward (still executing) are skipped, never scored as
   failures.

## Alternatives considered

- Model-backed lazy triage: rejected (see 2); the `--no-model`
  equivalent is the default here, with `EXCALIBUR_DIFF_TRIAGE=0` as the
  off switch.
- Interval-overlap STOP rule (current CI entirely below the reference
  CI): rejected in favour of the paired-diff rule, which accounts for
  per-task pairing and reuses one code path for run and baseline
  references.

## Consequences

- New switches: `EXCALIBUR_DIFF_TRIAGE` (`--no-triage`), and
  `EXCALIBUR_INTERIM_BAR` (`--bar` wins). Both default ON/at 0.03.
- New artifacts: `diff.json`/`diff.md`, `interim.json`/`interim.md`.
