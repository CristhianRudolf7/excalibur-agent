---
name: stackoverflow-search
description: Search questions and answers on Stack Overflow.
metadata:
  excalibur:
    group: browser
    version: 1.0.0
    platforms:
    - linux
    tools:
    - source_search
    - web_read
    display:
      name_en: Stack Overflow search
      name_es: Búsqueda en Stack Overflow
      desc_en: Search questions and answers on Stack Overflow.
      desc_es: Buscar preguntas y respuestas en Stack Overflow.
---
# Stack Overflow search

Use this skill for concrete programming errors and API usage. Accepted answers with version context solve specific failures fast — after checking they match your version.

## When to use

- An error message or exception needs its known causes.
- API usage is unclear from the docs.
- Workarounds for a version-specific bug are needed.

## Procedure

1. Search the exact error or API signature.
2. Match answers to the version in use; old fixes mislead on new versions.
3. Prefer answers explaining why, not just code dumps.
4. Test the fix in place before adopting it.
5. Cite the question link with the version it applied to.

## Output

Applicable answers with version match and in-place test result.

## Pitfalls

- Version mismatch is the top failure; check it first.
- Untested pasted code is a liability; run it.
