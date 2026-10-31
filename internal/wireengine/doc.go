// Package wireengine is the cross-milestone wiring of the execution
// path (wave 4): it replaces the fakes, seams and stubs left by the
// milestone waves with the real implementations, faithful to the
// architecture document.
//
// What it wires, and where the behavior is specified:
//
//   - engine <-> orchestrator: internal/engine.EngineRunner implements
//     orchestrator.Runner (interrupt, directive, snapshot); the resume
//     helper replays the journal on resume (Sections 7.2, 15.8, 17.3).
//   - workspace leases, worktrees and shared edit history inside the
//     edit tools (Sections 13.6, 13.7, 16.2, 16.4).
//   - LSP diagnostics after edits and code-graph impacted-test
//     selection in check selection (Sections 10.10, 13.8, 18.2).
//   - deferred tools reachable through search_tools/call_tool without
//     breaking the cache prefix (Section 13.3).
//   - skill tools registered in the catalog (Section 12.4).
//   - vault secret resolution in providers and tool processes plus
//     redaction in all outputs (Section 20.1).
//   - the eval embedded profile using all of the above, exercised
//     end-to-end through excalibur exec with a scripted fake provider.
//
// Fakes stay only in unit tests. This package is the production
// composition root for embedded (non-daemon) execution.
package wireengine
