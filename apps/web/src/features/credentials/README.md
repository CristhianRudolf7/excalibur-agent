# Secure credential forms

Credential requests render inside the conversation through `CredentialRequests`.
The event subscription handles `credentials.requested` and
`credentials.resolved`; initial loading, window focus and socket reconnects
refresh request metadata through `credentials.pending`.

`CredentialCard` keeps values in uncontrolled form inputs. It calls
`credentials.submit` directly, outside TanStack Query mutations and chat state.
Transient RPC parameter objects are cleared after use. Successful submission,
cancellation, external resolution and unmounting clear input nodes, including
detached nodes. A locked vault keeps the pending form available while a separate
`vault.unlock` form accepts the passphrase and retries submission. Errors shown
by these forms are fixed translations, so daemon error messages cannot echo a
submitted value. JavaScript strings and browser internals cannot be reliably
zeroed; the client clears references and DOM values without claiming guaranteed
memory erasure.

Password and API key fields use `new-password` autocomplete, username and email
fields disable autocomplete, and OTP fields use `one-time-code`. The forms do
not ask the browser to fill an existing password for the web app's origin. OTP
requests disable remembering. The browser or an installed password manager may
apply its own autofill policy.

Settings → Passwords lists site metadata and confirms deletion before calling
`vault.delete` with `confirmed: true`. Entries with expiration timestamps are
excluded from the remembered-site list so temporary credentials do not appear
as persistent entries. No RPC for revealing secret values is exposed here.

## Backend integration

Wire types live in `src/lib/rpc/credentials.ts`; the credential request schemas
reference the generated definitions in `packages/contracts/methods.json` and
`events.json` through `credentials.json`. `internal/api` owns method and event
registration; `TestCredentialContracts` regenerates and checks the JSON exports.

Two additive conventions support the complete UI:

- `vault.list_metadata` returns `{entries, locked}`. Older daemons returning only
  `{entries}` show an unknown status until the user explicitly locks or unlocks
  the vault; a successful metadata listing does not imply an unlocked vault.
- `credentials.pending` with `conversation_id: ""` lists pending requests across
  conversations. This lets `/?credential=<request_id>` locate a request created
  in Telegram. A link can instead include `&conversation=<conversation_id>` to
  avoid global discovery. Only request and conversation identifiers enter the
  URL. Linked conversation history and subsequent messages use that explicit
  conversation, with a separate chat-state key from the normal web conversation.

These conventions are implemented by the integrated daemon.
The browser tests mock both RPC replies and the event stream; they do not call
external services or require a running daemon.
