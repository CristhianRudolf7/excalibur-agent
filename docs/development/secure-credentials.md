# Secure credential requests

The daemon's assistant surface implements secure credential requests for Sections
3.5, 6.4, 14.2 and 20.1. `request_credentials` is deferred and selected for login,
password and credential intent. It is absent from the embedded eval surface;
the existing surface and eval profile pins remain unchanged.

The tool accepts an exact host, a short purpose, requested fields and a default
remember preference. Username and password are the default fields. It waits up
to ten minutes without calling the model again. Each conversation can hold eight
pending requests; the daemon can hold 64. A web reload recovers metadata through
`credentials.pending`. Cancellation, run termination and timeout settle the wait
and emit `credentials.resolved`. Pending requests are intentionally tied to the
live run rather than restored after a daemon restart.

Clients submit values through `credentials.submit`, separately from chat. A
locked vault returns the numeric application error with message `vault_locked`
and `data.code = "vault_locked"`; the client can unlock and retry the same
request. Validation and lock errors leave the card pending. Request IDs bind to
the conversation and run recorded by the server. The daemon retains no submitted
values in its request registry.

Each field is a separate encrypted vault entry. Returned references have the
form `secret://<bundle>.<field>`; the browser resolver maps them to the vault's
entry naming convention. Policies allow only `browser_act` on the exact host.
Remembered fields can be reused without a new request. Ephemeral fields expire
after one hour and are deleted at run end, daemon shutdown or recovery after an
interrupted run. OTP is always ephemeral, including when other submitted fields
are remembered. Owned byte buffers and gateway request frames are cleared on successful and failed submits;
Go JSON decoding can still leave transient string copies.

Login and OTP detection retains `input_required`. Once the requested readiness
criterion passes, the browser permits a snapshot and form actions with refs.
Daemon guidance explains the secure form sequence. CAPTCHA and site access
blocks keep their existing behavior. Browser output and captures continue to
use the existing private-page masking and redaction.

Non-web origins receive an assistant message instructing them to open the secure
web card. Set `EXCALIBUR_WEB_BASE_URL` to include a link; otherwise the message
says to open Excalibur web. The instruction contains no site, purpose, field
values or refs. This repository has no concrete Telegram transport: the daemon
publishes the instruction through its existing assistant-message boundary for
channel adapters to forward.

Backend coverage includes:

- `TestCredentialWaitReloadCancelTimeout`, `TestCredentialValidationAndCapacity`
  and `TestCredentialGlobalCapacity`: wait lifecycle, reload metadata and limits.
- `TestCredentialSubmitRememberReuseAndHostPolicy`: zeroing, persistent reuse,
  exact-host and tool policies, and plaintext scans of durable vault files.
- `TestCredentialEphemeralCleanupAndOTP`, `TestCredentialLockedVaultCleanup` and
  `TestCredentialCancellationAndTimeoutRejectValues`: cleanup and rejected writes.
- `TestCredentialSubmitLockedValidationRetryAndZero` and
  `TestCredentialRPCGateway`: dedicated RPC validation, unlock retry and cancellation.
- `TestCredentialChatJournalHistoryAndModelPrivacy`: scans model requests, run
  journal, conversation history and events for submitted sentinel values.
- `TestCredentialBrowserGuidance`, `TestCredentialBrowserRunScope`,
  `TestCredentialChatActivation` and `TestBrowserCredentialFieldReferences`:
  guidance, deferred activation, run ownership and field reference resolution.
- `TestCredentialTelegramLinkOnly` and `TestCredentialChannelFallback`: link-only
  channel messages, web suppression and safe fallback.
- `TestCredentialContracts`: generated RPC/event exports and Go shape parity.
- `TestCredentialRPCTransportBuffers`: request-buffer clearing before response
  encoding for HTTP single calls, HTTP batches and WebSocket calls.

`TestCredentialBrowserSecureLogin` runs with the existing `browser` build tag and
`EXCALIBUR_BROWSER_TESTS=1`. It uses real Chrome and a local HTTP fixture, submits
through JSON-RPC, fills both returned refs, verifies authentication, scans tool
outputs/events/capture blobs and checks run-end deletion. It does not call a
model or a paid service.

Run the focused suites from the repository root:

