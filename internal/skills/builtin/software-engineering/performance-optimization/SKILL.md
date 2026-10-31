---
name: performance-optimization
description: Measure before and after and optimize performance with profiles, benchmarks and result comparison.
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
      name_en: Performance optimization
      name_es: Optimización de rendimiento
      desc_en: Measure before and after and optimize performance with profiles, benchmarks and result
        comparison.
      desc_es: Medir antes y después y optimizar con perfiles.
---
# Performance optimization

Use this skill when performance must improve. Profiles find the bottleneck, benchmarks prove the fix, and before/after numbers — not intuition — decide what ships.

## When to use

- A latency, throughput, or resource budget is missed.
- Users or metrics report slowness.
- Someone proposes an optimization 'that should help'.

## Procedure

1. Reproduce the workload and measure the baseline with a benchmark.
2. Profile to find where time or memory actually goes; fix that, not the guess.
3. Change one factor at a time and re-measure against the baseline.
4. Compare full results: mean, tail, and resource cost, not a single lucky run.
5. Keep the benchmark with the change as lasting evidence.

## Output

Measured before/after results with the profile, the change, and the kept benchmark.

## Pitfalls

- No optimization without a baseline measurement.
- Single-run numbers are noise; report distributions.
