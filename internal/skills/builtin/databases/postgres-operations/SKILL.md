---
name: postgres-operations
description: "Run Postgres locally, manage roles and databases, and load, query and dump data with psql."
metadata:
  excalibur:
    group: databases
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Postgres operations
      name_es: Operación de Postgres
      desc_en: "Run Postgres locally, manage roles and databases, and load, query and dump data with psql."
      desc_es: "Postgres local: roles, bases, carga, consultas y dumps con psql."
---
# Postgres operations

Use this skill when the task needs a real server database. Start a local instance (or container), connect with `psql`, and keep credentials and dumps disciplined.

## When to use

- Setting up a local Postgres for development or tests.
- Creating roles/databases, loading fixtures, running migrations.
- Debugging connection, auth or permission errors.

## Procedure

1. Start locally: distro package (`systemctl start postgresql`) or container (`docker run -e POSTGRES_PASSWORD=... -p 5432:5432 postgres:16`); confirm with `pg_isready`.
2. Connect: `psql -h localhost -U <user> -d <db>`; set `PGPASSWORD` in the environment (never on the command line) or use a `~/.pgpass` file with `0600` perms.
3. Inspect and query: `\l` (databases), `\dt` (tables), `\d <table>` (schema), `\timing` for slow-query checks; script queries with `psql -X -q -c '...'`.
4. Load and dump: `psql -d db -f seed.sql` for fixtures; `pg_dump -Fc db > db.dump` and `pg_restore -d db2 db.dump` for moves; plain `pg_dump db > db.sql` for readable diffs.
5. Migrate safely: one migration at a time, inside a transaction when the tool allows, with a dump taken first.

## Pitfalls

- `peer` auth as the OS user vs `md5`/`scram` passwords confuse first setups; check `pg_hba.conf` when auth fails unexpectedly.
- Passwords in commands leak to history and process lists; use env vars or `.pgpass`.
- `DROP` without a fresh dump is unrecoverable; dump first, drop second.

## Verification

- `pg_isready` succeeds, the app connects with its configured DSN, and row counts match after every load or restore.
