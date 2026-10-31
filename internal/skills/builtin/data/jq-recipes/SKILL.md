---
name: jq-recipes
description: "Slice, filter and reshape JSON with jq: selection, mapping, grouping and safe in-place edits."
metadata:
  excalibur:
    group: data
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: jq recipes
      name_es: Recetas jq
      desc_en: "Slice, filter and reshape JSON with jq: selection, mapping, grouping and safe in-place edits."
      desc_es: "Filtrar y remodelar JSON con jq de forma segura."
---
# jq recipes

Use this skill for any JSON wrangling in pipelines. Build the filter incrementally on a sample, then stream it over the full input.

## When to use

- Extracting fields from API responses, logs or model-call records.
- Filtering arrays of objects by predicates.
- Reshaping JSON into CSV/TSV or summary tables.

## Procedure

1. Pretty-print and explore: `jq . sample.json | head -50`; list keys with `jq 'keys'` or `jq '.[0] | keys'`.
2. Select and project: `jq '.items[] | select(.status == "failed") | {id, msg: .error}'`; compare numbers with `tonumber` when the source quotes them.
3. Reshape output: `jq -r '.items[] | [.id, .status] | @csv'` for CSV, `@tsv` for TSV; `jq -s 'group_by(.k) | map({k: .[0].k, n: length})'` for counts.
4. Edit safely: `jq '.retries = 3' in.json > out.json` (never in place); validate with `jq -e` (exit non-zero on null/false results when asserting).
5. Stream big files: `jq -c '.events[]' big.jsonl` per record, or `--stream` for truly huge single documents.

## Pitfalls

- `jq .` on a 2 GB file loads it all; stream (`-c`, `--stream`) instead.
- `.foo` on missing keys yields `null`, which `select` may keep; guard with `has("foo")` or `//` defaults.
- Shell quoting eats filters: wrap the program in single quotes and pass values with `--arg`.

## Verification

- The filter on the sample returns exactly the expected rows; `jq empty out.json` parses the result; row counts match the source.

## References

- `references/jq-program-bank.md` — twenty copy-paste filters for logs, arrays and tables.
