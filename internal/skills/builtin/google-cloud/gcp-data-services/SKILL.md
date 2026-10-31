---
name: gcp-data-services
description: Manage databases and data services like BigQuery.
metadata:
  excalibur:
    group: google-cloud
    version: 1.0.0
    platforms:
    - linux
    tools:
    - userdb
    - exec
    display:
      name_en: GCP data services
      name_es: Datos en GCP
      desc_en: Manage databases and data services like BigQuery.
      desc_es: Gestionar bases de datos y BigQuery.
---
# GCP data services

Use this skill for data work on Google's platform. Schema design, partitioning, access control, and query cost discipline decide whether BigQuery and Cloud SQL stay fast and affordable.

## When to use

- Designing datasets, tables, or pipelines on GCP.
- BigQuery costs or query times spike.
- Setting up backups and access for cloud databases.

## Procedure

1. Design schemas with partitioning and clustering for the actual query patterns.
2. Control cost: estimate bytes scanned before running, cap interactive budgets.
3. Enforce access with dataset roles and authorized views, not shared keys.
4. Set backups, retention, and disaster-recovery tested by restore, not by hope.
5. Monitor slow queries and costs with alerts, not month-end surprises.

## Output

Governed data services with cost-controlled queries and tested recovery.

## Pitfalls

- Unbounded queries bill by the terabyte; estimate first.
- Shared keys instead of roles fail every audit.
