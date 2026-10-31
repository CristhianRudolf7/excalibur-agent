// Run persistence behind an interface (Sections 4.4, 15.5, 15.6,
// Appendix A).
//
// The SQLite porters belong to the storage milestone (H2): this file
// defines the RunStore contract the orchestrator works against and
// ships a MemoryStore used by unit tests and the embedded eval
// profile. A SQL adapter can implement the same interface later
// without touching the logic. Every method is safe for concurrent
// use; the orchestrator additionally serializes control orders with
// its own mutex (Section 15.8).
package orchestrator
