---
name: model-evaluation
description: Evaluate models and agents with reproducible benchmarks and quality, cost and performance
  metrics.
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
      name_en: Model evaluation
      name_es: Evaluación de modelos
      desc_en: Evaluate models and agents with reproducible benchmarks and quality, cost and performance
        metrics.
      desc_es: Evaluar con benchmarks reproducibles y métricas.
---
# Model evaluation

Use this skill to know what a model actually does. Reproducible harnesses with quality, cost, and latency metrics turn impressions into comparable numbers.

## When to use

- Comparing models or agents.
- Validating a release or a claim.
- Building an evaluation harness.

## Procedure

1. Fix tasks, versions, seeds, and budgets before running anything.
2. Measure quality plus cost and performance per task.
3. Run enough repetitions for the statistics claimed; report uncertainty.
4. Guard against contamination: no benchmark solutions in training or context.
5. Publish harness, results, and raw trajectories together.

## Output

Reproducible evaluations with quality, cost, and uncertainty reported.

## Pitfalls

- Contaminated benchmarks flatter; verify isolation.
- Single runs are anecdotes; repeat and report spread.
