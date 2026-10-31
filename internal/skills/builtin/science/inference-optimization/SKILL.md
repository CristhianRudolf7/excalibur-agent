---
name: inference-optimization
description: 'Optimize models and inference: quantization, pruning, distillation and speculative decoding.'
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - metrics_query
    display:
      name_en: Inference optimization
      name_es: Optimización de inferencia
      desc_en: 'Optimize models and inference: quantization, pruning, distillation and speculative decoding.'
      desc_es: 'Optimizar inferencia: cuantización, poda y destilación.'
---
# Inference optimization

Use this skill to serve models cheaper and faster. Measured quality-versus-cost trade-offs per technique keep speedups honest instead of silently degrading.

## When to use

- Serving costs or latency too high.
- Deploying to constrained hardware.
- Choosing between compression techniques.

## Procedure

1. Baseline quality and latency on the serving workload first.
2. Apply one technique at a time: quantize, prune, distill, or speculate.
3. Measure quality retention on the tasks that matter, plus tail latency.
4. Compare total cost per successful request, not per token alone.
5. Ship with the evaluation that justifies the trade-off.

## Output

An optimized serving setup with quality-retention evidence.

## Pitfalls

- Quality measured on the wrong benchmark hides degradation.
- Tail latency decides UX; report percentiles, not means.
