# 0051 — Full model context window (replaces the 500 000-token compaction ceiling)

Date: 2026-11-01. Owner: context-window workstream. Status: implemented.

## Context

Section 11.4 and Appendix B fix the compaction trigger at
`min(500 000, 0.85 x window - reserve)`. The 500 000-token value comes
from the original annotations, when the largest usable windows were
around that size. The current reference model (`gpt-6-luna`,
`reasoning_effort=low`) accepts about 1M tokens (models.dev:
context 1 050 000, max input 922 000, max output 128 000), so the old
ceiling compacts far earlier than the model requires and wastes the
window small cheap models need for real terminal work.

Read-only trial evidence (frozen full run, `analyze.py`) shows the
threshold helpers are currently test-only: no masking or compaction
event fires on the embedded `exec`/`eval` path, and every failure
ends as `completed` after shallow work. Using the full window does
not fix verification by itself, but it removes one premature
truncation source while masking and compaction actually run.

## Decision

Use the model's own limits instead of the fixed ceiling:

1. **Model limits table, overridable per model.** `internal/ctxcompiler`
   ships the pinned row (`gpt-6-luna`: 1 050 000 / 922 000 / 128 000,
   source models.dev) plus the conservative configured fallback
   (200 000 / 128 000 / 16 384) for unknown models. Operators
   override per lowercased model id through `[context.model_limits]`
   in the layered config or `EXCALIBUR_CONTEXT__MODEL_LIMITS__*` env,
   which wins over the shipped table.
2. **Trigger = max input - (reserve + safety).** The reserve covers
   the next tool result (`CompactionReserveTokens`, default 16 000);
   the safety margin is `EstimateMargin` (default 10%) of max input.
   The request therefore never exceeds the model's max input. The
   old fixed 500 000 ceiling becomes the optional cap
   (`CompactionCeilingTokens`, default 0 = model maximum) with the
   `EXCALIBUR_CONTEXT_CAP` environment alias; the smallest positive
   value among config, per-model and env wins.
3. **Masking and compaction run in the embedded path.**
   `engine.DefaultBuilder` applies observation masking (11.3) before
   the first compile and compaction (11.4, state-only, no model call
   to compact) after the first compile when the margined estimate
   trips the trigger. Only the activity history and the L3
   compaction block change; L0-L2 stay byte-identical, so the
   provider prefix cache breaks at most once per batch/compaction.
   History Seq numbers are monotonic across compactions.
4. **Profile and records report the effective window.** The eval
   profile carries `context_window` / `max_input` / `max_output`
   (frozen default = reference row) and the hash covers them, so the
   hash moves exactly when the effective window moves. Reference
   runs keep the frozen hash; unknown models report the conservative
   row. `profile.json` in every run records the same row.

## Why this alternative

- Keep 500 000: simplest, but discards ~400K usable input tokens on
  the reference model and keeps the test-only wiring that never
  fires in `exec`/`eval`.
- Scale the ceiling per model family without a table (e.g. always
  0.85 x window): still needs the window per model, so the table is
  unavoidable; the explicit max-input formula with reserve + margin
  is easier to audit than a bare fraction.

## Consequences

- `CompactionCeilingTokens` default changes 500 000 -> 0 (uncapped).
  `EXCALIBUR_CONTEXT_FULL_WINDOW=0` restores the legacy
  `min(500 000, 0.85*window-reserve)` trigger for ablations.
- New switches (all default ON, see `docs/development/agent-quality.md`):
  `EXCALIBUR_CONTEXT_FULL_WINDOW`, `EXCALIBUR_CONTEXT_MASK`,
  `EXCALIBUR_CONTEXT_COMPACT`, plus `EXCALIBUR_CONTEXT_CAP`.
- Tests: per-model thresholds, a long synthetic run that compacts
  under max input with a stable L0-L2 prefix, builder wiring with
  on/off switches, and profile hash/records coverage.
