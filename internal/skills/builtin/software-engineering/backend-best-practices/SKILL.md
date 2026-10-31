---
name: backend-best-practices
description: 'Framework best practices for backends: structure, errors, persistence, concurrency, configuration
  and security.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - lsp
    - code_graph
    display:
      name_en: Backend best practices
      name_es: Buenas prácticas de backend
      desc_en: 'Framework best practices for backends: structure, errors, persistence, concurrency, configuration
        and security.'
      desc_es: Buenas prácticas de frameworks de backend.
---
# Backend best practices

Use this skill when writing or reviewing backend code. It pins the non-negotiables — layering, error taxonomy, persistence discipline, concurrency safety, config handling, and secret hygiene — to the project's framework.

## When to use

- Starting backend work in a framework.
- Reviewing handlers, migrations, or concurrency code.
- Errors, config, or secrets look ad hoc.

## Procedure

1. Match the framework's structure: handlers thin, logic in services, persistence behind repositories.
2. Use the project's error taxonomy; never leak internals to clients.
3. Guard persistence with transactions and migrations; no ad hoc schema edits.
4. Make concurrency explicit and bounded; share nothing mutable without a guard.
5. Read config from the environment with validated defaults; secrets only via the vault.

## Output

Backend code following the framework's structure, error, persistence, and security rules.

## Pitfalls

- No business logic in transport handlers.
- No secrets in code, config files, or logs.
