# Persistent browser engine

`browser.New(Config) *Engine` implements the unchanged, 13-operation
`capabilities.BrowserManifest()` using system Google Chrome and Playwright Go.
BR-3 wires it into production daemon and embedded default/delegate runs when
available and enabled. Eval keeps its previous deterministic surface; the
default capability registry remains fake. See
[production wiring](../../docs/development/browser-search.md).

One lazily launched `LaunchPersistentContext` owns a persistent profile. Logical
`sess-N` sessions share its cookies and local storage, and own independent
`tab-N` tabs. Calls serialize profile mutations; navigation, actions, downloads
and routine replay return pollable tasks. Poll retains outcomes, including
`input_required`. Nonempty idempotency keys are scoped by operation and return
the first outcome even when invoked concurrently. These maps live for the
engine's lifetime; they are not a replacement for the harness journal.

Call `Close` to cancel pending work, join workers, close Chrome and flush the
profile. Suspend closes the process and remembers tab URLs. The next operation
that uses a session reopens those URLs. Status observes suspension without
waking Chrome. An idle worker suspends the process after `IdleTimeout`.

## Safe defaults

- Headless Chrome, with **Chromium sandbox enabled**. HTTPS errors are not
  ignored, CSP is not bypassed and no extra permissions are granted.
- `Channel` defaults to `chrome`. `chromium` is an explicit alternative;
  a failed Chrome launch never silently changes channels or disables sandboxing.
- Navigation permits HTTP(S) and exactly `about:blank`. URL credentials,
  `file:`, `data:` and other schemes are refused. A context route also checks
  navigation requests from redirects and page actions.
- Downloads use only `DownloadsDir`, with Playwright-generated paths, then go
  into `Blobs`. Suggested names are metadata, not filesystem destinations.
- Uploads require a regular file under `AllowedUploadDirs`, after symlink
  resolution. No directories are allowed by default.
- Tool arguments never become JavaScript source. Fixed observer functions read
  DOM state or bind accessibility nodes to snapshot refs. Actions use Playwright
  locators and its auto-waiting.
- `secret://name` resolves at execution through `Secrets.Resolve(ctx,name,host)`.
  The resolver must enforce domain policy. The included `MemorySecrets` matches
  exact host names, without implicitly allowing subdomains. Secret entry binds
  to an element in the current document so a navigation cannot retarget it.
  Outputs use `[secret]`; resolver and Playwright errors never expose the value.
  Text/DOM captures redact resolved values. Screenshots of a tab after secret
  entry are entirely masked because a page can echo credentials into pixels.
- Devtools records bounded console/network metadata, with no request/response
  bodies and with credential-like query parameters redacted. Full captures and
  overflow devtools records are retrievable from the configured blob store.

CAPTCHA, login, OTP and access-denied detection reports the fake's blocked
states and `Need` wording. There is **no live view or human takeover** here.
`Update` ignores claims in its answer and checks the actual page again; it
cannot bypass a block. Detection is heuristic, not a guarantee of site access.

## Configuration and collaborators

| Field | Default / meaning |
| --- | --- |
| `ProfileDir` | User config directory, `excalibur/browser/profile`; one engine owns it at a time |
| `DownloadsDir` | `downloads` sibling of the profile directory |
| `RoutinesDir` | `routines` sibling of the profile directory |
| `AllowedUploadDirs` | Empty; no uploads |
| `Headless`, `Sandbox` | `*bool`; nil means true, a pointer to false explicitly disables it |
| `Channel` | `chrome`; only `chrome` and `chromium` are supported |
| `IdleTimeout` | Five minutes |
| `MaxTabs` | 16, shared across sessions; at most eight logical sessions |
| `Blobs` | `BlobStore.Put(context.Context,kind,[]byte)`; defaults to `MemoryBlobs` |
| `Secrets` | `SecretResolver.Resolve(context.Context,name,host)`; unavailable unless configured |
| `Clock` | `func() time.Time`, defaults to `time.Now` |

