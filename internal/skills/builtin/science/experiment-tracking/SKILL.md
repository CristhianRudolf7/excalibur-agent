---
name: experiment-tracking
description: Record and compare experiments, versions, metrics and traces, keeping evidence and provenance.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - spreadsheet_ops
    display:
      name_en: Experiment tracking
      name_es: Seguimiento de experimentos
      desc_en: Record and compare experiments, versions, metrics and traces, keeping evidence and provenance.
      desc_es: Registrar y comparar experimentos con procedencia.
---
# Experiment tracking

Use this skill so experiments compound instead of evaporating. Every run recorded with versions, metrics, and provenance makes comparisons valid and results recoverable.

## When to use

- Running more than a couple of experiments.
- Comparing variants or hyperparameters.
- Reproducing a past result.

## Procedure

1. Log every run: code, data, config versions plus metrics and traces.
2. Compare variants on identical setups; note every difference.
3. Keep provenance: what produced each artifact, in what order.
4. Review the log on schedule; kill lines that stop informing.
5. Archive with enough to reproduce, not just to admire.

## Output

A queryable experiment log with provenance and comparisons.

## Pitfalls

- Unlogged runs did not happen; log before running.
- Comparisons across differing setups compare setups, not ideas.
