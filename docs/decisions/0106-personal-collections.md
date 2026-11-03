# Personal collections contract

Personal collections add `collections.*`, `records.*`, and `collections.changed`
to the JSON-RPC contract. Existing contracts and tool prefixes remain unchanged.
The daemon owns a separate `collections.db` SQLite database; the harness database,
eval runtime, and embedded tool catalog do not initialize it.

Field IDs are stable SQL column identifiers. Display names are metadata. Required,
unique, type, range, and relation checks run in the engine and schema edits are
transactional. The last ten schema snapshots include records; whole collections
remain in trash for thirty days. Incoming relation references block deletion.
Schema restoration is destructive and requires tool confirmation.

The web agent owns TypeScript contract additions. Shared shapes and method names
follow the personal collections round contract. Validation errors use JSON-RPC
`-32602` with `data.errors` containing field/message pairs.
