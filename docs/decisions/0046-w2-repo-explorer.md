# 0046 — W2 repo explorer: deterministic brief instead of model subagent

Status: accepted.

## Context

The task asks for an optional exploration phase: at task start a
lightweight explorer (subagent profile with read-only tools, small
budget, fresh context) inspects the working directory and environment
relevant to the instruction and returns a compact structured brief
(<=~60 lines) pinned into the main agent's working notes, with switch
`EXCALIBUR_EXPLORE=on|off` (default off) and a stable cache prefix
(brief in the task layer).

The architecture (Section 15.2) defines `explorer` as a read-only
subagent profile that returns a summary without loading the parent
context. A literal implementation would spawn a subagent (model call)
with the explorer toolset at task start.

## Decision

Implement the explorer as a deterministic, harness-side collector
(`internal/repoexplore`, stdlib only, no model call) that honours the
same contract:

- read-only (directory listings, stat, bounded file reads; no writes,
  no subprocess, no network) — the equivalent of the explorer
  profile's toolset;
- small budget (3s, 120 files, depth 3, 8KB per read);
- fresh context (filesystem read, no main-history pollution);
- structured brief (<=60 lines: layout, build, entry, tests,
  configs, data, versions, instruction paths, pitfalls) pinned into
  L3 working notes as one note slot, byte-stable across snapshots.

## Why

- Zero extra model calls and zero gateway changes (connectors are
  frozen); a model explorer would cost latency, tokens and a new
  failure mode on the cheapest reference model (`gpt-6-luna`, low
  effort).
- Byte-stable output: sorted, capped, no timestamps, collected once
  per run, so the L0-L2 cache prefix never churns. A model summary
  cannot guarantee this.
- Measurable: the `EXCALIBUR_EXPLORE` on/off ablation compares the
  same deterministic bytes; a model explorer would confound harness
  and model variance.
- Consistent with the W1 env-snapshot precedent (deterministic
  onboarding context in a stable layer).

## Alternative considered

Spawn a real `explorer`-profile subagent via the orchestrator at task
start and pin its report. Rejected for now: extra model calls on the
critical path, non-deterministic bytes (cache churn), and a larger
blast radius (orchestrator, budgets, mailbox). The deterministic
record shape (`explore.json`, `explore_brief` event, L3 note) leaves
room to layer a model-refine pass later without changing the switch.

## Consequences

- `EXCALIBUR_EXPLORE` defaults to off until measured.
- No changes to providers, gateway, api, contracts, integrations,
  MCP/ACP, or bench invocation.
- Tests pin the brief shape, the cap, determinism, and the L3
  stability; the live `excalibur exec` check confirms the mechanism
  is active in records.
