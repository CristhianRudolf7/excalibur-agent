---
name: prompt-optimization
description: Optimize instructions and generate structured responses.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - metrics_query
    display:
      name_en: Prompt optimization
      name_es: Optimización de instrucciones
      desc_en: Optimize instructions and generate structured responses.
      desc_es: Optimizar instrucciones y respuestas estructuradas.
---
# Prompt optimization

Use this skill to make prompts reliably effective. Systematic variants tested on real cases beat lore about magic phrases.

## When to use

- A prompt underperforms on its task.
- Structured output keeps breaking.
- Comparing instruction variants.

## Procedure

1. Define the success metric on representative cases first.
2. Vary one element at a time: role, format, examples, constraints.
3. Measure each variant on the full case set, not on one example.
4. Lock the winner with its score; document why it won.
5. Re-test on schedule; model updates move the optimum.

## Output

An optimized prompt with measured wins and locked version.

## Pitfalls

- One-example tuning overfits; test on sets.
- Magic-phrase lore without measurement is superstition.