Use a durable blob adapter in production; `MemoryBlobs` is intended for tests
and embedding. Never use a normal interactive Chrome profile concurrently.
Session `profile` arguments are logical labels, not alternate disk profiles.
`mode: headed|headless` may select the launch mode; changing a running process's
mode is rejected. Disabling sandboxing is an explicit operator decision;
the production wiring warns the operator.

## Operation arguments

All session operations use `session_id`; tab operations accept `tab_id` or the
session's active tab. Their names, existing field names and manifest are shared
with the fake. Additional real-engine observations are additive.

- `browser_tabs`: `action: list|new|switch|close` (`open` aliases `new`), `url`.
- `browser_navigate`: `action: go|back|forward|reload` (`url` aliases `go`),
  `url`, optional `timeout_ms`. Results include the final URL, title and status.
- `browser_wait_ready`: `mode: load|network_idle|dom_stable|selector`,
  `selector`, `stable_ms` (200 by default), `timeout_ms`.
- `browser_snapshot`: requires readiness. Returns `snapshot_id`, `interactive`,
  bounded visible `text`, `capture_ref`, `dom_ref`, URL and title. Interactive
  roles/names come from Chrome's accessibility tree; backend DOM node identities
  receive `data-excalibur-ref` attributes. Identical accessible names still get
  distinct refs. Inline captures are capped at 16 KiB, below the adapter's
  default 40 KiB budget. Full DOM and screenshot remain in blobs.
- `browser_act`: `snapshot_id`, `ref`, `action`, `value`; drag also requires
  `target_ref`. Actions are click, type, select, hover, scroll (into view), press,
  upload and drag. After success, wait and snapshot again. Old snapshots and
  unknown refs return `stale_ref`; acting before readiness returns the exact
  fake's `not ready: call browser_wait_ready first` error.
- `browser_download`: `snapshot_id`, `ref` of a download trigger. Returns
  `blob`, `name`, `size` and `mime`.
- `browser_devtools`: `type: console|network|performance`.
- `browser_emulate`: `device` from Playwright's device list, or `viewport:
  {width,height}` (also top-level `width,height`), optional `color_scheme` and
  `locale`. A tab-owned CDP session keeps emulation active without replacing
  its persistent context.
- `browser_routine`: `action: record|list|run`, `routine`, and `steps` to record.
  Each step has `action`, `target: {role,name}`, optional `value` and `assert`.
  Drag steps also carry `destination: {role,name}`.
  A `ref` may replace `target` when recording against a current session/tab
  snapshot; it is converted to role/name before persistence. Assertions accept
  a legacy text string or `{type,value,target}` with `text_contains`,
  `url_matches` (Go regular expression) or `element_visible`. Assertions run
  after each action; replay stops at the first failed step. Routines are
  atomically saved as private JSON in `RoutinesDir`.

Actions default to 10-second Playwright waits; task lifetimes default to 30
seconds. Explicit timeouts are capped at 60 seconds. Cancel closes the affected
page to interrupt pending Playwright work, retaining its URL for recreation.
Routine assertions and cancellation stop further steps. Errors classify bad
arguments/refs as input, network/timeouts as transient, and policy failures as
permanent. Blocked pages use `input_required` rather than a failed state.

## Installation and verification

Dependencies are pinned: `github.com/mxschmitt/playwright-go v0.6201.1` (the
upstream project's current module path), and its new runtime dependencies
`github.com/deckarep/golang-set/v2 v2.8.0` and `github.com/go-stack/stack v1.8.1`.
All three LICENSE files were checked: MIT. The updated test-only checksum for
`gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c` is BSD-2-Clause (LICENSE
checked). No vendoring or third-party source
changes are made. The downloaded Playwright driver uses Playwright 1.62.1.

The default installer invokes driver installation: `browser.Install(ctx, InstallOptions{})` uses
`SkipInstallBrowsers: true`. Upstream's synchronous driver download checks
cancellation before and after installation. `Available()` checks driver
compatibility and system Chrome, without launching a browser. An equivalent
one-time driver-only installation is:

```sh
export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"
nice -n 19 go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.1 --version
```

Chrome must be installed separately (`/usr/bin/google-chrome` in the test
host). Ordinary tests are hermetic and launch no browser. Integration tests
require **both** the `browser` build tag and `EXCALIBUR_BROWSER_TESTS=1`; they
use local `httptest` pages, headless Chrome and sandboxing enabled, with no paid
APIs or remote pages. Always cap tests:

```sh
systemd-run --user --scope -p MemoryMax=3G -p MemorySwapMax=0 \
  nice -n 19 go test -p 2 -count=1 -race -timeout 90s ./internal/browser/...
