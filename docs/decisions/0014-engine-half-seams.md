# Decision 0014: engine-half seams (journal replay, verifying state, key reuse)

Date: 2026-10-31. Milestone H3 (engine half: `internal/journal`,
`internal/engine`, single-model `internal/models` gateway).

## Context

Section 7.3 requires the run to pass through `verifying` while the
deterministic gate evaluates, and Section 17.1 requires the recorded
model response to be reused after a crash instead of calling the
model again. Three gaps had no exact answer in the document:

1. The 7.2 transition table has no `running -> completed` edge: the
   gate must run in `verifying`.
2. A kill between `model_response` and the `tool_intent` entries
   leaves a response with no intents; replay needs the calls.
3. `tools.ExecuteOptions` carries an idempotency key but no call id,
   so the engine cannot correlate journal results with provider
   call ids through the executor hook.

## Decision

- The engine moves `running -> verifying` before every gate
   evaluation and `verifying -> running` on a retry, journaling both
   moves. Terminal moves leave `verifying`, never `running`.
- `ModelResponsePayload` stores the full requested calls (id, name,
   arguments). `journal.MissingIntents` synthesizes pending intents
   for calls that never reached `tool_intent`; the effect provably
   never ran (intent precedes every effect), so replay with a fresh
   key is safe. The engine enriches synthesized intents with effect
   metadata from the tool catalog before classifying.
- The engine records intent/result itself around tool execution
   (instead of using the executor journal hook), correlating by
   provider call id. Retries reuse the same key. The executor hook
   stays for direct executor users.
- Recovery reconstruction (`Recover` force-setting the last recorded
   state, `ApplyRecovery` moving to the decided state) is a rebuild,
   not a live 7.2 transition, and is journaled as such. From
   `recovering` the diagram only allows `queued/paused/blocked`, so
   the ask-user outcome restores `waiting(user)` by direct set with a
   recovery reason instead of a diagram edge.
- `models.Gateway.Chat` is the engine seam on the background lane;
   the interactive lane stays reserved at the gateway for master
   turns and control reads (full lane scheduling belongs to H9).

## Alternatives

- Reusing the executor `IntentHook` with a call-id side channel:
   rejected, it would couple journal correlation to executor
   internals.
- Routing ask-user recovery through `recovering -> queued ->
   running -> waiting`: rejected, it would burn a model call just to
   re-enter the wait.

## Consequences

- Crash tests assert exactly-once tool effects and exactly one model
   call for the post-replay step.
- The orchestrator (H9) owns `run_started`, agenda reconciliation
   and safe mode; the engine owns only the per-run half.
