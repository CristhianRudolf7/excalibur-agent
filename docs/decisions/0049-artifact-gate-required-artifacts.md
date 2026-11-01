# 0049 — Required-artifact completion gate

Date: 2026-11-01. Workstream: required artifacts
(`internal/tasks/artifacts.go`, engine closing seam
`internal/engine/verify_hooks.go`, `internal/cli/exec_engine.go`
workdir wiring).

## What

A deterministic gate on the finish path that refuses `completed`
while any required artifact from the literal instruction is
missing/empty/not executable as stated, or while background sessions
the agent started are still running without an intentionally
persistent, verified reason. It returns the agent to work with a
concise checklist (bounded by the Section 7.3 gate attempts, then the
honest `completed_with_pending` state). Switch
`EXCALIBUR_ARTIFACT_GATE=on|off` (default on).

## Why

Read-only triage of 44 failed evaluation trials (all-on run,
2026-10-31): tool errors fell to ~0, but failures are dominated by
discipline and verification problems. 12 failures name
files/paths/outputs (backticked absolute paths, "put the function in
a file called X", "install to Y") that the agent never created, and 2
more declared done while a background server/process was still
starting. The Section 7.5 gate passes vacuously where no criteria
are recorded, and the history contrast only checks mentions, never
the filesystem.

## Alternatives considered

- Filesystem checks inside the self-verification drafts
  (`selfverify.go`): rejected. Draft coverage is history-text based
  and shared with the daemon path; the filesystem check needs the
  run workdir, which only the engine seam owns.
- Executing verification commands in the gate: rejected. The gate
  never executes anything (Section 18.1 keeps execution in the
  agent's hands); it reads the run workdir and the agent's own
  successful history, and the agent runs the checks on the retry.
- Port dialing in the gate: rejected as flaky. Ports/URLs are
  verified through the agent's successful tool history (the agent
  must have exercised them with curl, a client install or an
  `exec_io` poll), which is deterministic and record-backed.
- Blocking every live session unconditionally: rejected. A task that
  asks for a persistent service (server, port, localhost URL) keeps
  its verified session; the gate only blocks unverified sessions
  and verified-but-undeclared ones.

## Change

New `internal/tasks/artifacts.go` (stdlib plus the existing
package-private sentence/regex helpers only): requirement
extraction (absolute/relative paths, file names, executables,
ports, URLs, output files with header expectations; example prose,
pattern fragments and negated mentions never count), filesystem
checks (existence, non-empty outputs, execute bit) with workdir
resolution, service checks against successful history, and live
background-session detection with the persistent-service exception.
Engine seam merges the pendings into the gate report before the
honest-state logic; `exec` sets `Run.Workdir` to the project
directory.
