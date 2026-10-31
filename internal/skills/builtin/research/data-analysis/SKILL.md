---
name: data-analysis
description: 'Analyze and visualize tabular data: cleaning, statistics, comparisons and charts.'
metadata:
  excalibur:
    group: research
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - chart_render
    display:
      name_en: Data analysis
      name_es: Análisis de datos
      desc_en: 'Analyze and visualize tabular data: cleaning, statistics, comparisons and charts.'
      desc_es: Analizar y visualizar datos tabulares.
---
# Data analysis

Use this skill for tabular questions. Cleaned data, appropriate statistics, and honest charts turn a CSV into an answer with stated uncertainty.

## When to use

- A CSV, sheet, or table needs answers.
- Comparing groups, periods, or variants.
- A chart must support a claim.

## Procedure

1. Inspect and clean: types, missing values, duplicates, outliers — logged, not silent.
2. Describe before inferring: distributions, then comparisons with the right test.
3. Visualize honestly: axes from zero where it matters, no truncated drama.
4. Quantify uncertainty: intervals and sample sizes travel with every claim.
5. Keep the analysis reproducible: script, data version, and seed recorded.

## Output

Cleaned data, statistics with uncertainty, and honest charts.

## Pitfalls

- Silent cleaning manufactures results; log every change.
- A chart without sample size is decoration.
