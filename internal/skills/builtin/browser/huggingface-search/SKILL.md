---
name: huggingface-search
description: Search models, datasets and spaces on Hugging Face.
metadata:
  excalibur:
    group: browser
    version: 1.0.0
    platforms:
    - linux
    tools:
    - source_search
    - web_read
    display:
      name_en: Hugging Face search
      name_es: Búsqueda en Hugging Face
      desc_en: Search models, datasets and spaces on Hugging Face.
      desc_es: Buscar modelos, conjuntos de datos y espacios.
---
# Hugging Face search

Use this skill to find models, datasets, and demos with their cards, licenses, and metrics — the metadata that decides whether an artifact is usable, not just interesting.

## When to use

- Choosing a model, dataset, or demo.
- Checking licenses and intended uses.
- Comparing model variants and metrics.

## Procedure

1. Search Hub for the task and filter by license, language, and library.
2. Read the model or dataset card completely, including limitations and bias notes.
3. Check metrics, evaluation setup, and community discussion.
4. Verify the license covers the intended use, including commercial terms.
5. Record the exact revision hash used, not just the name.

## Output

Artifact candidates with cards, licenses, metrics, and pinned revisions.

## Pitfalls

- Names move; revisions pin. Record the hash.
- Missing cards mean missing guarantees; treat accordingly.
