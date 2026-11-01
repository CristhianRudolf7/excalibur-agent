// Package attempts implements best-of-N with self-selection (Section
// 18.2): when enabled, one task fans out to N independent attempts in
// isolated working-directory copies, each attempt runs the normal loop
// up to its verification phase, and a selection step keeps only the
// attempt whose own executable checks pass.
//
// The mechanism is budget-aware (each attempt receives a share of the
// task steps and cost budget under a shared wall-clock deadline) and
// never consults the benchmark verifier: selection uses only the
// attempts' own executable evidence (completion state, successful
// checks, tool errors) plus a cheap model comparison of evidence as a
// final tie-break. Per-attempt outcomes are recorded for standard
// evaluation and auditing (Section 23.2, Appendix F).
//
// The switch is EXCALIBUR_ATTEMPTS=<N> (1 means off, the default until
// measured). Concurrency is bounded by the container CPUs.
package attempts
