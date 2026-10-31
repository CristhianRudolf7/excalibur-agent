---
name: sqlite-recipes
description: "Query, inspect and migrate SQLite databases from the command line and scripts."
metadata:
  excalibur:
    group: databases
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: SQLite recipes
      name_es: Recetas SQLite
      desc_en: "Query, inspect and migrate SQLite databases from the command line and scripts."
      desc_es: "Consultar e migrar SQLite desde CLI y scripts."
---
# SQLite recipes

Use this skill for file-based relational data: inspect schemas, run analytical queries, and migrate safely with dumps.

## When to use

- Exploring an unknown `.db`/`.sqlite` file.
- Answering a data question with SQL instead of scripts.
- Migrating schema or moving data between SQLite files.

## Procedure

1. Inspect: `sqlite3 app.db '.tables'`, `sqlite3 app.db '.schema <table>'`, `sqlite3 app.db 'SELECT COUNT(*) FROM <table>;'`.
2. Query readably: `sqlite3 -header -column app.db 'SELECT ...'` for humans, `-csv`/`-json` for pipelines; always `LIMIT 5` first on unknown tables.
3. Migrate with dumps: `sqlite3 old.db .dump > m.sql`, review, then `sqlite3 new.db < m.sql`; wrap multi-statement changes in `BEGIN; ... COMMIT;`.
4. Check integrity: `sqlite3 app.db 'PRAGMA integrity_check;'` and `PRAGMA foreign_key_check;` before trusting results.
5. From Python use the stdlib `sqlite3` module with parameterized queries (`?` placeholders), never string-built SQL.

## Pitfalls

- SQLite types are flexible (type affinity, not rigid types): dirty data hides in "integer" columns; validate with `typeof()`.
- Concurrent writers lock the file (`database is locked`); use WAL mode (`PRAGMA journal_mode=WAL;`) or a server DB for writers.
- `.dump` of a huge DB is slow and single-shot; prefer incremental exports for large tables.

## Verification

- `integrity_check` returns `ok`; row counts before/after migration match; the target query returns expected rows.
