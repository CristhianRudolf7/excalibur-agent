---
name: gcp-security-operations
description: Review security, IAM permissions, logs and monitoring on Google Cloud.
metadata:
  excalibur:
    group: google-cloud
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - metrics_query
    display:
      name_en: GCP security operations
      name_es: Seguridad y operaciones en GCP
      desc_en: Review security, IAM permissions, logs and monitoring on Google Cloud.
      desc_es: Revisar IAM, registros y monitoreo en GCP.
---
# GCP security operations

Use this skill to keep a GCP footprint secure and observable. IAM review, audit logs, and alerting form the loop that catches misconfiguration before attackers do.

## When to use

- Reviewing GCP security posture.
- Setting up logging and alerting for cloud resources.
- Responding to a cloud security finding.

## Procedure

1. Audit IAM bindings across projects: remove wildcards, stale members, and over-broad roles.
2. Enable audit logs for admin and data access; ship them where they cannot be muted.
3. Alert on the events that matter: IAM changes, key creation, public exposure.
4. Review findings from the platform's security tools and triage with owners.
5. Rehearse the incident path: who gets paged, with what access, in which order.

## Output

A hardened GCP posture with logs, alerts, and triaged findings.

## Pitfalls

- Logs nobody reads are compliance theater; alert on them.
- Stale access outlives employment; review memberships on schedule.
