# 0044 — Tool surface for small models

Date: 2026-10-31. Workstream: tool surface for small models
(`internal/tools/**`, `internal/wireengine`, `internal/cli/exec_engine.go`).

## What

Every tool exposed in exec/eval works end to end; model-visible schemas
are simplified to what a small model needs locally; arguments are parsed
tolerantly with repair hints; results lead with exit code and duration
and name their retrieval path; shell-first guidance with strict file
edits kept.

## Why

Read-only trial evidence (57 trials, ~26% pass, all failures ending
`completed`): 17% of `exec` calls failed on model-filled `target`;
`load_skill`, `search_skills`, `subagent`, `task_db`, `ack_directive`
answered "not wired"; `search_tools` failed on `{}`; `call_tool` failed
on the `input` alias; `get_tool_output` received live `sess-N` ids.

## Alternatives considered

- Removing `env` from the exec schema entirely: rejected. The vault
  `secret://` resolution test (`vault_test.go`) executes through the
  executor with `env`, and eval keys arrive by environment; `env` stays
  valid but hidden with shell-syntax guidance.
- Executing spawned subagents in embedded scope: rejected. There is no
  engine loop per child in single-run scope; spawn records the child and
  carries a scope note telling the model to continue in this run.
- Teaching the engine a separate presentation schema: rejected as
  machinery. Stub definitions stay generic (`{"type":"object"}`) with
  tolerant providers; only true builtins carry real schemas.
- Wiring `send_message`/`ask_user`/`settings`/`reminders` in embedded
  scope: rejected. They are master/conversation subsystems owned by
  other workstreams (H9/H12); the eval surface never exposes them, and
  the enumeration test documents the one default-profile extra.

## Change

See `docs/development/agent-quality.md` (tool-surface section) for the
mechanisms and the six `EXCALIBUR_TOOL_*` ablation switches (all default
ON). Notable fixes: legacy `target`/`owner` ignored instead of erroring;
empty `search_tools`/`search_skills` list instead of failing; `call_tool`
accepts `input`; `get_tool_output` redirects `sess-N` refs to `exec_io`;
`get_tool_output` now shares the executor blob store (previously no
truncated ref resolved); explicit `"expected_fp": "*"` overwrites with a
note while genuine mismatches still reject.

## Verification

`TestExecSurfaceEveryToolWorks` enumerates every eval-profile core tool
and invokes it for real; `TestEmbeddedSurfaceWired`/`Disabled` pin the
wiring and its switch; `surface_test.go` pins aliases, hints, prefixes
and the wildcard. Full `make verify` gates apply.
