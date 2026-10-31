---
name: google-workspace-automation
description: Automate tasks in Google Workspace (Docs, Sheets, Drive, Gmail, Calendar) via connectors
  or browser routines.
metadata:
  excalibur:
    group: personal-assistant
    version: 1.0.0
    platforms:
    - linux
    tools:
    - email
    - calendar
    display:
      name_en: Google Workspace automation
      name_es: Automatización de Google Workspace
      desc_en: Automate tasks in Google Workspace (Docs, Sheets, Drive, Gmail, Calendar) via connectors
        or browser routines.
      desc_es: Automatizar tareas en Docs, Sheets, Drive, Gmail y Calendar.
---
# Google Workspace automation

Use this skill for Google Workspace chores. Connectors first, browser routines when no connector exists — either way with explicit confirmation for irreversible actions.

## When to use

- Docs, Sheets, Drive, Gmail, or Calendar tasks recur.
- Moving data between Workspace apps.
- No direct connector covers the operation.

## Procedure

1. Prefer the connector for the app; fall back to a recorded browser routine otherwise.
2. Read before writing: confirm the target document, range, or thread.
3. Parametrize routines so they survive UI text changes; assert per step.
4. Confirm before sending, sharing, or deleting anything.
5. Log what the automation changed so the user can audit it.

## Output

A working Workspace automation with audit trail and confirmed side effects.

## Pitfalls

- Sharing and sending need confirmation every time.
- Hardcoded IDs rot; resolve targets dynamically.
