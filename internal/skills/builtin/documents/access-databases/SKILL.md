---
name: access-databases
description: Read and migrate Access databases.
metadata:
  excalibur:
    group: documents
    version: 1.0.0
    platforms:
    - linux
    tools:
    - document_read
    - userdb
    - file_convert
    display:
      name_en: Access databases
      name_es: Bases de datos de Access
      desc_en: Read and migrate Access databases.
      desc_es: Leer y migrar bases de datos de Access.
---
# Access databases

Use this skill to extract value from Access databases. Inventory, migrate data faithfully, and rebuild queries and forms in the target — tested row by row.

## When to use

- Reading legacy Access databases.
- Migrating Access to a modern store.
- Recovering queries and logic from MDB/ACCDB.

## Procedure

1. Inventory tables, queries, forms, macros, and linked sources.
2. Extract schema and data with types and constraints preserved.
3. Migrate to the target with row-count and checksum verification per table.
4. Rebuild essential queries and validate results against Access output.
5. Document what was migrated, what was dropped, and why.

## Output

Migrated data with per-table verification and a coverage report.

## Pitfalls

- Row counts and checksums per table, or migration is a guess.
- Macros rarely migrate; inventory their logic explicitly.
