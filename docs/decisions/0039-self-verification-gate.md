# 0039 Self-verification closing hooks (W1)

Date: 2026-10-31. Scope: `internal/tasks/**` (gate, criteria,
evidence) and the finish path of `internal/engine` (completion and
closing hooks only). Status: implemented.

## What

The agent declared success on wrong results: measured ~29% success on
evaluation trials, and ALL failures end as `completed` (the agent
declares success while the result is wrong), with very shallow work
(avg 13 tool calls on failures, 8.6 on passes). Root cause: the
completion gate (`internal/tasks/gate.go`, Section 7.5) is strict but
only over recorded criteria. On paths where no criteria are ever
recorded (non-interactive and embedded runs), the gate passes
vacuously on the first final answer, so nothing ever forces the
build-verify loop.

Three engine-independent mechanisms in
`internal/tasks/selfverify.go`, merged into the gate outcome in
`ApplySelfVerify` and enforced from the engine closing hook
(`internal/engine/verify_hooks.go`, called from `finishTurn`):

1. Requirement extraction: a cheap deterministic pass over the
   literal instruction pulls explicit, checkable items (exact paths,
   file names, formats, outputs, commands, constraints) into draft
   criteria with a suggested executable check each, plus one
   verifier-role model pass that refines them. Deterministic drafts
   always stand alone; refine only improves wording and never gates.
2. Mandatory verification phase before `completed`: the hook confirms
   every draft against fresh executable evidence from the tool
   history (a passing check after the last change; the last result
   is neither failed nor a still-running session) and re-reads the
   instruction against the produced artifacts (contrast coverage).
   Anything unverified returns the agent to work with one concise
   injected note; returns stay bounded by `GateAttempts` (2), ending
   in the honest state (`completed_with_pending`/`blocked`), never
   silent success.
3. Independent verifier input for big tasks: a fresh small context
   (request, drafts, short evidence lines, change diff) that never
   carries executor reasoning, for the Section 8.7 verifier profile.
   Spawning stays with the orchestrator; this change provides the
   input builder, the size inference, and the enforcement point.

The verifier never looks for the benchmark's own tests: it only
re-checks the agent's own checks and the executable suggestions
above.

## Why this shape

- The gate stays deterministic and the verification phase executes
  no tools itself: it checks the agent already ran fresh checks. A
  harness-side re-execution seam would need tool access inside the
  gate and a second effect model; the history check is cheaper and
  covers the observed failure (shallow work, last check failing or
  still running, e.g. a build session `alive: true` at finish time).
- The literal instruction reaches the hooks without driver changes:
  the run stashes it from the compiled task layer (`request:\n...`),
  which the context compiler already renders verbatim.
- Empty request with no drafts and no criteria keeps the legacy
  behavior exactly, so every pre-existing unit test passes
  unchanged; two CLI embedded scripts were updated to the new
  contract (verify after writing instead of finishing immediately).

## Alternatives considered

- Prompt-only guidance ("verify before finishing"). Rejected as the
  sole mechanism: small models agree and still stop early, and
  nothing in the records distinguishes verified from unverified
  completions. Prompts remain complementary (owned by the engine-loop
  task).
- Harness re-runs every check before completing. Rejected for this
  round: effect and idempotency handling inside the gate, plus
  double cost on every finish. Revisit if the history check proves
  gameable.
- Requiring recorded criteria rows on the embedded path. Rejected:
  it would force a planning store into the one-shot path and break
  its minimal-records shape; drafts plus history evidence give the
  same enforcement without new state.

## Ablation

All switches in `tasks.SelfVerifyConfig`, environment-overridable,
defaults ON; listed in `docs/development/agent-quality.md`
(self-verification section). Key names: `EXCALIBUR_SELFVERIFY`
(master), `_EXTRACTION`, `_VERIFICATION`, `_CONTRAST`,
`_INDEPENDENT`, `_REFINE`, `_MIN_CHECKS`, `_MAX_RETURNS`,
`_MAX_DRAFTS`, `_BIG_TASK_STEPS`.

## Coordination

- Engine loop prompts: owned by another task; the retry note format
  changed to the capped `self-verify before finishing: ...` line as
  part of the closing hook (no test depended on the old prefix).
- `internal/cli` embedded scripts: two scripts updated to the new
  contract only (verify after write; recover with an in-surface
  check). No production CLI code changed.
- Orchestrator: to enforce the 8.7 subagent verifier on big daemon
  tasks, spawn it from `BuildVerifierContext` and record verdicts
  with `RecordVerdicts`; the gate already honors `VerifierRequired`.
