---
name: official-docs-lookup
description: Consult the official documentation before deciding on frameworks and libraries, citing the
  version and flagging the unverified.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - web_search
    - web_read
    - source_search
    display:
      name_en: Official docs lookup
      name_es: Consulta de documentación oficial
      desc_en: Consult the official documentation before deciding on frameworks and libraries, citing
        the version and flagging the unverified.
      desc_es: Consultar la documentación oficial citando la versión.
---
# Official docs lookup

Use this skill before betting on a framework or library behavior. Official docs at a pinned version beat memory and forum posts; anything unverified gets flagged, not trusted.

## When to use

- Choosing or upgrading a dependency.
- Behavior contradicts your expectation of a library.
- An API detail decides a design.

## Procedure

1. Find the official docs for the exact version in use; cite version and page.
2. Confirm the behavior in the docs; run a minimal check when docs are ambiguous.
3. Flag anything you could not verify instead of asserting it.
4. Prefer primary sources over tutorials and forum answers.
5. Record the version and links with the decision.

## Output

A decision backed by cited official docs at a pinned version, with gaps flagged.

## Pitfalls

- Memory of an API is not a source; look it up.
- Unverified claims travel flagged, never as facts.
