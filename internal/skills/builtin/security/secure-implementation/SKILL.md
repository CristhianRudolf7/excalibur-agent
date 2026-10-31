---
name: secure-implementation
description: 'Apply protections when coding: authentication, secret management, input validation and dependency
  review.'
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - lsp
    - secrets
    display:
      name_en: Secure implementation
      name_es: Implementación segura
      desc_en: 'Apply protections when coding: authentication, secret management, input validation and
        dependency review.'
      desc_es: Aplicar protecciones al programar.
---
# Secure implementation

Use this skill while writing code, not after. Authentication checks, validated inputs, vault-held secrets, and reviewed dependencies built in from the first line cost little and prevent the findings other skills hunt.

## When to use

- Implementing features that handle auth, secrets, or untrusted input.
- Writing new endpoints, parsers, or integrations.
- Hardening existing code incrementally.

## Procedure

1. Validate every input at the boundary; encode every output for its context.
2. Enforce authentication and authorization on each operation, defaulting to deny.
3. Keep secrets in the vault from creation to use; never log or return them.
4. Review each added dependency for advisories and maintenance before merging.
5. Run the security checks of the suite and fix new warnings before committing.

## Output

Code with boundary validation, enforced auth, vault secrets, and reviewed deps.

## Pitfalls

- Deny by default; allowlists beat denylists.
- Convenience shortcuts in security code become permanent holes.