```sh
export PATH=/usr/local/go/bin:$HOME/.local/opt/node/bin:$PATH
systemd-run --user --scope -p MemoryMax=4G -p MemorySwapMax=0 \
  nice -n 19 go test -p 2 -count=1 -race \
  ./internal/api ./internal/daemon ./internal/wireengine ./internal/browser
EXCALIBUR_BROWSER_TESTS=1 systemd-run --user --scope \
  -p MemoryMax=4G -p MemorySwapMax=0 nice -n 19 \
  go test -p 2 -count=1 -race -tags browser ./internal/daemon \
  -run TestCredentialBrowserSecureLogin
```

An explicit `EXCALIBUR_MASTER_TIMEOUT` still bounds the whole turn, including
credential waits. Without an override, browser and credential turns receive
enough time for the ten-minute secure form wait and subsequent model work.
`TestCredentialChatRespectsTurnDeadline` covers the operator deadline.

Integration coverage includes `TestCredentialDaemonRememberedFlow`. A fake
model calls the tool through real daemon chat wiring. A gateway WebSocket
receives the requested and resolved events, and HTTP JSON-RPC drives the locked
submit, unlock, retry with `remember: true`, and a second conversation turn.
The second call returns `stored` with the same refs and emits no new request.
The test scans both runs' journal entries, public history, events and model
requests for plaintext sentinel values and checks the retained exact-host policy.

The integrated web renders secure cards, unlocks and retries submissions,
recovers requests after reload, and provides Passwords settings for metadata,
locking and confirmed deletion. An empty conversation filter lists all pending
requests for the local user; configured channel links include both request and
conversation identifiers. Metadata listings include an explicit `locked` flag.
Credential Playwright specs use mocked RPC/events; the tagged Go browser test
uses real Chrome and a local login server.

Integration verification covers both merged branches and the daemon/web contract
adjustments. The full Go race suite passed under a 4 GiB memory limit with swap
disabled, low process priority and two package workers. The tagged real-Chrome
suites passed for `internal/browser` and `internal/daemon`. The integration,
contract, credential and gateway tests also passed in a focused race run.
The known gateway subscription timing test passed in isolation; it did not fail
in the integration branch's full run.

All five credential Playwright specs passed, covering desktop and mobile links,
unlock-and-retry, request/resolution events, metadata deletion and scrolling.
The six chat browser regressions passed. One settings chart visibility check
timed out in the combined run and passed unchanged when rerun alone. No timing
threshold was relaxed.

The web frozen-lockfile install, typecheck, lint, formatting check, all 504 unit
tests across 101 files, and production build passed. Existing React lint warnings
and the production bundle-size warning remain.

Formatting, vet, staticcheck, the goroutine check, and native, Darwin and Windows
builds passed. `TestEmbeddedSurfaceAndEvalProfileDoNotMove` verifies that the
eval profile remains
`4ecb6a58986660326b7d20d912918d479e7c72140c6e04ab38d83a23315fec28`;
the existing parity pins were not changed.

Daemon chat browser arguments are described explicitly to the model, including
`session_id`, tab creation, navigation actions, readiness criteria, and
snapshot-bound form actions. The shared browser manifest uses an unrestricted
object schema; exposing that schema directly caused the live model to omit tab
creation and repeatedly open sessions after `unknown tab` errors. Chat now
publishes concrete schemas without changing the embedded or eval catalog.
Browser turns default to 24 steps so that waiting and taking a fresh snapshot
before each form action fits in the turn. `EXCALIBUR_MASTER_STEPS` and
`EXCALIBUR_MASTER_TIMEOUT` remain explicit operator overrides.

Production startup already selects the real browser when system Chrome and its
Playwright driver are available. No browser environment switch is needed;
`browser.enabled` defaults to true. Run `excalibur install` to install the driver,
and install Chrome separately. Missing browser dependencies produce a startup
warning. The interactive browser permits HTTP(S) loopback and private targets
under its existing URL policy; no local-development override is needed. This is
separate from the guarded HTTP reader used by `web_read`, whose private-address
restrictions remain in place.

