// Package attempts implements best-of-N with self-selection (Section
// 18.2): when enabled, one task fans out to N independent attempts in
// isolated working-directory copies, each attempt runs the normal loop
// up to its verification phase, and a selection step keeps only the
// attempt whose own executable checks pass.
//
// The mechanism is budget-aware (each attempt gets the full parent
// steps and cost budget by default so it runs to its own verification,
// under a shared wall-clock deadline) and never consults the benchmark
// verifier: selection uses only the attempts' own executable evidence
// (completion state, successful checks, tool errors) plus a cheap model
// comparison of evidence as a final tie-break. Per-attempt outcomes are
// recorded for standard evaluation and auditing (Section 23.2,
// Appendix F); the outer trial records merge every attempt's model and
// tool calls tagged by attempt.
//
// The switch is EXCALIBUR_ATTEMPTS=<N> (1 means off, the default until
// measured). Concurrency is bounded by the container CPUs.
// EXCALIBUR_ATTEMPTS_FULL_BUDGET=0 restores the legacy step/budget
// split; EXCALIBUR_ATTEMPTS_MERGE_RECORDS=0 keeps the legacy
// outer-only records.
package attempts
