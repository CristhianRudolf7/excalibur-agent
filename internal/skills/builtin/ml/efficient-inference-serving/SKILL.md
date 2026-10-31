---
name: efficient-inference-serving
description: "Serve models cheaply and fast: quantization, batching, caching and load measurement."
metadata:
  excalibur:
    group: ml
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Efficient inference serving
      name_es: Servir inferencia eficientemente
      desc_en: "Serve models cheaply and fast: quantization, batching, caching and load measurement."
      desc_es: "Servir modelos barato y rápido: cuantización, batching y medición."
---
# Efficient inference serving

Use this skill to turn a working model into a service that survives real traffic. Measure first, then cut precision, batch work and cache repeats.

## When to use

- Latency too high or GPU costs too steep for the current setup.
- Choosing quantization, batching or a serving stack.
- Proving the service holds up under load before release.

## Procedure

1. Baseline: measure p50/p99 latency and throughput at concurrency 1, 8 and 32 with a load tool (`wrk`, `hey`, or a python loop); record GPU util and memory.
2. Quantize deliberately: dynamic/static INT8 or FP16 via the framework's toolkit; re-run YOUR eval metric — accept only quantization that preserves it.
3. Batch and cache: batch incoming requests with a small timeout window; cache deterministic repeats (same input → same output) in Redis or in-process LRU (see `redis-operations`).
4. Serve behind a real server: one model process per GPU, a reverse proxy in front (see `nginx-static-services`), health endpoint plus model-revision in the response header.
5. Guard the rollout: canary on a traffic slice, compare error rates and latency against the baseline, keep the previous artifact one command away.

## Pitfalls

- Benchmarking on an idle box lies; measure on production-shaped hardware with production-shaped inputs.
- Quantization without eval re-runs ships silent quality regressions; the metric gate is mandatory.
- Unbounded queues turn latency spikes into OOMs; bound queues and shed load with 429s instead.

## Verification

- p99 within budget at 2x expected peak, eval metric within the accepted delta, and rollback tested (previous artifact serves correctly).
