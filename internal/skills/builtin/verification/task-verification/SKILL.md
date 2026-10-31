---
name: task-verification
description: Check that every requirement has fresh evidence, that the full test folder ran green on the
  current version, and that the result answers the original request. Used by the independent verifier
  (8.7).
metadata:
  excalibur:
    group: verification
    version: 1.0.0
    platforms:
    - linux
    tools:
    - task_db
    - history_search
    - get_tool_output
    display:
      name_en: Task verification
      name_es: Verificación de tareas
      desc_en: Check that every requirement has fresh evidence, that the full test folder ran green on
        the current version, and that the result answers the original request. Used by the independent
        verifier (8.7).
      desc_es: Comprobar requisitos, evidencias frescas y tests antes de dar por terminado.
---
# Task verification

Use this skill as the final gate before calling work done. It answers three questions with evidence: is every requirement covered by fresh checks, did the whole suite pass on this exact version, and does the result match what was asked.

## When to use

- A task claims completion and the gate must decide.
- Acting as the independent verifier on large tasks.
- Before merging, releasing, or reporting a result.

## Procedure

1. List every requirement and its criteria; for each, fetch the latest evidence and check it is fresh (current code version, current fingerprints).
2. Run the full test folder on the current version; partial runs or runs on older versions do not count.
3. Contrast the delivered result against the literal original request, not against the plan.
4. Report pass or fail per criterion with evidence IDs; never summarize without references.
5. Fail the gate on any stale evidence, skipped test, or mismatch with the request.

## Output

A per-criterion verdict with evidence references and the gate decision: complete or not, with what is missing.

## Pitfalls

- Freshness is mandatory: evidence from a previous version is not evidence.
- The verifier never trusts the implementer's summary; it re-checks.
