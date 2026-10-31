---
name: dataset-curation
description: Prepare, filter and deduplicate data for training and evaluation.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Dataset curation
      name_es: Curaduría de conjuntos de datos
      desc_en: Prepare, filter and deduplicate data for training and evaluation.
      desc_es: Preparar, filtrar y deduplicar datos.
---
# Dataset curation

Use this skill because models inherit their data. Documented filtering, deduplication, and decontamination keep training honest and evaluation meaningful.

## When to use

- Building training or evaluation datasets.
- Cleaning raw collected data.
- Checking for train-test contamination.

## Procedure

1. Define inclusion criteria and document every filter applied.
2. Deduplicate within and across splits; check contamination against eval sets.
3. Verify licenses and consent for every source; drop what cannot be used.
4. Record provenance, version, and statistics of the final set.
5. Freeze the dataset version used for each reported result.

## Output

A versioned dataset with filters, dedup, license, and provenance records.

## Pitfalls

- Contaminated evals flatter; check overlap explicitly.
- Undocumented filters make results irreproducible.
