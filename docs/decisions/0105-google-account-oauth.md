# 0105: Daemon Google account connection

Add the four `google.*` RPC methods and safe account status types to the shared
API contract. Keep `internal/googleapi.TokenProvider` limited to access-token
resolution and account metadata; daemon services expose it as `Google`.
OAuth-specific operational details use a separate status type. Tokens and client
secrets never appear in RPC results.

The public proxy forwards `/rpc` but does not forward OAuth paths. The web SPA
therefore handles `/oauth/google/callback` and submits code and state through
`google.connect.complete`. The daemon stores pending state and PKCE verifiers
for ten minutes, consumes each state once, and serializes token refresh with
account changes. Token storage prefers the unlocked vault and falls back to a
private file with a visible warning. Eval and embedded tool wiring are unchanged.
