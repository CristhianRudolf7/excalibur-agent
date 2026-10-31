---
name: data-governance
description: Manage quality, lineage, catalog and permissions of the user's data.
metadata:
  excalibur:
    group: research
    version: 1.0.0
    platforms:
    - linux
    tools:
    - userdb
    - bi_ops
    display:
      name_en: Data governance
      name_es: Gobierno de datos
      desc_en: Manage quality, lineage, catalog and permissions of the user's data.
      desc_es: Gestionar calidad, linaje, catálogo y permisos.
---
# Data governance

Use this skill to keep the user's data trustworthy and controlled. Lineage, quality rules, a catalog, and enforced permissions make data usable without leaking or rotting.

## When to use

- Data accumulates across projects and people.
- Nobody knows where a dataset came from.
- Access needs restricting by scope.

## Procedure

1. Catalog datasets with owner, description, freshness, and location.
2. Record lineage: source, transformations, and refresh schedule per dataset.
3. Enforce quality rules at entry; quarantine, don't silently fix, violations.
4. Gate access by scope with explicit grants; audit who sees what.
5. Review the catalog on schedule; archive what nobody uses.

## Output

A governed catalog with lineage, quality rules, and enforced access.

## Pitfalls

- Unknown lineage means untrusted data; label it so.
- Permissions by default-open leak; default to scoped.
