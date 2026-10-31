---
name: github-issues
description: Read, create, classify, comment and close GitHub issues and pull requests, linking them with
  tasks and requirements.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - github
    display:
      name_en: GitHub issues
      name_es: Issues de GitHub
      desc_en: Read, create, classify, comment and close GitHub issues and pull requests, linking them
        with tasks and requirements.
      desc_es: Gestionar issues y pull requests enlazados con tareas.
---
# GitHub issues

Use this skill for any GitHub issue or PR work. Issues stay linked to requirements and tasks so nothing tracked in GitHub drifts from what the harness executes.

## When to use

- Triaging, implementing, or closing an issue.
- Opening or reviewing a pull request.
- Linking external discussion to internal tasks.

## Procedure

1. Read the issue or PR fully, including linked threads, before acting.
2. Create issues with reproduction, expected behavior, and acceptance criteria.
3. Link each issue to its requirements and tasks; keep states in sync both ways.
4. Comment progress with evidence, not intentions; close only when checks pass.
5. Never force-push or rewrite history without asking.

## Output

Issues and PRs with criteria, links to tasks, and state synced on both sides.

## Pitfalls

- No issue without acceptance criteria.
- Closing without green checks reopens the work, not the thread.
