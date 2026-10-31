# 0004 — Ambiguity readings in Section 11

Date: 2026-10-31. Status: accepted.

Section 11 leaves a few points open. Each reading below is the
closest feasible behavior; the alternative is noted.

## 1. Compaction threshold at the 500K boundary (11.4)

The Appendix B parameter is `min(500 000, 0.85*window - reserve)`;
the prose says windows at or above the limit compact "as is". For a
window of exactly 500K the formula yields ~421K, not 500K. The
formula governs (`CompactionThreshold` implements it exactly):
it is the normative parameter, and the prose example reads as the
shorthand for larger windows, where `min` resolves to the ceiling
anyway. A per-model ceiling can still lower the trigger
(`CompactionThresholdForModel`).

## 2. Masking protection set (11.3.5)

"Recent errors" is read as all error results: errors are small,
valuable, and re-fetchable markers would hide failure evidence from
a cheap model. Alternative (protect only errors inside the newest
10) was rejected because an old masked error would read as a
marker, not as the failure it was.

## 3. Margin direction (11.7)

Threshold comparisons use the margined estimate
(`WithMargin(total) > trigger`), i.e. the compiler compacts and
masks conservatively early rather than late. Alternative (margined
thresholds) would delay both mechanisms; early is safer for the
small-context goal of 11.2.

## 4. Tail selection overshoot (11.4.4)

`SelectTail` keeps whole events, so it may undershoot the budget by
less than one event; the pair re-attachment may overshoot by one
call. Splitting an event to hit the budget exactly would break the
never-split-pairs rule, so exactness yields to pair integrity.

## 5. Anthropic mark placement (11.6)

"Marks at the boundaries of L0, L1 and L2/L3" is read as marks
after L0, L1 and L2, plus the fourth sliding mark at the end of L4
(4 marks total, the documented maximum). Alternative (after L3)
would pin the frequently re-rendered task layer inside the prefix.

## 6. Conversation recalculation (11.4 master rules)

The summary recalculates past 24K literal tokens, or on resume past
the cache TTL when the literal history exceeds the 12K budget. The
second clause is an interpretation: below 12K there is nothing to
condense even though the cache is lost.

## 7. Open/closed lifecycle for the summary check (11.4.3)

`StructuredState.OpenIDs` treats `done`, `satisfied`, `retired`,
`canceled` and `not_applicable` as closed; everything else
(including `failed`) is open and must appear in the summary. The
spec does not enumerate the closed set; this reading keeps failed
criteria visible, which is the point of the check.
