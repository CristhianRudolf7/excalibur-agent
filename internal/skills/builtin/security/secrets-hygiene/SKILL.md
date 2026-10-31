---
name: secrets-hygiene
description: "Keep credentials out of code, logs and history: detection, rotation and safe storage."
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - grep
    display:
      name_en: Secrets hygiene
      name_es: Higiene de secretos
      desc_en: "Keep credentials out of code, logs and history: detection, rotation and safe storage."
      desc_es: "Credenciales fuera de código, logs e historial: detección y rotación."
---
# Secrets hygiene

Use this skill whenever credentials, tokens or keys appear in the work. Assume anything committed or logged is compromised and act accordingly.

## When to use

- Before committing: scanning the diff for keys, tokens and passwords.
- After a leak: rotating and revoking the exposed credential.
- Storing secrets for local runs (env files, vaults, keyrings).

## Procedure

1. Scan before every commit: `git diff --cached | grep -iE 'api[_-]?key|secret|token|password|BEGIN .* PRIVATE KEY'` plus a committed-secret scanner when configured; review `.env` files are gitignored.
2. Never print or log secrets: redact tool outputs, mask in reports (`<redacted>`), keep keys out of command lines (env vars or files with `0600`).
3. Store properly: local dev in untracked `.env` (0600) or the OS keyring; production in the project's vault (see the secrets skill group) — never in code, docs or chat transcripts.
4. On exposure: revoke/rotate immediately, purge from history only as a courtesy (rotation is the fix; history rewrites do not un-leak), and check access logs for misuse.
5. Use least privilege: separate keys per environment and purpose so one leak has a small blast radius.

## Pitfalls

- `git rm` does not remove history; the secret stays in past commits until rotated.
- Test keys committed "temporarily" become permanent; use obviously-fake placeholders (`EXAMPLE-KEY`) in committed fixtures.
- Echoing secrets into `exec` commands lands them in logs and history; prefer env-file or stdin delivery.

## Verification

- The diff scan is clean, no secret appears in logs or records, and any exposed credential shows as revoked/rotated in its provider console.
