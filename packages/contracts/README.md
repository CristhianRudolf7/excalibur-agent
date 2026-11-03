# Contracts

Shared wire contracts for non-Go clients (web UI, channel adapters,
external tools). Source of truth is `internal/api`; the JSON documents
here are an export of its registries.

- `conversation-context.json`: optional per-message app context for
  `conversation.send`, including classroom course and topic ids.
- `jsonrpc.json`: JSON Schema (2020-12) for the JSON-RPC 2.0 envelope.
- `methods.json`: every method name, family and summary. Must match
  `api.Methods` exactly; `TestContractsParity` enforces this.
- `automations.json`: scheduled automation, graph, version and run wire schemas.
- `events.json`: every typed event and its payload shape. Must match
  `api.EventTypes` exactly. Its definitions also include `ConversationMessage`,
  `FileChange`, and `FileHunk`: optional assistant `changes` in both live
  message events and conversation history, bounded to 50 files and 40 hunks
  per file.

Field shapes for params, results and payloads live in `internal/api`
(`methods.go`, `events.go`) and will gain full JSON Schemas as the
milestones implement them.
