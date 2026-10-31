---
name: codebase-exploration
description: 'Find your way around an unknown repository: structure, entry points, conventions and build/test
  commands, using the code graph.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - code_graph
    - glob
    - grep
    - read_file
    display:
      name_en: Codebase exploration
      name_es: Exploración de repositorios
      desc_en: 'Find your way around an unknown repository: structure, entry points, conventions and build/test
        commands, using the code graph.'
      desc_es: Orientarse en un repositorio desconocido con el grafo de código.
---
# Codebase exploration

Use this skill first in any unfamiliar repository. A systematic tour — layout, entry points, conventions, how to build and test — prevents wrong assumptions that cost hours later.

## When to use

- Starting work in a repository you have not touched.
- Onboarding a subagent onto an existing codebase.
- Estimating a change before touching code.

## Procedure

1. Map the layout: entry points, module boundaries, and where tests live.
2. Read the build and test commands from manifests and docs; run the fastest check to confirm the setup works.
3. Follow the request's feature through the code graph: who calls what, where state lives.
4. Note conventions: naming, error handling, logging, and config patterns.
5. Summarize the mental model in a few lines before proposing any change.

## Output

A short orientation brief: structure, entry points, conventions, verified build/test commands.

## Pitfalls

- Do not edit before the orientation brief exists.
- Verify build commands by running them; docs lie.
