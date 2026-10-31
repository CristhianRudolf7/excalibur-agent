// Package envsnap builds the proactive environment snapshot of
// Section 13.5: OS and distribution, languages and versions,
// compilers, package managers, notable installed tools, network
// reachability, a bounded working-directory tree summary, running
// services and disk/RAM/CPU limits.
//
// The snapshot is collected once at task start and rendered once into
// the stable scope layer (L2), so the model stops spending tool calls
// discovering what is installed (the most common Terminal-Bench
// failure mode is calling a binary that is missing or not on PATH).
// Collection is bounded and best effort: every probe has a short
// timeout, failures record absence, and the whole pass never fails
// the run.
//
// The tool-surface task owns the exec tool itself; this package only
// exposes the EnvProvider interface so both sides share one snapshot
// shape without an import cycle (envsnap imports nothing but the
// standard library, x/sys and internal/safego).
package envsnap
