# Contracts

Shared wire contracts for non-Go clients (web UI, channel adapters,
external tools). Source of truth is `internal/api`; the JSON documents
here are an export of its registries.

- `jsonrpc.json`: JSON Schema (2020-12) for the JSON-RPC 2.0 envelope.
- `methods.json`: every method name, family and summary. Must match
  `api.Methods` exactly; `TestContractsParity` enforces this.
- `events.json`: every typed event and its payload shape. Must match
  `api.EventTypes` exactly.

Field shapes for params, results and payloads live in `internal/api`
(`methods.go`, `events.go`) and will gain full JSON Schemas as the
milestones implement them.
