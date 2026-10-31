---
name: common-pitfalls
description: 'Review the frequent-mistake list before delivering: skipped tests, out-of-scope changes,
  embedded paths or credentials, uncleaned generated files, outdated docs, unchecked outputs.'
metadata:
  excalibur:
    group: verification
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    - grep
    display:
      name_en: Common pitfalls
      name_es: Errores frecuentes
      desc_en: 'Review the frequent-mistake list before delivering: skipped tests, out-of-scope changes,
        embedded paths or credentials, uncleaned generated files, outdated docs, unchecked outputs.'
      desc_es: Lista de errores frecuentes a revisar antes de entregar.
---
# Common pitfalls

Use this skill as the last checklist before delivery. Most failed gates fail on the same handful of avoidable mistakes; this pass catches them in minutes.

## When to use

- Right before requesting the completion gate.
- After commands or scripts touched the tree outside the editor.
- When credentials, paths, or generated files were involved.

## Procedure

1. Confirm no test was skipped, disabled, or left failing; grep for skip markers and TODOs tied to this task.
2. Diff the change set: every change must trace to a requirement; revert the rest.
3. Search for embedded absolute paths, tokens, and credentials in the diff.
4. Remove generated files and scratch output; confirm .gitignore covers them.
5. Check that touched docs were updated and every tool output was actually read, not assumed.

## Output

A pitfalls checklist with findings fixed or explicitly accepted, attached to the delivery.

## Pitfalls

- Do not hand-wave generated files; list and remove each.
- Never commit or report a credential; redact and rotate if one leaked.
