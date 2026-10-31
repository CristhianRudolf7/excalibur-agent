# 0039: Agent loop quality levers without touching shared contracts

Date: 2026-10-31. Workstream: agent loop and prompting (W1).

## What

Working-method L0 block, per-model-family prompt variants, actionable
loop nudges with recovery strategies, pinned working-notes block, and
a thin-work finish guard, each behind a config switch defaulting ON
(`docs/development/agent-quality.md`, W1 section). No changes to
`internal/providers`, the model gateway wire protocol,
`internal/gateway`, `internal/api`, `packages/contracts`,
`integrations`, the MCP/ACP servers, or the bench Harbor invocation.

## Why

Read-only trial evidence (57 trials, ~26% pass): every failure ends
as `completed` with shallow work (~14 tool calls); 17% of `exec`
calls fail on invented `target` values; unwired tools are retried
blindly; the run carries almost no working method and the unwired
gate passes trivially. Research (LangChain harness engineering,
Meta-Harness) shows the same failure modes yield to prompt method,
pre-completion verification reminders, loop nudges, and context
engineering with the model fixed.

## Alternatives considered

- Hard-coding correct `exec` parameter values into the prompt:
  rejected. The tool surface belongs to a sibling workstream; the
  prompt teaches the general rule (copy allowed values from the tool
  definition or the error message, never invent enums) and an
  `ErrorHint` hook carries precise per-tool hints later.
- Blocking premature completion in the engine by default: rejected.
  The gate belongs to the verification workstream. The default guard
  only fires when no gate is wired at all and tools were attempted
  without a single success; runs that never attempted a tool, and
  any gate, gate input, or hook, stand it down. This keeps trivial
  no-tool answers (and the exec retry contract tests) on exactly
  their old call counts.
- Editing `internal/cli/exec_engine.go` to pass the resolved model
  id into `DefaultBuilder.ModelID`: deferred. It is a one-line
  additive follow-up for whoever owns that call site; the GPT
  default (the reference family) is active without it, and the
  mechanism plus precedence are tested.
- New dependencies: none. Standard library only.
