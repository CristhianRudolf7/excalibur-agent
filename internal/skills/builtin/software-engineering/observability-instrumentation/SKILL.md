---
name: observability-instrumentation
description: Instrument applications with logs, metrics, traces and alerts.
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - metrics_query
    display:
      name_en: Observability instrumentation
      name_es: Instrumentación de observabilidad
      desc_en: Instrument applications with logs, metrics, traces and alerts.
      desc_es: Instrumentar con registros, métricas, trazas y alertas.
---
# Observability instrumentation

Use this skill so production stops being a black box. Structured logs, key metrics, traces across boundaries, and alerts on symptoms let you find and prove issues instead of guessing.

## When to use

- Operating a service without visibility.
- Incidents take too long to diagnose.
- Adding a feature whose behavior must be watched.

## Procedure

1. Log structured events with correlation IDs at every boundary.
2. Track the golden signals for the service: latency, traffic, errors, saturation.
3. Trace requests across components so a slow call names its culprit.
4. Alert on symptoms users feel, with runbook links; keep noise below the ignore threshold.
5. Verify the instrumentation by triggering and finding a canary event.

## Output

Logs, metrics, traces, and alerts that locate issues in minutes, verified with a canary.

## Pitfalls

- Alert on symptoms, not on every internal blip.
- Unverified instrumentation is decoration; test it.
