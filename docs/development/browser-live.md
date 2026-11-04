# Live browser view

A watch-only view of the agent's browser in the web interface's right panel.
The viewer cannot click or type: there is no takeover.

This document is the binding contract between the engine, daemon, gateway and
web layers.

## Data flow

```text
internal/browser      Page.startScreencast per watched session, action observer
       |  LiveEvent (observer), LiveFrame (watcher callback)
internal/daemon       browserLiveHub: session -> conversation binding
       |  bus: browser.session, browser.activity    RPC: browser.live_state
internal/gateway      /ws (events), /rpc, /ws/browser (binary frames)
       |
apps/web              features/browserlive: store, frame socket, panel view
```

Frames never touch the event bus, telemetry sink, journal, blobs, or logs. They
exist only in memory between Chrome and the connected viewers.

## Engine API (`internal/browser/live.go`)

```go
type LiveRect struct{ X, Y, Width, Height float64 }

type LiveEvent struct {
	Type    string // "session_opened", "session_closed", "activity"
	Session string
	Tab     string
	Phase   string // "start", "done", "failed" (activity only)
	Kind    string // api.BrowserKind* values (activity only)
	Target  string // accessible element name, secrets removed, at most 80 runes
	URL     string // no query string or fragment
	Title   string
	Box     *LiveRect // viewport CSS pixels, only while watched
}

type LiveFrame struct {
	Seq           uint64
	Tab           string
	JPEG          []byte // owned by the receiver's read; never mutated afterwards
	Width, Height int    // CSS pixels of the area the JPEG shows
	At            time.Time
}

// SetLiveObserver installs a non-blocking callback. Nil disables events.
func (e *Engine) SetLiveObserver(fn func(LiveEvent))

// WatchLive streams the session's active tab while at least one watcher exists.
// fn must not block. stop is idempotent. ErrUnknownLiveSession for unknown IDs.
// A late watcher immediately receives the latest cached frame, when one exists.
func (e *Engine) WatchLive(session string, fn func(LiveFrame)) (stop func(), err error)

// LiveWatched reports whether any watcher is attached to the session.
func (e *Engine) LiveWatched(session string) bool
```

Rules:

- `WatchLive` and `stop` never wait on `opMu`, so they stay responsive while a
  long action runs. Live state has its own mutex and a shadow map of each
  session's active tab, which `execute` refreshes after each operation.
- Screencast: `format: jpeg`, `quality: 60`, `maxWidth: 1280`, `maxHeight: 800`.
  Each frame is acknowledged from a separate goroutine (`safego.Go`), never
  from the Playwright event handler, and paced to at most 10 fps by delaying the
  acknowledgement. Frames above 1 MiB are dropped but still acknowledged.
- The stream follows the session's active tab and survives tab switches and
  page restoration. It stops with the last watcher, session close, suspension
  and engine close. A silent page gets an initial frame through a CDP
  `Page.captureScreenshot` if no screencast frame arrives within 500 ms. This
  deadline runs independently of `Page.startScreencast`: once opening CDP has
  succeeded, a pending start cannot block the fallback.
- Each stream protocol call (open, start, ack, screenshot, layout metrics, stop
  and detach) runs through a `safego.Go` helper with a 3 s deadline. Cancellation
  releases the stream immediately and late results are discarded. Stop and
  detach share a 3 s cleanup budget; `stop` never waits for cleanup. The CDP API
  has no per-call cancellation, so an underlying worker still needs its
  transport to return or disconnect after the caller abandons it.
- A supervisor retains the registered stream while watchers remain. Failed or
  timed-out open, start or ack calls tear down the current CDP session and retry
  after 250 ms, doubling to a maximum of 2 s. Every wait is interruptible by
  stream cancellation. Retired sessions cannot deliver frames to a successor.
- If no frame has been delivered for 1.5 s, a watchdog captures a screenshot
  through the current CDP session. It delivers only changed bytes, shares the
  10 fps budget with screencast frames and never captures or delivers withheld
  tabs. An idle, unchanged page costs at most one screenshot per 1.5 s and is
  not repeatedly restarted. A failed screenshot restarts CDP with the same
  backoff, as does 6 s without a screencast frame when the main-frame URL has
  changed since the last screencast frame (or since this CDP session opened).
- Each failed attempt emits one fixed `slog.Warn` message with only session ID,
  tab ID and failure class (`open`, `start`, `ack`, `screenshot`, `timeout`). The
  first successful capture or acknowledgement after failure emits one fixed
  recovery `slog.Info`. Logs never contain URLs, titles, frames or raw errors.
- With a nil observer and no watcher, the engine's behavior and latency are
  exactly as before: `Fill`, no dwell, no bounding-box calls.

Observed operations: `browser_navigate` (navigate), `browser_wait_ready` (wait),
`browser_snapshot` (read), `browser_act` (its action; `type` becomes
`type_password` for password targets) and `browser_download` (download). Each
emits `start`, then `done` or `failed`. A main-frame navigation emits a `page`
activity with its URL (phase `done`). `done` carries the current URL and title.

While watched, an action also:

1. scrolls the target into view and reads its bounding box (1 s limit; a failure
   gives a nil `Box` and never fails the action);
2. emits `start` with `Box`, then dwells 350 ms so a viewer sees the highlight;
3. types visibly, as described below.

### Visible typing and secrets

