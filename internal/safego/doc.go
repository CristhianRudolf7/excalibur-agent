// Package safego is the single place allowed to launch long-lived
// goroutines. In Go an unrecovered panic in any goroutine kills the
// process, so every turn, run and adapter goes through Go, which
// installs recover() at its boundary, logs the panic with its stack
// and marks the owner as failed instead of crashing the daemon.
//
// A writer-goroutine failure is critical: the daemon exits cleanly and
// lets the supervisor restart it (Section 25.2).
//
// Launching long-lived goroutines with bare go statements outside this
// package is forbidden and flagged by scripts/check-no-bare-go.sh (wired
// into make lint).
//
// Architecture reference: Sections 3.2 and 5.3. Implemented in wave 0.
package safego
