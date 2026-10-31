---
name: microsoft-365-automation
description: Automate tasks in Microsoft 365 (Word, Excel, Outlook, Teams, OneDrive) via connectors or
  browser routines.
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
      name_en: Microsoft 365 automation
      name_es: Automatización de Microsoft 365
      desc_en: Automate tasks in Microsoft 365 (Word, Excel, Outlook, Teams, OneDrive) via connectors
        or browser routines.
      desc_es: Automatizar tareas en Word, Excel, Outlook, Teams y OneDrive.
---
# Microsoft 365 automation

Use this skill for Microsoft 365 chores. Same discipline as Workspace: connectors first, assertive routines otherwise, confirmation for everything irreversible.

## When to use

- Word, Excel, Outlook, Teams, or OneDrive tasks recur.
- Moving data between Microsoft 365 apps.
- Teams notifications or file operations need automation.

## Procedure

1. Prefer the connector; record a browser routine only when none exists.
2. Confirm tenant, mailbox, or site before acting — M365 holds lookalikes.
3. Parametrize and assert each routine step; UIs shift without notice.
4. Confirm before sending mail, posting, sharing, or deleting.
5. Report exactly what changed, where, for audit.

## Output

A working M365 automation with audit trail and confirmed side effects.

## Pitfalls

- Similar names across tenants and sites; verify the target.
- Posts and shares are public acts; confirm them.
