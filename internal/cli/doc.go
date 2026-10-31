// Package cli implements the excalibur command-line commands as thin
// layers over the shared core: no agent or provider logic lives here.
//
// Architecture reference: Sections 3.5 and 3.6 (CLI and delegation
// server). The command surface skeleton ships in milestone H0
// (wave 0); full behavior arrives with milestones H4 (exec) and H8
// (task, mcp).
package cli
