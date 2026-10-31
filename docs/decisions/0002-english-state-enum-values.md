# 0002 — English enum values for states and scopes

Date: 2026-10-31. Status: accepted.

## What

The architecture document labels several state machines in Spanish
(`lista`, `ejecutando`, `pausada`, ...). The repository is English-only
by agent instruction, so the canonical stored values are English:

| Document (Spanish) | Code (English) |
|---|---|
| `creada` | `created` |
| `lista` | `queued` |
| `ejecutando` | `running` |
| `esperando` (+ motivo) | `waiting` (+ reason) |
| `verificando` | `verifying` |
| `pausada` (+ motivo) | `paused` (+ reason) |
| `bloqueada` (+ motivo) | `blocked` (+ reason) |
| `recuperando` | `recovering` |
| `completada` | `completed` |
| `completada_con_pendientes` | `completed_with_pending` |
| `fallida` | `failed` |
| `cancelada` | `canceled` |
| `abierta`, `en_curso` (tasks) | `open`, `in_progress` |
| `cumplido`, `fallido`, `no_verificado`, `no_aplicable` | `satisfied`, `failed`, `unverified`, `not_applicable` |
| wait motives | `tool`, `user`, `subagents`, `agenda`, `capability`, `lease` |
| pause motives | `user`, `parent`, `resource`, `budget`, `recovery`, `safe_mode` |

## Why

The English-only rule exists so that humans, models and external tools
share one vocabulary. Keeping Spanish enum values in a database that
English code, logs and APIs describe would split that vocabulary at the
most load-bearing layer. The transition tables and semantics are
unchanged; only the labels differ.

## Alternative

Store the Spanish labels verbatim. Rejected: it would violate the
repository language rule and force every consumer to map both ways.

## Scope data

Scope kinds (`general`, `project`, `conversation`, `task`, `app`) are
already English in the document and are kept as-is.
