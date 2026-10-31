---
name: google-developer-tools
description: Use Google's developer tools (CLI, SDK, consoles).
metadata:
  excalibur:
    group: google-cloud
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - web_search
    display:
      name_en: Google developer tools
      name_es: Herramientas de desarrollo de Google
      desc_en: Use Google's developer tools (CLI, SDK, consoles).
      desc_es: Usar CLI, SDK y consolas de Google.
---
# Google developer tools

Use this skill to drive Google tooling efficiently. The right CLI flags, SDK patterns, and console views turn cloud chores into repeatable commands.

## When to use

- Working with gcloud, gsutil, or Google SDKs.
- Automating GCP tasks in scripts.
- Finding the relevant console view fast.

## Procedure

1. Use the CLI with explicit project, region, and format flags; script the repetition.
2. Prefer SDK calls over screen-scraping consoles in automation.
3. Authenticate with service accounts and short-lived credentials, never user keys in scripts.
4. Check the official docs for the exact command version before scripting.
5. Save working commands as project runbooks.

## Output

Repeatable Google-tooling commands saved as runbooks.

## Pitfalls

- User credentials in scripts leak; use service accounts.
- Implicit project or region flags act on the wrong target.
