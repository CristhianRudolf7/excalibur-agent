---
name: systematic-debugging
description: Reproduce the problem systematically, formulate and discard hypotheses, and reach the root
  cause.
metadata:
  excalibur:
    group: debugging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - lsp
    - code_graph
    display:
      name_en: Systematic debugging
      name_es: Depuración sistemática
      desc_en: Reproduce the problem systematically, formulate and discard hypotheses, and reach the root
        cause.
      desc_es: Reproducir, formular y descartar hipótesis hasta la causa raíz.
---
# Systematic debugging

Use this skill for failures that resist a quick look. The method — reproduce, hypothesize, bisect, confirm — converges on the cause with the fewest experiments instead of the most edits.

## When to use

- The cause is not obvious from the error message.
- Intermittent or environment-dependent failures.
- Several components could plausibly be at fault.

## Procedure

1. Reproduce reliably: script the minimal trigger and confirm it fails on demand.
2. List hypotheses ranked by likelihood and cheapness to test.
3. Bisect: halve the search space per experiment (commits, inputs, components, time).
4. Discard each hypothesis with a recorded observation; keep the log.
5. Confirm the survivor by toggling it and watching the symptom follow.

## Output

A hypothesis log with discarded alternatives and the confirmed cause.

## Pitfalls

- Do not change two variables per experiment.
- Intermittents need loops and counts, not single runs.
