---
name: parquet-columnar-data
description: "Read, write and inspect Parquet datasets with schemas, partitioning and predicate pushdown."
metadata:
  excalibur:
    group: data
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Parquet columnar data
      name_es: Datos columnares Parquet
      desc_en: "Read, write and inspect Parquet datasets with schemas, partitioning and predicate pushdown."
      desc_es: "Leer y escribir Parquet con esquemas, particiones y pushdown."
---
# Parquet columnar data

Use this skill for analytics-sized tabular data. Parquet's columnar layout plus explicit schemas beats CSV on size, speed and type safety.

## When to use

- Datasets too big or too loosely typed for CSV.
- Reading a subset of columns or filtered rows fast.
- Exchanging typed tables between Python, R and query engines.

## Procedure

1. Inspect without loading all: `parquet-tools schema data.parquet`, `parquet-tools head -n 5 data.parquet`, or `python -c 'import pyarrow.parquet as pq; print(pq.read_schema("data.parquet"))'`.
2. Read selectively: `pq.read_table(path, columns=["a","b"], filters=[("year","==",2024)])` pushes projection and predicates into the reader.
3. Write partitioned datasets: `pq.write_to_dataset(table, root, partition_cols=["year","month"])` for time-sliced data.
4. Convert from CSV/JSON once, at ingest: `pandas.read_csv(...).to_parquet(...)` with explicit dtypes; record the schema version next to the data.
5. Mind the engines: keep `pyarrow` and `pandas` versions pinned (see `python-pip-uv-venv`); snappy is the default sane codec.

## Pitfalls

- Schema drift across files (int vs string for one column) breaks dataset reads; enforce schemas at write time.
- Thousands of tiny row groups or partition files kill performance; target ~100 MB–1 GB per file.
- Timestamps need explicit timezones; naive vs aware mixes corrupt joins silently.

## Verification

- Schema print matches the contract; row counts and a checksum column reconcile against the source CSV.
