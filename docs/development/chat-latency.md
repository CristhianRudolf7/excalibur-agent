# Chat latency

## Intake

Conversational daemon intake uses deterministic preanalysis before asking a
model. `EXCALIBUR_INTAKE_GATED_TRIAGE` defaults to `true`: only control
intents, task/run/ULID references, or the existing status vocabulary can
trigger the decider. Other messages reach the master turn with conversation
triage and `Decided=false`. Slash commands bypass classification as before.
Set the switch to `false` to restore classification for all non-slash messages.
Unset or invalid boolean values use the default.

The existing broad control lexicon is preserved, including `what is` and
`how is`; general questions containing these phrases can still be candidates.
The gate does not alter eval or Terminal-Bench execution paths.

`EXCALIBUR_INTAKE_SINGLE_QUESTION` defaults to `true`. The model answers
only the request-type question when there is at most one scope. A single
scope is filled deterministically; multiple scope options retain their
scope question. Durable-instruction and clarification questions are omitted;
`NeedsClarify` uses the existing deterministic `models.ShouldAsk` signals.
The triage fields remain compatible. Set this switch to `false` to restore
the four-question sequence.

`EXCALIBUR_INTAKE_DECIDER_TIMEOUT` defaults to `2500ms` and accepts Go
durations. Intake enforces the deadline even if a decider ignores context.
On timeout or cancellation, it keeps the existing deterministic triage
metadata, sets `Decided=false`, and delivers the message to the master.
A late result cannot execute a fast-lane control. Decider errors continue to
use the existing deterministic fallback. Invalid or negative durations use
the default; `0` explicitly disables the deadline for rollback. Deciders
should honor cancellation to release provider resources promptly.

`EXCALIBUR_INTAKE_FAST_LANE_GUARD` defaults to `true`: natural-language
fast-lane replies require a deterministic control intent or task/run reference,
in addition to the existing completed classification and confidence checks.
A world command classified as `quick_action` cannot become a run-status reply
from that label alone. The daemon preserves the `quick_action` type rather
than relabeling it as `status_query`. Set the guard to `false` to restore the
previous admission rule. Slash commands and mixed control/content splitting
retain their existing behavior; unresolved targets still reach the master.

`EXCALIBUR_INTAKE_TIMING` defaults to `true`. Each message routed through
the fast lane/master boundary emits one structured INFO event, `intake timing`:

- `gated`: classification was skipped by the candidate gate (slash commands
  bypass classification independently and report `false`).
- `decider_calls`: number of `Classify` attempts, including timeouts (zero or
  one). This counts intake invocations, not internal questions in rollback mode.
- `decider_ms`: elapsed classification wait in milliseconds, zero if skipped.
- `fast_lane`: the fast lane handled a control/reply, including mixed messages
  whose remaining content goes to a master turn.

The event includes no message text, identifiers, prompts, or credentials.
Set `EXCALIBUR_INTAKE_TIMING=false` to disable these events. Duplicate message
receipts are not routed a second time and do not emit another timing event.
