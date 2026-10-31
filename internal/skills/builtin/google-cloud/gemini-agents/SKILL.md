---
name: gemini-agents
description: Create and deploy AI agents with Gemini and other models on Google's platform.
metadata:
  excalibur:
    group: google-cloud
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - web_search
    display:
      name_en: Gemini agents
      name_es: Agentes con Gemini
      desc_en: Create and deploy AI agents with Gemini and other models on Google's platform.
      desc_es: Crear y desplegar agentes con Gemini en Google.
---
# Gemini agents

Use this skill for agent work on Google's platform. Model choice, grounding, tool wiring, and deployment configuration decide whether the agent helps or hallucinates at scale.

## When to use

- Building an agent on Gemini or Vertex AI.
- Deploying model-backed features on Google Cloud.
- Grounding answers in enterprise data.

## Procedure

1. Pick the model and reasoning settings for the task's difficulty and budget.
2. Ground with enterprise data through grounding or retrieval, citing sources.
3. Wire tools with least-privilege service accounts and explicit schemas.
4. Evaluate on the task's own cases before deploying; record the scores.
5. Deploy with quotas, monitoring, and a rollback to the previous version.

## Output

A deployed Gemini agent with grounding, tools, evaluation scores, and monitoring.

## Pitfalls

- Ungrounded agents on enterprise data hallucinate confidently; ground everything.
- Deployments without evaluation scores are experiments on users.