Each chat turn owns one logical browser session and receives an initial tab in
`browser_open_session` output. Repeated opens reuse that session. Additional tabs
use `browser_tabs`; missing or unrelated session IDs are rejected with repair
instructions. Invalid tab IDs retain the session and explain how to list or
create tabs. The turn closes its session after success, failure, cancellation or
timeout, using a separate bounded cleanup context. A failed initial tab also
closes its session. The persistent Chrome profile and remembered vault entries
survive this cleanup. This ownership is confined to daemon chat and does not
change embedded browser behavior or benchmark tools.

`TestCredentialDaemonChatBrowserLogin` (`browser` tag on Linux,
`EXCALIBUR_BROWSER_TESTS=1`) exercises the public chat and credential RPCs with
real Chrome and a deterministic model. It logs in twice to a local HTTP server,
checks that only the first login creates a credential card, and verifies both
authenticated panel responses and session cleanup. The fixture echoes the
submitted values to verify redaction in model context, conversation history,
events, daemon logs, the SQL journal and stored browser artifacts. This covers
the chat argument and resource lifecycle boundary that direct executor tests
did not exercise.

A loaded login or OTP form is ready for observation even though authentication
is pending. On that form, daemon `browser_wait_ready` returns `ready: true` and
an `authentication` marker, directing the agent to take a snapshot and use the
credential references it already has. Navigation guidance retains the existing
`input_required` marker and tells the agent to keep its session and tab. CAPTCHA
and site blocks remain blocked. Credential requests take a hostname without a
scheme, path or port. These distinctions prevent repeated navigation and
credential requests after a secure form has already been submitted.
Similarly, a successful `browser_act` that leaves a login or OTP form visible
acknowledges the action and requests a fresh observation before filling the next
field or submitting. Authentication remaining pending does not mean a completed
field entry failed.

Two live reproductions on 2026-11-03 used `gpt-6-luna`, low reasoning and the
default service tier, the production daemon, an unlocked vault and the local
login fixture. These exposed two additional observation problems beyond the
original missing browser argument schemas:

| Reproduction | Credential card and submit | Result |
| --- | --- | --- |
| 1 | `credentials.pending` returned one card; `credentials.submit` accepted remembered values | Navigation succeeded, but readiness guidance repeatedly sent the model back to authentication instead of observing the form. No dashboard code; 53 seconds. |
| 2, after readiness correction | One card; submission accepted | Snapshots and username entry succeeded. The model repeated username entry; action feedback still described the remaining authentication requirement instead of acknowledging success. No dashboard code; 35 seconds. |

The password was absent from `conversation.history` and daemon logs in both
reproductions. Both daemon and fixture processes were terminated by PID after
each attempt. No additional live calls were made after the two-reproduction
limit. The final action acknowledgement correction is covered by unit tests
and the deterministic-model, real-Chrome chat integration test; successful
live dashboard reporting and a second live login with remembered credentials
remain unverified. The fixture and nonsecret reports are outside the repository
under `/tmp/excalibur-cred-live-ogzjeva_` and
`/tmp/excalibur-cred-live-sm_r7erf`.

Follow-up verification used `systemd-run --user --scope -p MemoryMax=4G
-p MemorySwapMax=0`, `nice -n 19` and Go package parallelism `-p 2`:

- The final targeted daemon tests passed with `-race -tags browser`, including
  the two-login chat integration and the embedded/eval hash pin.
- Browser checks across `internal/browser`, `internal/daemon` and
  `internal/wireengine` passed with `EXCALIBUR_BROWSER_TESTS=1`, `-race`,
  `-tags browser -run 'Browser|GuardedRenderer'` and `-timeout 30m`.
- The full `go test -race -p 2 -count=1 -timeout 30m ./...` run passed 74
  packages and failed the unchanged CLI `TestSIGTERMContext` startup deadline.
  The complete `cmd/excalibur` package passed on rerun. Earlier ten-minute
  suite timeouts in daemon tests and a gateway subscription deadline did not
  recur in the longer full run; isolated signal and subscription checks also
  passed. No timing-test source changes were made.
- Formatting, `go vet -p 2 ./...`, `staticcheck ./...` (with `GOFLAGS=-p=2`),
  `go build -p 2 ./...`, Darwin and Windows builds, and the bare-goroutine check
  passed. The eval profile hash remains
  `4ecb6a58986660326b7d20d912918d479e7c72140c6e04ab38d83a23315fec28`;
  tool-surface pins were not changed.
