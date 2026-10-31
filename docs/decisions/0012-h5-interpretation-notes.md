# 0012 — H5 interpretation notes: checklist language, R3 scope, micro-plan ids

Date: 2026-10-31. Status: accepted.

## What

Three small interpretation decisions, none changing specified
behaviour:

1. **Checklist lines render in English** (e.g. "passed",
   "not run on the current version"). The architecture example is
   Spanish, but AGENTS.md makes the repository English-only, which
   overrides the bilingual-docs rule of Section 3.4.
2. **R3 needs a cycle of distinct calls.** An all-identical
   repetition is R1's domain, which stays sensitive to the result:
   identical calls with changing results (e.g. a polling tool) are
   not a loop. `wait_for` is additionally exempt by default, since
   Section 18.5 requires legitimately repetitive polling to run
   untouched. The exemption set is configurable per detector.
3. **Micro-plan incarnations get distinct ids**
   (`<task>-micro`, then `<task>-micro-2`, ...), so archived
   micros stay queryable instead of collapsing into one row.

## Why

Each follows the closest feasible reading of the document: the
English-only rule is explicit in AGENTS.md; the R3/R1 split keeps
the documented false-positive requirement ("legitimate wait_for
trajectories are not stopped"); per-incarnation ids match the
plan-versioning principle of keeping previous versions queryable.
