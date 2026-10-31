---
name: web-navigation
description: 'Operate the browser with the 14.2 contract: wait for load, observe DOM and capture, act
  by reference and handle blocks (CAPTCHA, login).'
metadata:
  excalibur:
    group: browser
    version: 1.0.0
    platforms:
    - linux
    tools:
    - browser_open_session
    - browser_navigate
    - browser_snapshot
    - browser_act
    - browser_tabs
    - browser_close_session
    display:
      name_en: Web navigation
      name_es: Navegación web
      desc_en: 'Operate the browser with the 14.2 contract: wait for load, observe DOM and capture, act
        by reference and handle blocks (CAPTCHA, login).'
      desc_es: Operar el navegador con el contrato 14.2.
---
# Web navigation

Use this skill for any browser-driven task. Contract navigation — wait, observe, act by reference — plus explicit block handling keeps automated browsing reliable instead of flaky.

## When to use

- Reading or acting on web pages through the browser.
- Forms, logins, or multi-step flows are involved.
- CAPTCHAs or bot checks may appear.

## Procedure

1. Open a session with the right profile; navigate and wait for the needed readiness.
2. Snapshot: read DOM and capture, then act by element reference, never by coordinates.
3. Handle blocks explicitly: report CAPTCHAs and logins instead of forcing them.
4. Verify each action's effect in a fresh snapshot before proceeding.
5. Close the session and report what was read or changed.

## Output

Browser results with snapshots backing each action.

## Pitfalls

- Coordinates rot; act by reference only.
- Blocked means blocked: report, don't circumvent.
