# 0055 Explicit finish protocol (agent quality)

Date: 2026-11-01. Branch: `explicit-finish`. Status: implemented.

## What

Non-interactive runs (`exec`, and therefore the `default`, `delegate`
and `eval` CLI profiles, which are all non-interactive under `exec`)
end ONLY through an explicit `finish` tool call with `summary` (what
was done) and `evidence` (commands run and their results proving each
requirement):

- An assistant turn without tool calls never ends the run. The engine
  replies with a short continuation note and counts consecutive
  text-only turns; after 3 in a row it enters closing mode (tools stay
  available so `finish` remains callable).
- `finish` is refused (with the reason) when the most recent execution
  of any check the agent itself ran failed after the last change, when
  required artifacts are missing, or when the agent's own last command
  errored. Refusals are not bounded by a fixed small count; only the
  time/step budget bounds them.
- Confirmation round: the first accepted `finish` triggers one short
  re-read-and-confirm turn; the second `finish` ends the run.
- `blocked` may only be declared via `finish` with `status: blocked`
  and a concrete reason; a blocked claim about missing data/files is
  checked deterministically against the filesystem paths mentioned and
  refused if any exists.
- Records keep the model's raw final text and the `finish` arguments
  (`explicit_finish.json` next to `skills.json`/`explore.json`, plus
  an `explicit_finish` event); the delivery summary is
  `finish.summary` and never echoes the instruction.
- Switch `EXCALIBUR_EXPLICIT_FINISH=on|off` (default on); off restores
  the old text-finish behaviour for ablation.

## Why

Measured root cause (director's analysis of read-only evaluation
trial records): runs end when the model emits an assistant turn
without tool calls (e.g. the agent ran its own check, got `exit: 1`
with an ImportError, and the very next turn ended as `completed`).
The final model text is not even recorded: the trajectory shows a
harness-written `completed: <instruction text>`. The gate and
verification hooks only run on the text-finish path, so a premature
text answer slips through. Forcing an explicit, evidence-carrying
`finish` call with refusal checks closes that hole at the step-loop
level.

## Alternatives considered

- Keep text-finish and strengthen the gate note: rejected, because the
  observed failure is the absence of any tool call at all, not a weak
  gate message; the model must be forced to act (or call `finish`
  with evidence), not just nudged.
- Put `finish` in the frozen eval profile table: rejected, because it
  would move the frozen Section 23.2 hash and break comparability.
  `finish` is a harness control tool, not a benchmark tool: the
  definition rides the model prefix when the switch is on, the
  profileGatedRunner always allows it, and the frozen table (and its
  hash) never moves.

## Departures from the architecture document

The document's Section 7.3 closing lets a text-only answer run the
gate and complete. Under this protocol, text-only answers in
non-interactive profiles never complete; only `finish` does (with the
same gate, self-verification, artifact and strong-verifier checks
reused as refusal checks, unbounded by `GateAttempts`). The
Running -> Verifying -> terminal transition shape is unchanged.
Interactive daemon runs keep the old behaviour.

## Compatibility

- Connectors untouched: no change to `internal/providers/**`, the
  model gateway wire protocol (`internal/models`, `internal/gateway`),
  `internal/api`, `packages/contracts`, `integrations/**` (Harbor
  adapter, client templates), the MCP and ACP servers, or the `bench`
  Harbor invocation.
- Frozen hashes untouched: `DefaultEvalProfile` and `EffectiveTools`
  are unchanged; `finish` is injected into the provider request and
  allowed by the gate without entering the table.
- Budgets, gate, providers and tool surfaces unchanged: the mechanism
  only changes step control flow and volatile notes.
