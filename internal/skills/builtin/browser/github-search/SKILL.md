---
name: github-search
description: Search repositories, code, issues, releases and discussions on GitHub.
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
      name_en: GitHub search
      name_es: Búsqueda en GitHub
      desc_en: Search repositories, code, issues, releases and discussions on GitHub.
      desc_es: Buscar repos, código, issues y releases en GitHub.
---
# GitHub search

Use this skill to mine GitHub: code examples, issue history, releases, and discussions that answer how software really behaves.

## When to use

- Evaluating a library or pattern.
- An error message needs its issue history.
- Release notes or migration guides are needed.

## Procedure

1. Search code for real usage, not just READMEs.
2. Read issues for failure modes and maintainer stance before adopting.
3. Check releases for breaking changes affecting the pinned version.
4. Mine discussions for trade-offs the docs omit.
5. Record links and versions with every conclusion.

## Output

GitHub-grounded findings with links and versions.

## Pitfalls

- README promises differ from issue reality; read both.
- Pin conclusions to versions; defaults move.
