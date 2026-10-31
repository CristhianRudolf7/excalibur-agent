---
name: arxiv-search
description: Search and read arXiv papers, including methods, results and limitations sections.
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
      name_en: arXiv search
      name_es: Búsqueda en arXiv
      desc_en: Search and read arXiv papers, including methods, results and limitations sections.
      desc_es: Buscar y leer papers de arXiv.
---
# arXiv search

Use this skill for primary ML and science literature. Reading methods, results, and limitations — not just abstracts — separates claims from evidence.

## When to use

- A paper's claim matters to a decision.
- Surveying methods for a research task.
- Checking whether results replicate.

## Procedure

1. Search arXiv and select by relevance, date, and venue signal.
2. Read methods before results: data, setup, baselines, and ablations.
3. Extract results with their uncertainty and the authors' stated limits.
4. Check citations and follow-ups for refutations or extensions.
5. Cite with arXiv ID and version; never trust the abstract alone.

## Output

Paper briefs with methods, results, limits, and IDs.

## Pitfalls

- Abstracts advertise; methods decide.
- Version matters; cite the exact revision read.
