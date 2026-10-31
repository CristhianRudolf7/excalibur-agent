---
name: memory-curation
description: Decide what to keep and how to phrase it as an atomic fact with source, validity and expiry,
  and how to treat duplicates and contradictions. Guides the extractor and the daily review (10.5-10.6).
metadata:
  excalibur:
    group: memory
    version: 1.0.0
    platforms:
    - linux
    tools:
    - memory_write
    - memory_propose
    display:
      name_en: Memory curation
      name_es: Curaduría de memoria
      desc_en: Decide what to keep and how to phrase it as an atomic fact with source, validity and expiry,
        and how to treat duplicates and contradictions. Guides the extractor and the daily review (10.5-10.6).
      desc_es: Decidir qué guardar, cómo enunciarlo y cómo tratar duplicados.
---
# Memory curation

Use this skill when turning conversation into durable memory. Curation keeps the store trustworthy: atomic facts, sourced, with expiry, where contradictions are resolved explicitly instead of accumulating.

## When to use

- Extracting candidate facts from a conversation.
- Running or reviewing the daily consolidation.
- Duplicates or contradictions appear in recalls.

## Procedure

1. Keep only facts that change future decisions; drop trivia and transients.
2. Phrase each as one atomic statement with source, validity window, and expiry.
3. On duplicates, merge into the canonical fact; on contradiction, keep both with states and resolve by evidence.
4. Propose uncertain facts as candidates for consolidation, never as settled memory.
5. Respect the notebook rewrite guard: reject rewrites deleting over 25 percent.

## Output

Curated facts with source, validity, and expiry; duplicates merged, contradictions resolved or flagged.

## Pitfalls

- Atomic means one claim per fact; conjunctions rot.
- Never overwrite a contradiction; resolve it with evidence.
