---
name: awk-pattern-recipes
description: "Summarize and transform line-oriented logs with awk one-liners and field programs."
metadata:
  excalibur:
    group: data
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: awk pattern recipes
      name_es: Recetas awk
      desc_en: "Summarize and transform line-oriented logs with awk one-liners and field programs."
      desc_es: "Resumir y transformar logs con one-liners y programas awk."
---
# awk pattern recipes

Use this skill when `cut`/`grep` are not enough but a full script is overkill: per-line field math, conditional counting and grouped sums.

## When to use

- Summing a column, averaging timings, or counting by status code in logs.
- Reformatting delimited lines (reorder, recompute, filter fields).
- Quick per-file stats across many inputs.

## Procedure

1. Set the field separator: `awk -F, '{...}'` for CSV-ish, `awk '{...}'` for whitespace; fields are `$1..$NF`, whole line is `$0`, line number `NR`.
2. Count and sum: `awk -F'"' '/ERROR/ {c[$2]++} END {for (m in c) print c[m], m}' app.log` tallies by message.
3. Aggregate numerically: `awk '{s+=$3; n++} END {print s/n, n}' timings.txt`; group with associative arrays keyed by `$1`.
4. Reformat: `awk -F: -v OFS=, '{print $2, $1, $NF}' in.txt > out.txt`; guard headers with `NR==1{print; next}`.
5. Graduate to a file for anything over ~3 rules: `awk -f prog.awk` with `BEGIN`/`END` blocks, so the program is reviewable and rerunnable.

## Pitfalls

- Default whitespace splitting collapses runs and trims edges; set `-F` explicitly for structured data.
- Uninitialized variables are 0/empty, not errors: typos in names silently compute the wrong answer — print intermediates.
- Locale decimal commas break `$3+0` math; force `LC_ALL=C` for numeric logs.

## Verification

- Totals reconcile against `wc -l` and an independent `sort | uniq -c` count on the same key.
