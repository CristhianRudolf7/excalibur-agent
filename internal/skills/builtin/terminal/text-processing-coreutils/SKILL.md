---
name: text-processing-coreutils
description: "Slice and reshape text with cut, sort, uniq, tr, paste, comm and friends instead of editors."
metadata:
  excalibur:
    group: terminal
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Text processing with coreutils
      name_es: Procesar texto con coreutils
      desc_en: "Slice and reshape text with cut, sort, uniq, tr, paste, comm and friends instead of editors."
      desc_es: "Cortar y transformar texto con cut, sort, uniq, tr, paste y comm."
---
# Text processing with coreutils

Use this skill to reshape logs, lists and reports from the command line. The small tools compose: extract columns, normalize, sort, deduplicate, then compare or count.

## When to use

- Extracting columns or fields from logs, CSV-ish or tabular output.
- Counting, deduplicating or diffing line-oriented data.
- Normalizing case, whitespace or line endings before comparing.

## Procedure

1. Extract with `cut -d<delim> -f<fields>` (e.g. `cut -d: -f1 /etc/passwd`); for fixed positions use `cut -c1-20`.
2. Normalize before counting: `tr '[:upper:]' '[:lower:]'`, `tr -s ' '`, strip CR with `tr -d '\r'`, then `sort | uniq -c | sort -rn` for frequency tables.
3. Compare sorted sets with `comm -23 <(sort a.txt) <(sort b.txt)` (lines only in a); join columns with `paste -d,` or `join -t,`.
4. Number, head and tail slices: `nl -ba`, `head -n 20`, `tail -n +100` (from line 100), `sed -n '10,20p'` for ranges.
5. For anything needing backreferences or field math, graduate to `awk` (see `awk-pattern-recipes`) or `jq` for JSON (see `jq-recipes`).

## Pitfalls

- `uniq` only collapses adjacent duplicates: always `sort` first.
- Locale changes sort order; prefix `LC_ALL=C sort` for byte-stable, fast sorts in scripts.
- `sort` without `-n` orders `10` before `2`; use `-n` (numeric) or `-h` (human sizes).

## Verification

- Pipe to `wc -l` before and after to confirm row counts match expectations.
- Spot-check the first and last 3 lines of the result against the source.