EXCALIBUR_BROWSER_TESTS=1 systemd-run --user --scope \
  -p MemoryMax=3G -p MemorySwapMax=0 nice -n 19 \
  go test -p 2 -count=1 -race -tags browser -timeout 15m ./internal/browser/...
```

On non-Linux systems, `Available`, `Install` and `Invoke` return
`errors.ErrUnsupported`. The requested `New(Config) *Engine` signature cannot
also return an error, so construction remains inert on those platforms.

## Deliberate differences from the fake

The manifest is identical, including existing profile metadata; BR-3 adds
production wiring outside the default fake registry. Logical sessions share one real profile. Suspend resumes on use. Poll
retains results; repeated nonempty keys return their first result rather than
merely counting the effect once. Real blocked tasks remain blocked until the
page actually clears. Long operations consistently return working tasks; the
fake completes ordinary navigation/actions synchronously. `Need` retains its
legacy headed-mode wording for compatibility; the tool adapter replaces it
with the no-takeover guidance in production. Real actions perform effects,
and routine assertions examine the post-action page. Non-Linux construction
and synchronous install cancellation follow the limitations described above.

## Verification record (2026-11-02)

- Ordinary package tests passed with `-race -p 2 -count=1`, a 3 GiB scope,
  zero swap, low priority and `GOMAXPROCS=2`.
- The complete local-Chrome suite passed with the same cap and
  `EXCALIBUR_BROWSER_TESTS=1`, `-tags browser -race -timeout 15m`:
  **391.162 seconds**. Chrome ran with its sandbox enabled. This includes all
  action kinds, readiness modes, snapshot-bound refs, blobs, domain-bound
  secrets, upload policy, devtools, viewport emulation, routine replay and
  assertions, cookie/local-storage persistence, suspend/resume, RSS, cancellation
  and timeout recovery. A subsequent secret-domain policy check also passed
  (`-run TestBrowserEndToEnd/secrets`, 58.331 seconds).
- `gofmt -l .` produced no output. `go vet -p 2 ./...`,
  `GOMAXPROCS=2 staticcheck ./...`, tagged browser static analysis,
  `scripts/check-no-bare-go.sh`, and Linux/Darwin/Windows `go build -p 2 ./...`
  passed. Commands ran at low priority; Go tests always ran in capped scopes.
- **The repository-wide race regression is not green.** Two attempted 6 GiB
  runs (`-timeout 180s` and `-timeout 10m`) hit existing CLI timing assertions;
  the first also hit the package timeout in `internal/memory`. Runs were stopped
  after failures were confirmed. CLI failures included
  `TestEvalTimeOnlySurvivesPast25Steps`, `TestExecSignalRecords`,
  `TestExecRateLimitWaitRecords`, and `TestExecRateLimitsExpireWallBudget`.
  An isolated capped rerun of those CLI tests still failed the first and last.
  Those packages and their tests are unchanged. Observed host load exceeded
  100, and starting the installed driver's `--version` alone took 26.306 seconds.
  No shared tests, deadlines, contracts or profile pins were modified to hide
  these failures. Re-run the global regression on a less loaded host before merge.

## Future work

- Live view and human takeover (not implemented).
- Prompt-injection defenses: untrusted-content marking, plan-then-execute,
  confirmations for sensitive actions, and an injection detector.
- Process confinement using Landlock/seccomp through
  `internal/platform.Isolator`.
- An egress policy.
