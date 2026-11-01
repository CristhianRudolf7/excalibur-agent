# 0060 — Call/result pairing invariant

Date: 2026-11-01. Scope: keep tool-call/result pairs intact across
masking, compaction and history handling so strict model wires never
see an orphan half (W15 call-pairing workstream).

## Problem

Long runs fail at the model wire with `http 400: No function call
found for function call output with call_id '...'` after roughly a
hundred tool calls: the request carries a `function_call_output`
whose `function_call` is no longer in the same request (and the
mirror image, a call with no result, is rejected by Chat
Completions). The architecture (Sections 11.3-11.4, 19.2) assumes
context control preserves pairs, and the 11.4.4 tail rule covers
only a single leading pair: a budget cut inside a multi-call batch
(one assistant event, several results) keeps the newest result while
dropping its call, and any upstream split (resumed history,
trimming) persists for the rest of the run. The provider wire
protocol itself is frozen, so the fix lives entirely on the harness
side.

## What changed

- `internal/ctxcompiler/pairing.go` (new): the pairing invariant on
  the shared internal message format (which both wires project
  from), `ValidateMessages`/`ValidateActivity`,
  `RepairMessages`/`RepairActivity` (orphan halves become bounded
  plain-text summaries preserving order and sequence numbers), and
  `IsOrphanPairError` (matches the Responses 400 and the Chat
  Completions mirror, nothing else).
- `internal/ctxcompiler/compaction.go`: `SelectTail` keeps pairs
  atomically — a cut starting at a tool result extends back to its
  call so the whole batch (one assistant event plus every result)
  rides along; a result whose call is gone entirely falls into the
  discarded span that the compaction summary still covers. Masking
  needs no change: it replaces outputs in place and keeps every call
  identity, so it is atomic by construction (covered by test).
- `internal/engine/builder.go`: `DefaultBuilder.BuildRequest`
  validates every built request and repairs orphans to text before
  the wire; the run history itself is untouched there.
- `internal/engine/pairing.go` + `engine.go` (new file, additive
  fields): when a strict wire still rejects a request, the engine
  repairs the run history the same way, rebuilds the request and
  retries the model call exactly once. A second rejection fails the
  run as before. Run counters `PairRepairs`/`PairRetries` track both
  paths.
- `internal/eval/trialrec.go`: `RecordPairRepair` appends to the new
  `pairing.jsonl` trial record; `internal/cli/exec_engine.go` wires
  the builder and retry hooks to it. No connector, gateway, MCP/ACP
  or Harbor code changed.

## Switches (all default ON)

`EXCALIBUR_CONTEXT_PAIR_ATOMIC` (pair-atomic compaction tails),
`EXCALIBUR_CONTEXT_PAIR_CHECK` (pre-request validate+repair),
`EXCALIBUR_CONTEXT_PAIR_RETRY` (orphan-400 history repair with one
retry). Builder/engine `*bool` overrides allow per-run ablations.

## Alternatives considered

- Sending untruncated full history so pairs never split: rejected,
  it defeats the context budgets (11.2) the harness exists to hold.
- Retrying the identical request on the 400: rejected, the same
  bytes fail deterministically; only a repaired history can succeed.
- Fixing the provider adapters to tolerate orphans: rejected, the
  connectors are frozen and the server owns the rejection.

## Discrepancies and cross-area notes

- The architecture text describes the 11.4.4 tail as never splitting
  pairs; the shipped rule only re-attached a single leading pair.
  The atomic extension implements the documented rule for batches,
  with the legacy single-pair behavior kept behind the atomic
  switch for ablations.
- Section 19.2 lists the wire pair requirements; this change adds
  no new wire behavior, only harness-side enforcement plus one
  bounded retry, recorded here as an additive strengthening.


## Recovery and boundary refinements

The atomic cut inspects every retained result rather than only the first
event, since pinned notes can separate a call and result. Synthetic long
histories are also checked on serialized Responses and Chat Completions
requests through simulated servers.

A server may reject snapshot-owned exchanges or ordering that the local
presence check considers complete. If history repair changes nothing, the
one retry renders the rebuilt request's tool exchanges as bounded plain
text. This is a conservative recovery under the director's requested
single retry, with both halves removed together. Requests with no tool
exchange cannot benefit and are not retried. The matcher requires HTTP 400
and a pairing diagnostic, excluding unrelated schema and transient errors.

## Integration with later run controls

The integrated step loop checks deadlines before recovery and again after a
failed recovery, preserving budget pauses rather than converting them to model
failures. Recovery errors propagate to the existing error handler, so quota
exhaustion during the retry enters capability waiting. The retry retains the
`engine.step` audit call site. Regression tests cover deadlines, quota waiting
and audit identity alongside the existing interrupt tests. These changes retain
the run-state behavior of Sections 7.2-7.3 and the existing provider error path;
they introduce no additional retry or connector change.
