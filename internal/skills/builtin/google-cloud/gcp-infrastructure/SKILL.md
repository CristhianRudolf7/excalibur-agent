---
name: gcp-infrastructure
description: Manage infrastructure on Google Cloud and Kubernetes (GKE).
metadata:
  excalibur:
    group: google-cloud
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: GCP infrastructure
      name_es: Infraestructura en GCP
      desc_en: Manage infrastructure on Google Cloud and Kubernetes (GKE).
      desc_es: Administrar infraestructura en GCP y GKE.
---
# GCP infrastructure

Use this skill for Google Cloud and GKE operations. Declared infrastructure, right-sized clusters, and least-privilege IAM keep the platform reproducible and auditable.

## When to use

- Provisioning or changing GCP resources or GKE clusters.
- Debugging workloads on Google Kubernetes Engine.
- Reviewing cost or IAM on GCP.

## Procedure

1. Declare resources as code; import strays instead of tolerating drift.
2. Right-size clusters and workloads: requests, limits, and autoscaling from metrics.
3. Apply least-privilege IAM with workload identity; audit bindings regularly.
4. Diagnose with platform logs and traces from the failing workload outward.
5. Verify changes in a non-production project first.

## Output

Reproducible GCP/GKE state with verified changes and IAM evidence.

## Pitfalls

- Console click-ops drift; keep everything declared.
- Cluster-admin bindings are findings until justified.
