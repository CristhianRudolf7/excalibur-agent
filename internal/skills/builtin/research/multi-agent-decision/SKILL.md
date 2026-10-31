---
name: multi-agent-decision
description: Drive a decision with proponents and critics, comparison under equivalent conditions and
  a maximum round count, where tests weigh more than consensus.
metadata:
  excalibur:
    group: research
    version: 1.0.0
    platforms:
    - linux
    tools:
    - subagent
    - task_db
    display:
      name_en: Multi-agent decision
      name_es: Decisión multiagente
      desc_en: Drive a decision with proponents and critics, comparison under equivalent conditions and
        a maximum round count, where tests weigh more than consensus.
      desc_es: Decidir con proponentes, críticos y pruebas.
---
# Multi-agent decision

Use this skill for contested decisions. Structured proponent-versus-critic rounds under identical conditions, bounded in time, with tests outranking opinions, converge instead of oscillating.

## When to use

- Options split reasonable people.
- A decision keeps reopening.
- Evidence exists but nobody weighed it.

## Procedure

1. Frame the decision and the criteria that will settle it.
2. Assign proponents and critics per option with equal information.
3. Run bounded rounds; compare options under equivalent conditions.
4. Let tests and measurements outrank consensus and seniority.
5. Record the verdict with the evidence that decided it.

## Output

A decided question with weighed evidence and a recorded verdict.

## Pitfalls

- Unbounded debate is procrastination; cap the rounds.
- Consensus without tests is polling, not deciding.
