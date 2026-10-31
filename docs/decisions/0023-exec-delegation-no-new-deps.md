# 0023 — Exec, delegation and benchmark wiring without new dependencies

Date: 2026-10-31. Status: accepted.

## What

Milestone w3-exec-eval (H4 closure + H8 delegation) wires the
end-to-end surface in `internal/cli` with the standard library only:
profiles (`default`/`delegate`/`eval`), `excalibur exec` with the
Section 6.5 exit codes, the ten `excalibur task` orders on a
file-backed store, the 7-tool `excalibur mcp` stdio server,
`excalibur mcp install` for four clients, the `excalibur-delegate`
Agent Skill, `excalibur acp`, and the Harbor/bench plumbing that
already existed (`scripts/bench/build.sh`, `run.sh`,
`integrations/harbor/`). `go.mod` is unchanged.

## Choices and alternatives

1. **MCP without the official SDK.** The architecture names
   `github.com/modelcontextprotocol/go-sdk`, but the delegation
   server only needs the stdio subset (initialize, tools/list,
   tools/call, progress notifications). A hand-rolled newline-delimited
   JSON-RPC 2.0 server implements exactly the Section 3.6 surface with
   no new dependency; adopting the SDK stays possible without
   changing the tool contracts.
2. **Exec runs an embedded deterministic step, not the full agent
   loop.** `RunExec` writes the Appendix F records (profile + hashes,
   ATIF, events, metrics.json, result) and reaches every Section 6.5
   terminal state, but the work itself is a deterministic stub: live
   model execution behind the providers gateway arrives in a later
   milestone without changing this contract. Tests pin the exit-code
   table through the same entry point the CLI uses.
3. **Question protocol is explicit.** Delegated tasks park with
   `{{ask: text}}` markers in the petition instead of model-generated
   questions, so the wait/reply/never_ask rows are testable without a
   model. Real question generation belongs to the engine/master.
4. **Background tasks are child processes; adoption is file-based.**
   `task start` spawns a detached `__run-task` child; the daemon
   adopts the same `<project>/.excalibur/tasks/<id>/` directory later
   (17.3). Daemon API routing is not implemented here.
5. **ACP is the evaluation subset.** initialize, session/new,
   session/prompt and session/cancel are implemented; permission
   modes, session-update streaming and Harbor usage conventions are
   future work.
6. **Client versions are best-effort.** The install fragments record
   the checked format date (2026-10) in their headers; offline
   verification against live client releases was not possible.

## Deferred rows (other milestones own them)

- Section 6.7 preanalysis, fast-lane, triage-counting and voice rows
  (reception/master, H9).
- The Section 3.7 `files.list` row (workspace, H7) and daemon-mode
  delegation rows (H9): status/wait/result read the same files the
  daemon will adopt, so the contract is stable.
- Engine-side eval gating (task_db project-op rejection inside the
  tool runtime): `cli.EvalToolAllowed` pins the table and the tests;
  the engine clamps real calls when it lands.
