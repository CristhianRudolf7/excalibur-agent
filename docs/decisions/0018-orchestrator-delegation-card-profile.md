# 0018 — Orchestrator: explicit profile on the delegation card

Date: 2026-10-31. Status: accepted.

## What

`orchestrator.DelegationCard` carries an explicit `Profile` field
naming the Section 15.2 role profile of the child. The Section 15.4
table implies the profile through the toolset ("tools of the child
profile"), but the Section 15.2 identity (name, id, description,
profile, parent, level, scope) needs it named, so the card names it.
An empty profile falls back by level (coordinator below the master or
a root, implementer below a parent). The toolset is still clamped to
the profile: the card may reduce it, never expand it (15.9).

## Why

Without a named profile the orchestrator cannot build the identity,
pick the model role, or apply the tool clamp deterministically. The
alternative (deriving the profile from the requested toolset) is
ambiguous: several profiles share subsets of tools, and the parent
would have to guess.

## Alternative

A separate `spawn(profile, …)` argument outside the card. Rejected:
the profile belongs to the structured handover the child is built
from, and keeping one struct keeps the `subagent` tool schema and the
journal `run_started` payload uniform.
