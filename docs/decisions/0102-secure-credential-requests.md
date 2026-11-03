# Secure credential requests

Section 20.1 requires credential values to enter through a dedicated secure call.
The additive `credentials.pending`, `credentials.submit` and `credentials.cancel`
methods and `credentials.requested` / `credentials.resolved` events carry request
metadata separately from chat. The local daemon has one principal; request IDs
select server-owned conversation and run metadata.

JSON-RPC retains numeric error codes. A locked vault uses the application error
code, message `vault_locked`, and `data.code = "vault_locked"` so clients can
unlock through the existing `vault.unlock` method before retrying submission.
The exported schemas are generated and checked by `TestCredentialContracts`.

The daemon retains at most eight pending requests per conversation and 64 in
total. Pending metadata survives a browser reload while the run waits; submitted
values are written directly into separate encrypted vault entries. Host-derived
bundle names include a request suffix, so failed submissions cannot overwrite
previous credentials. Only complete bundles with the exact browser-only policy
are reused. OTP fields always expire.

Ephemeral entries expire after one hour, are deleted when the turn ends, and
are swept on daemon shutdown and startup after an interrupted run. Mutable
submission buffers are cleared on every exit path. JSON decoding can leave
transient Go string copies, as described in Section 20.1.

Credential field references use `secret://<bundle>.<field>`. Browser resolution
maps the suffix to a separate vault entry (`<bundle>-<field>`) and retains the
existing host, tool and delegation checks. The daemon also checks the owning
run before dispatching an ephemeral reference. Login and OTP readiness keeps
`input_required` visible while allowing snapshots and form actions.

Secure login guidance is attached to daemon browser results and the assistant
prompt. The embedded browser skill belongs to the frozen eval catalog; keeping
that skill unchanged preserves the eval profile hash and benchmark surface.

For non-web origins (including Telegram), request creation emits a normal
assistant message containing only the instruction to open the secure web card.
`EXCALIBUR_WEB_BASE_URL` supplies the optional web link. URLs containing user
info, query parameters or fragments are rejected to avoid forwarding embedded
credentials. Without a suitable URL, the message says to open Excalibur web.
This checkout has no concrete Telegram transport; channel delivery uses the
existing assistant-message publication boundary. Delegated chat runs retain the
origin channel and clean up their own ephemeral credentials.

An explicit empty `credentials.pending.conversation_id` returns all pending
requests for the local principal. A nonempty identifier filters to that
conversation; missing or null identifiers are invalid. This supports request
links opened from another channel without changing conversation history routing.
Configured channel links include URL-encoded `credential` and `conversation`
parameters so the web can open the originating card directly.

`vault.list_metadata` adds a boolean `locked` alongside `entries`. Metadata
remains available while locked, so clients must use this explicit field rather
than infer lock state from a successful listing.
