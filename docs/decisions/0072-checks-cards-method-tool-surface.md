# 0072: Self-checks, error cards, eval tool surface and working method

This record covers four changes to the harness reliability work: the
finish-time evidence rule, error-triggered skill cards, the working method
text, and the eval tool surface.

## Eval tool surface and profile hash

The eval core table no longer lists `ack_directive`,
`leave_continuation_note`, `subagent` and `task_db`. In a single autonomous
run each of them can only answer with nothing to do, or record state that no
gate, record or finish path reads back:

- `ack_directive` answers "no pending directives" because directives come
  from a parent orchestrator that a single run never has.
- `leave_continuation_note` writes a run-scoped store that nothing reads, since
  a single run has no restart.
- `subagent` spawn records a child that no runner executes in single-run
  scope.
- `task_db` is read only by its own query operation.

A tool surface that cannot act spends model turns and prompt tokens without
changing the outcome. The eval profile hash moves because `EnabledTools` is part
of the frozen table. Profiles recorded before this change are not comparable to
trials recorded after it on the tool surface. The assistant (default) and
delegate profiles keep the earlier core through `cli.EffectiveTools`, so their
tool hashes do not change. No switch is added: the eval table is frozen code,
and the trim is a deletion, not a new behaviour.

## Switches

- `EXCALIBUR_CARDS_STRICT` (default on): an error card fires only for a
  non-probe command, once per card name per run, and not when the same output
  shows a successful install of the binary the error names.
- `EXCALIBUR_VERDICT_RETRY` (default on): an empty or truncated verifier verdict
  is retried once with the output budget doubled (cap 32768). Off keeps the
  original budget. A verdict still missing after the retry is journaled as
  `verifier_no_verdict` and returns `tasks.ErrVerifierNoVerdict`; callers treat
  it as advisory.
- `EXCALIBUR_EXERCISE_STRICT` (default on): help and version output,
  syntax-only checks, import-only interpreter calls and observation commands do
  not count as exercising a deliverable. Off restores the marker rule.
- `EXCALIBUR_METHOD_V2` (default on): the working method gains evidence rules
  (steps 9 to 17). Off reproduces the original block byte for byte.

## Discrepancy and alternative

Narrowing `search_skills` and `load_skill` when `EXCALIBUR_SKILLS=off` was in
the backlog but is not implemented: it needs a mode-aware surface in
`cli/exec.go` and the design for this round keeps skill changes out of scope.
The finish gate's use of `ErrVerifierNoVerdict` (advisory instead of refusal)
belongs to the finish work package.
