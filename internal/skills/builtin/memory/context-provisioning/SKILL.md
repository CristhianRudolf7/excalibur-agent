---
name: context-provisioning
description: 'Select the pertinent context for each agent and moment: which memories, documents, decisions
  and instructions to include in a card or a query.'
metadata:
  excalibur:
    group: memory
    version: 1.0.0
    platforms:
    - linux
    tools:
    - memory_search
    - library_search
    display:
      name_en: Context provisioning
      name_es: Aprovisionamiento de contexto
      desc_en: 'Select the pertinent context for each agent and moment: which memories, documents, decisions
        and instructions to include in a card or a query.'
      desc_es: Seleccionar el contexto pertinente para cada agente y momento.
---
# Context provisioning

Use this skill when assembling what an agent will see. Context is a budget: every included item must earn its tokens by changing the decision at hand.

## When to use

- Writing a delegation card or a retrieval query.
- A context is too large or full of irrelevant recalls.
- Deciding which documents travel with a task.

## Procedure

1. List the decision the agent must make and the minimum facts it needs.
2. Retrieve memories and documents filtered by scope; rank by recency, relevance, and reliability.
3. Include decisions and standing instructions that constrain this exact work.
4. Cut anything that does not change the decision; note what was excluded.
5. Size the bundle to the recipient's budget: child under 32K, parent under 48K.

## Output

A minimal sufficient context bundle with its selection rationale.

## Pitfalls

- More context is not better; irrelevant recalls actively hurt.
- Never provision across scopes to 'be helpful'.
