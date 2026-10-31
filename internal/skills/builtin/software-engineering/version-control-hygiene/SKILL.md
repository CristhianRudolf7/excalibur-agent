---
name: version-control-hygiene
description: 'Make coherent commits and versions matching the changes: scope per commit, clear messages,
  semantic versioning.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Version control hygiene
      name_es: Higiene de control de versiones
      desc_en: 'Make coherent commits and versions matching the changes: scope per commit, clear messages,
        semantic versioning.'
      desc_es: Commits coherentes, mensajes claros y versionado semántico.
---
# Version control hygiene

Use this skill to keep history readable and releases safe. One concern per commit, messages that explain why, versions that mean what semver says.

## When to use

- Committing task work.
- Cutting a release or bumping a version.
- History is tangled and needs discipline, not rewriting.

## Procedure

1. Stage one concern per commit; split mixed changes before committing.
2. Write messages with what and why; reference the task or issue.
3. Version by semver: breaking, feature, or fix — no guesses.
4. Never rewrite published history or push without consent.
5. Tag releases with notes generated from the merged changes.

## Output

Clean commits and correct versions, traceable to tasks.

## Pitfalls

- Mixed-concern commits hide regressions; split them.
- Published history is immutable without explicit permission.
