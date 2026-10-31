---
name: infrastructure-security-review
description: 'Review infrastructure: network, secrets, cloud permissions, containers.'
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - secrets
    display:
      name_en: Infrastructure security review
      name_es: Revisión de infraestructura
      desc_en: 'Review infrastructure: network, secrets, cloud permissions, containers.'
      desc_es: Revisar red, secretos, permisos de nube y contenedores.
---
# Infrastructure security review

Use this skill for the ground the code runs on. Open networks, leaked secrets, over-broad cloud roles, and privileged containers undo application security silently.

## When to use

- Provisioning or changing infrastructure.
- Reviewing cloud IAM, secrets, or container configs.
- After any infrastructure incident.

## Procedure

1. Review network exposure: what listens, on what, reachable from where.
2. Audit secrets: where they live, who reads them, rotation and expiry.
3. Check cloud permissions against least privilege; remove wildcards and stale grants.
4. Review container posture: users, capabilities, mounts, image provenance.
5. Verify backups and recovery before declaring the review done.

## Output

Infrastructure findings across network, secrets, IAM, and containers.

## Pitfalls

- Secrets in code or logs fail the review instantly.
- Wildcard grants are findings, not conveniences.
