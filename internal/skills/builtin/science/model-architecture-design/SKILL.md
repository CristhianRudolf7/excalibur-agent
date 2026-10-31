---
name: model-architecture-design
description: Design and compare model architectures and tokenizers.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - source_search
    display:
      name_en: Model architecture design
      name_es: Diseño de arquitecturas
      desc_en: Design and compare model architectures and tokenizers.
      desc_es: Diseñar y comparar arquitecturas y tokenizadores.
---
# Model architecture design

Use this skill for architecture choices. Controlled comparisons with matched budgets reveal what actually helps instead of what sounds elegant.

## When to use

- Designing or modifying a model architecture.
- Choosing or training a tokenizer.
- Comparing architectural variants.

## Procedure

1. Fix the comparison budget: parameters, data, and compute matched.
2. Vary one factor at a time; ablate deliberately.
3. Train tokenizers on the target distribution and measure fertility and coverage.
4. Evaluate on the tasks that matter, not on perplexity alone.
5. Record configs and results for every variant tried.

## Output

Compared architectures with controlled results and chosen design.

## Pitfalls

- Unmatched budgets compare spending, not ideas.
- Perplexity alone misranks task performance.
