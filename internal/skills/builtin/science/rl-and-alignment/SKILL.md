---
name: rl-and-alignment
description: Research reinforcement learning and preference alignment.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - gpu_training
    display:
      name_en: RL and alignment
      name_es: RL y alineamiento
      desc_en: Research reinforcement learning and preference alignment.
      desc_es: Investigar RL y alineamiento con preferencias.
---
# RL and alignment

Use this skill for preference optimization and RL. Reward design, KL discipline, and human evaluation separate aligned improvements from reward hacking.

## When to use

- Aligning a model with preferences.
- Designing rewards for RL training.
- Evaluating alignment interventions.

## Procedure

1. Define the target behavior with examples, not just principles.
2. Design rewards that measure the behavior; probe for hackable proxies.
3. Constrain drift from the reference; monitor KL and qualitative samples.
4. Evaluate with humans on the behaviors that matter, plus capability retention.
5. Report failures and hacks alongside successes.

## Output

Aligned models with reward design, evaluations, and hack reports.

## Pitfalls

- Proxy rewards get hacked; monitor for gaming explicitly.
- Capability regressions hide behind preference wins; test both.
