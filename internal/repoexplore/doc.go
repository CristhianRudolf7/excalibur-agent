// Package repoexplore implements the optional exploration-before-acting
// phase for small models (agent-quality workstream).
//
// At task start a lightweight explorer inspects the working directory
// and the environment relevant to the instruction (project layout,
// build files, entry points, tests, configs, versions, existing data
// files and their formats) and returns a compact, structured brief
// (at most ~60 lines: what exists, where, how to build/run/test,
// likely pitfalls) that is pinned into the main agent's working
// notes (L3 task layer, so the L0-L2 cache prefix never churns).
//
// Design notes (Sections 7.3, 11.1, 15.2-15.4):
//
//   - Read-only: only directory listings, stat and bounded file reads.
//     No writes, no subprocess execution, no network. This is the
//     deterministic equivalent of running the Section 15.2 explorer
//     profile (read-only tools) with a small budget and a fresh
//     context: it never touches the main run history, only the
//     returned brief is pinned.
//   - Small budget: bounded walk (max files, max depth), bounded
//     per-file reads, overall context timeout. Collection never fails
//     the run; every probe degrades to absence.
//   - Fresh context: the collector reads the filesystem directly and
//     returns a self-contained brief. No model call is spent.
//   - Stable prefix: the brief carries no timestamps and is collected
//     once per run; every snapshot of the run renders the same bytes.
//   - Instruction relevance: path-like tokens in the instruction are
//     resolved against the workdir and reported as exists/missing, so
//     the brief covers what the task actually names.
//
// The mechanism is gated by EXCALIBUR_EXPLORE (default off until
// measured). See ConfigFromEnv and docs/development/agent-quality.md.
package repoexplore
