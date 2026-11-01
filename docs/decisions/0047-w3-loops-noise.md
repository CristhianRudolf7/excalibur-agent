# 0047 — W3 loop breaker and tool noise: recovery prompt, repeat block, prefix pruning

Status: accepted.

## Context

Automatic triage of 44 failed evaluation trials of the current
agent-quality build (tool errors near zero; failures now dominated by
discipline and verification problems) showed:

- 4 `loop/no-progress` failures: the same command repeated without
  progress (process polling, repeated rebuild-and-run cycles). The
  exact-match R1/R2 rules never fired because outputs varied between
  repeats, and the generic stage-1 nudges did not change the approach.
- `ack_directive` called 36 times with no directive pending
  (single-run scope never has any): pure prefix and step waste.
- `call_tool` called 11 times without a name (the model nests the name
  inside `arguments` or wraps the call as `{"toolname": {...}}`).
- A load-bearing bug found while implementing: nothing in exec/eval
  single-run scope ever installs a progress sampler
  (`SetProgressFunc` has no callers), so the R4 no-progress rule
  hashed the zero state and fired spuriously every 8 tool steps on
  every long run (note, escalate, block at 24 steps).

## Decision

Loop breaker (`internal/tasks`, `internal/engine`; all default ON):

- New `near_repeat` detector rule: 3 consecutive same-tool calls with
  near-identical arguments (whitespace-collapsed, normalized edit
  similarity >= 0.8) fire even when R1/R2 stay silent. Staged
  note/escalate/blocked like every other rule. A different tool or a
  genuinely different command restarts the run.
- New `no_info` detector rule fed by per-step novelty tracking in the
  engine: a step is informative only when a call succeeds with a
  non-empty output differing from that tool's previous success; 6
  dull steps (`LoopNoInfoSteps`) fire, then stage every 6.
- Structured stuck-recovery prompt at stage 1 of both rules
  (`EXCALIBUR_Q_RECOVERY`): keeps the `loop:` prefix, lists the last
  tried calls, and forces the shape "summarise in 2 lines, list 3
  DIFFERENT strategies, pick one and execute its first step now".
  Off falls back to the plain nudge.
- Exact-repeat block (`EXCALIBUR_Q_REPEATBLOCK`): a single-call batch
  repeating the previous single call byte-for-byte with a
  deterministic outcome is rejected once per run and signature with
  guidance; the call never executes and never pollutes the R1/R2
  windows. Retryable outcomes (transient, timeout, canceled) are
  never blocked; multi-call batches pass through. The block shifts
  the classic escalation trajectory by one step (recorded in the
  updated H5 lifecycle test), which is the point: one repeat becomes
  guidance instead of a wasted execution.
- The R4 progress rule is skipped while no progress signal exists
  (zero state, no sampler) when the recovery switch is on; the
  novelty tracker replaces it in single-run scope. Off restores the
  legacy observation.

Tool noise (`internal/tools`, `internal/cli`; all default ON):

- `ack_directive` is pruned from the model prefix while no directive
  is pending (`EXCALIBUR_TOOL_HIDE_ACK`); a pending root directive
  restores it the next step. Execution stays wired.
- `call_tool` accepts the tool name nested inside arguments
  (`name/tool/tool_name`, case-insensitive) or as the single leading
  key wrapping the arguments (`EXCALIBUR_TOOL_CALL_NAME`), and both
  the core stub description and the deferred definition carry a
  one-line example (`{"name":"lsp","arguments":{"path":"x"}}`).
- `clock` is pruned from the eval prefix
  (`EXCALIBUR_TOOL_HIDE_CLOCK`): zero productive calls across the
  frozen and current trial sets, while wall time and the remaining
  budget already ride every model call in L5. It stays executable and
  stays in wider profiles.

## Why

- LangChain harness engineering (Feb 2026): the `LoopDetectionMiddleware`
  (per-file edit counts plus "consider reconsidering your approach")
  and the forced build-verify loop are the direct priors; our
  recovery prompt is the same intervention moved to stage 1 with a
  forced output shape, and the repeat block is the deterministic
  half (code decides) with the model kept for the strategy choice.
- Meta-Harness (arXiv 2603.28052): harness code (what to store,
  retrieve, present) moves results as much as weights, with larger
  gains on weaker models; all five mechanisms are code-side changes
  with no prompt growth except the injected recovery shape.
- Open-source practice (general behavior only): OpenHands stops on
  repeated identical observations; mini-sWE-agent shows bash-only
  tools suffice (supports pruning idle tools rather than adding
  surface); Codex/OpenCode use a closing `tool_choice: none` pass
  (our block is the same instinct mid-loop: stop executing, force
  reconsideration).

## Alternatives considered

- Feeding blocked repeats into the detector as errors: rejected, it
  would corrupt the R1/R2 windows with synthetic outcomes the tools
  never produced.
- Removing `status` instead of `clock`: rejected, `status` lists
  live terminal sessions, which incomplete-stop failures show the
  model actually needs; `clock` has no such record.
- Removing noise tools from the frozen eval table: rejected, the
  Section 23.2 table is frozen; dynamic per-step pruning keeps the
  profile hash comparable while removing the per-call cost.

## Consequences

- Five switches, all default ON, listed in
  `docs/development/agent-quality.md` (W3 section).
- No changes to providers, gateway wire protocol, api, contracts,
  integrations, MCP/ACP servers, or the bench Harbor invocation.
- The `TestLoopEscalationNoteExpertBlocked` H5 row now runs 8
  identical calls (one consumed by the block) to reach the blocked
  stage; the staged note/escalate/blocked coverage is unchanged.
- `TestLegitimateWaitForNotStopped` now pins the new contract:
  same-command polls with varying results fire `near_repeat` (note
  first, never a stop on short polls); the `wait_for` exemption is
  untouched.
