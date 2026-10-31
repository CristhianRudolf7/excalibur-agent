---
name: performance-profiling
description: "Find the real bottleneck with time, perf, language profilers and flame graphs before optimizing."
metadata:
  excalibur:
    group: debugging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Performance profiling
      name_es: Perfilado de rendimiento
      desc_en: "Find the real bottleneck with time, perf, language profilers and flame graphs before optimizing."
      desc_es: "Encontrar el cuello de botella real con profilers antes de optimizar."
---
# Performance profiling

Use this skill when something is too slow. Profile first, guess never: one measurement of where time goes beats ten plausible theories.

## When to use

- A program, test suite or pipeline misses its time budget.
- Before any optimization attempt (pairs with `performance-optimization`).
- Deciding between CPU, memory, I/O or lock contention as the cause.

## Procedure

1. Establish the baseline: `time cmd` (real/user/sys split: high sys = syscalls, high real-vs-user = waiting) and `/usr/bin/time -v cmd` for peak memory.
2. System-wide CPU: `perf record -g -- cmd` then `perf report` (needs `perf` and symbols; `-g` keeps call graphs); for quick stacks use `perf top`.
3. Language profilers for app code: Python `python -m cProfile -o p.stats script.py` + `pstats`/`snakeviz`, Go `go test -bench=. -cpuprofile=cpu.out` + `go tool pprof`, Java async-profiler/JFR, Node `--cpu-prof`.
4. Separate I/O from CPU: `iostat -x 1`, `iotop`, strace sampling (`strace -c -f -p <pid>` counts syscalls) — disk/network waits look like CPU idleness.
5. Fix the top frame only, then re-profile: one bottleneck at a time, same workload, same machine.

## Pitfalls

- Profiling an unoptimized debug build measures the wrong program; profile the build flavor you ship (with symbols kept).
- Microbenchmarks without warmup and statistics lie; report medians over repeats, not single runs.
- Sampling needs load: profile under realistic concurrency, not an idle single request.

## Verification

- The profile attributes >50% of the budget to the fixed site before the change and the re-profile shows it gone with wall-time improved.
