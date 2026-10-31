---
name: rag-research
description: Research semantic search and retrieval-augmented generation.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - source_search
    - library_search
    - exec
    display:
      name_en: RAG research
      name_es: Investigación RAG
      desc_en: Research semantic search and retrieval-augmented generation.
      desc_es: Investigar búsqueda semántica y RAG.
---
# RAG research

Use this skill for retrieval-grounded generation. Chunking, ranking, and citation discipline decide whether RAG grounds answers or decorates hallucinations.

## When to use

- Building or improving a RAG system.
- Choosing chunking, embeddings, or rerankers.
- Answers need citations to sources.

## Procedure

1. Fix the evaluation: questions with known answers from the corpus.
2. Experiment with chunking and ranking one factor at a time.
3. Measure groundedness: claims supported per answer, not just retrieval recall.
4. Enforce citation to retrieved passages for load-bearing claims.
5. Test failure modes: missing, conflicting, and adversarial passages.

## Output

A tuned RAG with groundedness metrics and failure-mode tests.

## Pitfalls

- Recall without groundedness decorates; measure support per claim.
- Conflicting passages need resolution rules, not averaging.
