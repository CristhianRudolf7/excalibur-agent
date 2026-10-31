// Package journal implements the durable journal: the append-only log of
// every run that lets work resume after crashes or restarts without
// duplicating effects.
//
// Architecture reference: Section 17.1 (durable journal), 17.2 (effect
// classes), 17.3 (boot reconciliation). Implemented in milestone H3.
package journal
