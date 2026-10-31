# 0024 — Memory judgment calls: decider order, semantic floor, safeguards

Date: 2026-10-31. Area: memory (H11: Section 10).

## What

Deterministic choices inside the specified bands, all covered by
tests:

1. **Contradiction outranks refinement.** A candidate that extends
   an old statement while flipping its claim ("X, not Y") disputes
   rather than refines. The alternative (substring-first) merged
   genuine disputes into refinements.
2. **No fixed overlap cutoffs in the default decider.** Below-band
   similarities return `new` without judgment; inside the band the
   deterministic decider merges only on near-identity and flags
   only negation flips, leaving paraphrase judgment to the
   calibrated judge (H12). This mirrors the GBrain finding that
   fixed cutoffs miss reformulated duplicates.
3. **Semantic cosine floor 0.2.** Neighbors below it do not count
   as semantic evidence, in memories or chunks. Without a floor,
   KNN-style lists never come back empty and the zero-result
   rewrite plus the insufficiency notice could never fire.
4. **Disputed rows stay retrievable.** The default validity filter
   is active plus disputed; only withdrawn (retired) rows hide.
   The contradiction notice names the M-codes in the result set
   instead of silently picking a winner.
5. **Lossless compaction rule.** Note rewrites are refused when
   removed fragments outnumber preserved timeline lines — the
   concrete form of the 25% safeguard for an append-only timeline
   that keeps one line per removed note.
6. **External rows without live sources retire; user and agent
   rows without sources stay.** Explicit writes need no external
   backing; external claims do.

## Alternatives considered

Score-fused thresholds and Jaccard cutoffs were prototyped and
dropped: they either never fired (RRF floors) or over-merged
(overlap ≥ 0.6 duplicated distinct claims differing in one word).
