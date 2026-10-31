// Package engine implements the single agent loop (one engine for master,
// parent and child subagents, distinguished by role profile), the
// completion gate and the loop detector.
//
// Architecture reference: Section 7 (agent cycle), Section 18
// (verification, loops and limits). Implemented in milestones H3 (step
// loop) and H5 (gate, loop detector, limits).
package engine