| Value | Target | Behavior |
| --- | --- | --- |
| Plain, at most 120 runes | Not a password field | Clear, then one key at a time, 45 ms per key and 2 s in total; verifies the value and falls back to `Fill` |
| Plain, over 120 runes | Not a password field | `Fill` |
| Any value | `type=password`, or `autocomplete` containing `password` | `Fill`; kind `type_password`, without animation |
| `secret://….username` or `.email` | Not a password field | Document-bound handle, typed visibly |
| Any other `secret://` ref | `type=password` | Document-bound `Fill`; kind `type_password` |
| Any other `secret://` ref | Visible field | Document-bound `Fill`; kind `type_password`; the tab's frames are withheld until its next main-frame navigation |

Unwatched, every row is the existing `Fill` path. Values never appear in events,
errors, or logs. `Target`, `URL`, and `Title` pass through `Engine.clean`.

## Daemon (`internal/daemon/browser_live.go`)

`browserLiveHub` binds a session to `credentialOrigin` (conversation and run)
when `browser_open_session` succeeds, through a tool wrapper registered in
`engine_wire.go`. Engine events for unbound sessions are ignored.

- `browser.session` `active` is published at the session's first activity of any
  kind, so opening a session or a blank tab alone shows nothing.
- `browser.session` `ended` is published when a visible session closes, and the
  hub terminates its frame viewers. A session that never became visible ends
  silently.
- `browser.activity` is published for each observed activity with an increasing
  `seq`. `browser.live_state` returns the last session and activity of the
  conversation; ended sessions are returned for at most 60 seconds.

## Gateway: `GET /ws/browser?conversation_id=…&session_id=…`

Dedicated WebSocket so video can never delay or close the event socket. Viewer
lifetime equals socket lifetime, which is what starts and stops the screencast.
The gateway uses an optional backend interface:

```go
type BrowserFrame struct {
	Seq           uint64
	Tab           string
	JPEG          []byte
	Width, Height int
	AtMillis      int64
}

type BrowserLiveBackend interface {
	// WatchBrowser delivers frames until stop or until done closes.
	// A session outside the conversation yields an *api.Error.
	WatchBrowser(conversationID, sessionID string, fn func(BrowserFrame)) (stop func(), done <-chan struct{}, err error)
}
```

- The loopback peer rule of `ServeHTTP` applies. `websocket.Accept` uses its
  default same-origin check, unlike the event socket.
- Unknown session: HTTP 404 before the upgrade. Backend without the interface:
  HTTP 404.
- At most 4 viewers per session; a fifth gets HTTP 429.
- Each viewer has a one-frame mailbox (newest wins). One writer goroutine owns
  writes with a 5 s timeout. A failed write ends only that viewer.
- Session end closes with status 1000, reason `ended`.
- Client messages are read and discarded, with a small read limit.

Every binary message is:

```text
uint32 big-endian header length | UTF-8 JSON header | JPEG bytes
{"seq":12,"tab_id":"tab-1","width":1280,"height":720,"at":1793000000000}
```

## Events and RPC

See `internal/api/browser_live.go` and `packages/contracts`. `browser.activity`
and `browser.session` are ordinary bus events, so they replay after a reconnect;
clients must order by `seq`, ignore replayed sessions older than the current
state, and use `browser.live_state` as the source of truth after connecting.

## Web (`apps/web/src/features/browserlive`)

- A new right-panel view, `browser`, added to `PanelView` and `panels.ts`.
- Opens automatically once per `session_id` on a fresh `browser.session`
  `active`, for the conversation on screen. Never on replayed or stale events.
- After the user closes the panel it stays closed for that session. A discreet
  control beside the panel toggle reopens it while the session is active.
- Shows the URL and title bar, the live image, the action label, and the
  highlight box. The box scales with the frame's `width` and `height`. The image
  has no pointer or keyboard behavior.
- Connects to `/ws/browser` only while the view is on screen and the session is
  active. It reconnects with bounded backoff.
- Ended sessions keep the last frame, dimmed, with a "session ended" notice.
- On an overlay panel (narrow screens), a `credentials.requested` event for this
  conversation closes the panel so the secure form in the chat stays reachable.

## Vault logins

`credentials.save_login` stores a login from Settings > Passwords as a complete
remembered bundle, so `request_credentials` returns its `secret://` refs without
showing a form and the values never reach the model. Entries are bound to the
exact login host. The model is told which saved logins exist: label, start URL,
and login host, and never any value.

## Verification

Hermetic: `go test -race ./internal/browser ./internal/gateway ./internal/daemon
./internal/api ./internal/config`, and `pnpm check` in `apps/web`. Real Chrome:
`EXCALIBUR_BROWSER_TESTS=1 go test -race -tags browser -run 'TestLive|BrowserSecureLogin'
./internal/browser ./internal/daemon`. Web flows: `e2e/browser-live.spec.ts` and
`e2e/credentials.spec.ts`.

A full run with a real daemon, the real agent and a local form page covered a
saved login, the automatic panel, live frames, action labels, highlight boxes and
the final answer, at 1440 and 390 pixels. A test password was absent from page
text, events, the daemon log and the data directory.

A real institutional login was verified read-only, with guard-rails stopping the
test on any action resembling a submission: secure fill by reference, redirects
across hosts, a score-based reCAPTCHA page and a slow post-login redirect.

The engine treats score-based reCAPTCHA as no block, because it presents no
challenge, while challenge-capable embeds stay blocks. It solves and forges
nothing: a site that rejects the session's score just rejects the login. On the
observed path, a click or Enter on a login page waits up to 8 s for the page to
respond, so a slow site doesn't read as a failed login.

Known limits: a page that reveals a password field's value through its own
controls is not masked; two simultaneous conversations share one Chrome, and a tab
that isn't in front may produce fewer screencast frames, in which case the watchdog
supplies screenshots.
