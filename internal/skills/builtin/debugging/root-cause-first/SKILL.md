---
name: root-cause-first
description: Propose no fixes until the error is identified and checked with evidence.
metadata:
  excalibur:
    group: debugging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - grep
    - history_search
    display:
      name_en: Root cause first
      name_es: Causa raíz primero
      desc_en: Propose no fixes until the error is identified and checked with evidence.
      desc_es: No proponer soluciones hasta comprobar la causa con evidencia.
---
# Root cause first

Use this skill at the start of any debugging. Guessing fixes wastes cycles and masks the real defect; a cause confirmed by evidence makes the fix obvious and the regression test writable.

## When to use

- Any failure, flake, or wrong behavior is reported.
- Someone proposes a fix before the cause is known.
- A previous 'fix' did not hold.

## Procedure

1. Reproduce the failure and capture the exact error, input, and version.
2. Form hypotheses and kill them with evidence: logs, traces, minimal cases.
3. State the cause only when an experiment confirms it: disabling the cause removes the symptom.
4. Write the fix against the confirmed cause, not the symptom.
5. Add the regression test before closing.

## Output

A confirmed cause with its evidence, then a fix and a regression test.

## Pitfalls

- No fix proposals before a confirmed cause — enforce the order.
- Correlation in logs is not confirmation; demand the disabling experiment.
