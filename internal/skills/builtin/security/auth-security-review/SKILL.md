---
name: auth-security-review
description: Review authentication, sessions, token management and account recovery.
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - grep
    - code_graph
    display:
      name_en: Auth security review
      name_es: Revisión de autenticación
      desc_en: Review authentication, sessions, token management and account recovery.
      desc_es: Revisar autenticación, sesiones, tokens y recuperación.
---
# Auth security review

Use this skill for anything that decides who a user is. Session handling, token storage, and recovery flows are high-value targets; each gets checked against its specific failure catalog.

## When to use

- Implementing or reviewing login, sessions, or tokens.
- Adding SSO, passwordless, or recovery flows.
- Session or account-takeover issues appear.

## Procedure

1. Review credential handling: storage, comparison, rotation, and lockout policy.
2. Check session and token lifecycle: issuance, storage, expiry, revocation, and binding.
3. Walk the recovery flow as an attacker: enumeration, token guessability, expiry.
4. Verify MFA enrollment, recovery codes, and fallback paths.
5. Confirm secrets and signing keys come from the vault, never from code.

## Output

Auth findings with attack-path evidence and lifecycle fixes.

## Pitfalls

- Recovery flows are authentication; audit them as such.
- Client-side session data is attacker-controlled; treat it so.
