# 0105: Daemon-only Google tools

`internal/googleapi` accepts the shared OAuth `TokenProvider` contract and
registers `gmail`, `drive`, and `docs` through the existing `tools.Registry`
interface. `ToolOptions` injects REST endpoints, an HTTP client, a minimal blob
store interface already satisfied by gateway stores, and an explicit confirmation
policy override. `daemon.Services.Google` is the sole new shared service field;
it is additive. OAuth owns that field's initialization and credential lifecycle.

Chat registers these tools after building the embedded engine, marks them
as deferred, and adds only their names to its allowlist. Definitions use the
`default` profile exclusively. Disconnected accounts retain identical definitions
and return an actionable connection error. This keeps the chat tool list stable
without adding Google operations to embedded or eval surfaces.

Consequential actions require explicit confirmation by default. External writes
are non-idempotent and are never automatically retried by the HTTP client. HTTP
error bodies and provider errors are suppressed to prevent credential disclosure.
Downloads use the daemon's existing content-addressed blob endpoints, avoiding
new HTTP routes or changes to the library contracts.
