---
name: documentation-updates
description: Update project documentation following the changes made.
metadata:
  excalibur:
    group: documents
    version: 1.0.0
    platforms:
    - linux
    tools:
    - document_read
    - document_edit
    - read_file
    display:
      name_en: Documentation updates
      name_es: Actualización de documentación
      desc_en: Update project documentation following the changes made.
      desc_es: Actualizar la documentación según los cambios.
---
# Documentation updates

Use this skill to keep docs true after code changes. Every behavior, API, or setup change updates its document in the same task — docs rot the moment they diverge.

## When to use

- Code, API, or setup changed in a task.
- Docs contradict the implementation.
- Closing a task that touched documented behavior.

## Procedure

1. List the documents covering the changed area before editing.
2. Update each: behavior, examples, commands, and version notes.
3. Re-run every example and command quoted in the touched docs.
4. Remove or fix what the change invalidated; no orphan paragraphs.
5. Record doc updates as task evidence like code changes.

## Output

Docs matching the implementation, with examples re-verified.

## Pitfalls

- Untested doc examples lie; run each one.
- Docs updated 'later' stay outdated; do it in the task.
