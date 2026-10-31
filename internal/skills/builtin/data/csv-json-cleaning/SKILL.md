---
name: csv-json-cleaning
description: "Clean and reshape CSV and JSON Lines data with csvkit, python one-liners and schema checks."
metadata:
  excalibur:
    group: data
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: CSV and JSON cleaning
      name_es: Limpieza de CSV y JSON
      desc_en: "Clean and reshape CSV and JSON Lines data with csvkit, python one-liners and schema checks."
      desc_es: "Limpiar y remodelar CSV y JSON Lines con csvkit y Python."
---
# CSV and JSON cleaning

Use this skill when data arrives as CSV or JSON/JSONL and must be validated, normalized and converted before analysis.

## When to use

- Unknown delimiters, encodings, headers or mixed types in a CSV.
- Nested JSON that must become a flat table (or vice versa).
- Joining, filtering or sampling datasets too big for a spreadsheet.

## Procedure

1. Profile first: `csvstat data.csv` (csvkit) or `python -c` with the csv module printing row counts, headers and per-column null rates; `head -c 500` to see the raw bytes.
2. Normalize encoding and newlines: convert to UTF-8 (`iconv -f <enc> -t UTF-8`), strip BOMs and `\r` before parsing.
3. Flatten JSONL to CSV with `python -c` + `json` + `csv` modules (explicit field list), or `jq -r '[.a,.b]|@csv'` per record (see `jq-recipes`).
4. Validate the schema: expected columns, no nulls in key fields, value ranges sane; reject or quarantine bad rows with counts, never silently drop.
5. Convert deliberately: `csvjson`/`in2csv` for CSV→JSON, `python pandas .to_parquet()` for analytics (see `parquet-columnar-data`).

## Pitfalls

- Excel "CSV" uses `;` and latin-1 in many locales; sniff the dialect (`csv.Sniffer`) instead of assuming commas.
- Numbers with commas, currency symbols or `%` parse as strings; strip and convert explicitly.
- Huge files need streaming (`csv` reader, `jq -c` per line), not `read()`-everything-into-memory.

## Verification

- Row counts in vs out reconcile (kept + quarantined = input); the schema check passes; a 5-row eyeball sample looks right.
