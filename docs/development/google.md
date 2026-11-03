# Google account connection

## OAuth agent

The daemon account provider lives in `internal/googleapi`. Its shared
`TokenProvider` contract is unchanged. `ConnectionStatus` adds safe configuration,
reconnect, storage-warning and missing-scope details for Settings.

The authorization-code flow uses S256 PKCE, random single-use state with a
10-minute lifetime, offline consent and the five agreed scopes. Refresh and
account changes are serialized. `invalid_grant` clears credentials and persists
a reconnect reason. Disconnect forgets local credentials even when revocation
fails. No HTTP callback route is added to the daemon.

Tokens use the unlocked daemon vault. A locked or unavailable vault uses an
atomically replaced 0600 `google-connection.json` in the daemon config directory.
The next status request migrates a fallback token into the unlocked vault.
A credential-free import marker prevents re-import after disconnect; a disconnect
tombstone takes precedence over stale vault credentials after a locked-vault
write. The original import file is never modified.

Configuration: `EXCALIBUR_GOOGLE_OAUTH_CLIENT`,
`EXCALIBUR_GOOGLE_REDIRECT_URI`, and `EXCALIBUR_GOOGLE_REFRESH_TTL` (Go duration,
default `168h`, `0` means unknown). The default client and initial import paths
are `~/.config/excalibur/credentials/google-oauth.json` and
`~/.config/excalibur/credentials/google-oauth-token.json`. Restart the daemon
after adding or changing the client configuration. Imported issue dates come
from the local ID token's `iat`, falling back to the token file modification time;
refreshing does not extend the recorded testing lifetime.

Provider tests use only `httptest` servers and fake credentials. They cover
PKCE, consent parameters, state replay/expiry, refresh concurrency, revocation,
invalid grants, restart persistence, one-time import, partial scopes, expiry,
unknown TTL, vault storage, locked fallback and migration.

The four additive `google.*` RPC methods are registered in the shared method
registry, JSON schema definitions, web wire types, gateway and daemon backend.
`Services.Google` and `BackendServices.Google` expose the agreed token provider.
An HTTP gateway test exercises status, start, complete, disconnect and malformed
callbacks, checks account transitions and asserts that access tokens stay private.
