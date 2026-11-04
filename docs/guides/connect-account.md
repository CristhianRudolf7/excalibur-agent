# Connect your ChatGPT/Codex account

Open **Settings → Connections → ChatGPT / Codex account**. The card is available
in English and Spanish. Use an account with a plan that includes Codex. Usage
counts against your ChatGPT plan limits.

## Sign in on the same machine

1. Select **Connect**.
2. Select **Open Sign in with ChatGPT**. The sign-in page opens in another tab.
3. Complete sign-in. The browser returns to a temporary loopback callback at
   `http://localhost:1455/auth/callback`.
4. Return to Settings. The card refreshes automatically and shows your email,
   plan, and credential source when those details are available.

The callback listener is bound only to loopback and lasts at most ten minutes.
A new connection attempt replaces the previous attempt. If port 1455 is already
occupied, use the paste flow below; Excalibur does not stop the other application.

## Sign in to a remote or headless server

1. Open the remote server's Settings card and select **Connect**.
2. Open the sign-in link on any device with a browser.
3. After authorizing, copy the complete final `http://localhost:1455/auth/callback`
   URL from the address bar. On a different machine the callback page may fail to
   load; the URL still contains the completion information.
4. Paste that URL into **Final redirect URL or code** in the original Settings
   card and select **Complete connection**. You may paste just the authorization
   code from the URL instead; keep the original card open so it retains the
   matching state.

No callback port forwarding is required for the paste flow. The URL and code
are temporary sign-in credentials: paste them only into the card that started
this attempt. After ten minutes, an exchange error, or a daemon restart, select
**Connect** to obtain a new link. The Settings card clears the pasted input after
completion and never displays account tokens.

## Status and disconnect

The card shows **Connected as**, **Plan**, and **Source**. Email and plan may be
unavailable even when the account is connected. The source identifies the
credential store selected by the account service:

- **Excalibur**: a connection saved by this application.
- **Codex CLI**: an existing CLI connection supplied by the shared token store.
- **Environment-configured file**: a credential file selected by the environment.

Select **Disconnect** to cancel a pending sign-in and remove Excalibur's own
connection. This does not sign out of your browser, revoke the account's
sessions, or remove the CLI or environment credential file. If another source
remains available, the card continues to show that connection. Sign out at that
source to stop using it.

Excalibur saves successful browser sign-ins in
`<data-dir>/codexlink/auth.json`, using the CLI-compatible account format and
private file permissions. It does not save new sign-ins to `~/.codex/auth.json`.
The token-store service owns credential discovery and refresh; agent model
routing is configured separately from this card.

## Protocol and RPC integration

The browser flow uses the public CLI client, PKCE S256, a fresh random state per
attempt, and a form-encoded authorization-code exchange. The daemon rejects
missing, mismatched, expired, superseded, and replayed callback state. A complete
pasted URL must match the registered callback, with exactly one code and state.
The authorization code is consumed once, including when token exchange fails.
Errors contain fixed messages rather than issuer responses or credential data.

The gateway exposes these four methods:

| Method                 | Parameters      | Result                                             |
| ---------------------- | --------------- | -------------------------------------------------- |
| `codex.status`         | `{}`            | Account metadata                                   |
| `codex.login.start`    | `{}`            | `{auth_url}`                                       |
| `codex.login.complete` | `{code, state}` | Account metadata                                   |
| `codex.logout`         | `{}`            | Account metadata after removing the own connection |

For `codex.login.complete`, `code` may contain the complete callback URL. Direct
RPC callers may omit `state` only when it is supplied by that URL. Bare codes
always require the original state. Results expose only `connected`, `source`,
`account_email`, `plan_type`, and optional `expires_at`.

The implementation pins the browser protocol from CLI release
[`rust-v0.101.0`, login server](https://github.com/openai/codex/blob/rust-v0.101.0/codex-rs/login/src/server.rs)
and its [public client identifier](https://github.com/openai/codex/blob/rust-v0.101.0/codex-rs/core/src/auth.rs).
That release matches the required localhost callback and identity/offline scopes.
For supported authentication options and account access, see the
[official authentication documentation](https://learn.chatgpt.com/docs/auth?surface=cli).
